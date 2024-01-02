package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/containerd/containerd/reference/docker"
	"github.com/google/uuid"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/metrics"
	"github.com/warm-metal/csi-driver-image/pkg/mountexecutor"
	"github.com/warm-metal/csi-driver-image/pkg/mountstatus"
	"github.com/warm-metal/csi-driver-image/pkg/pullexecutor"
	"github.com/warm-metal/csi-driver-image/pkg/secret"
	csicommon "github.com/warm-metal/csi-drivers/pkg/csi-common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
	"k8s.io/klog/v2"
	k8smount "k8s.io/mount-utils"
)

const (
	ctxKeyVolumeHandle    = "volumeHandle"
	ctxKeyImage           = "image"
	ctxKeyPullAlways      = "pullAlways"
	ctxKeyEphemeralVolume = "csi.storage.k8s.io/ephemeral"
)

type ImagePullStatus int

func NewNodeServer(driver *csicommon.CSIDriver, mounter *backend.SnapshotMounter, imageSvc cri.ImageServiceClient, secretStore secret.Store, asyncImagePullMount bool) *NodeServer {
	return &NodeServer{
		DefaultNodeServer:   csicommon.NewDefaultNodeServer(driver),
		mounter:             mounter,
		secretStore:         secretStore,
		asyncImagePullMount: asyncImagePullMount,
		mountExecutor: mountexecutor.NewMountExecutor(&mountexecutor.MountExecutorOptions{
			AsyncMount: asyncImagePullMount,
			Mounter:    mounter,
		}),
		pullExecutor: pullexecutor.NewPullExecutor(&pullexecutor.PullExecutorOptions{
			AsyncPull:          asyncImagePullMount,
			ImageServiceClient: imageSvc,
			SecretStore:        secretStore,
			Mounter:            mounter,
		}),
	}
}

type NodeServer struct {
	*csicommon.DefaultNodeServer
	mounter             *backend.SnapshotMounter
	secretStore         secret.Store
	asyncImagePullMount bool
	mountExecutor       *mountexecutor.MountExecutor
	pullExecutor        *pullexecutor.PullExecutor
}

func (n NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (resp *csi.NodePublishVolumeResponse, err error) {
	u, _ := uuid.NewRandom()
	valuesLogger := klog.LoggerWithValues(klog.NewKlogr(), "pod-name", req.VolumeContext["pod-name"], "namespace", req.VolumeContext["namespace"], "uid", req.VolumeContext["uid"], "request-id", u.String())
	valuesLogger.Info(fmt.Sprintf("mount request: %s", req.String())) // TODO: info(fmt.Sprintf(...)) is messy but Logger doesn't support infof
	deadline, _ := ctx.Deadline()
	valuesLogger.Info(fmt.Sprintf("deadline: %f", time.Until(deadline).Seconds()))
	if len(req.VolumeId) == 0 {
		err = status.Error(codes.InvalidArgument, "VolumeId is missing")
		return
	}

	if len(req.TargetPath) == 0 {
		err = status.Error(codes.InvalidArgument, "TargetPath is missing")
		return
	}

	if req.VolumeCapability == nil {
		err = status.Error(codes.InvalidArgument, "VolumeCapability is missing")
		return
	}

	if _, isBlock := req.VolumeCapability.AccessType.(*csi.VolumeCapability_Block); isBlock {
		err = status.Error(codes.InvalidArgument, "unable to mount as a block device")
		return
	}

	if len(req.VolumeContext) == 0 {
		err = status.Error(codes.InvalidArgument, "VolumeContext is missing")
		return
	}

	if req.VolumeContext[ctxKeyEphemeralVolume] != "true" &&
		req.VolumeCapability.AccessMode.Mode != csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY &&
		req.VolumeCapability.AccessMode.Mode != csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
		err = status.Error(codes.InvalidArgument, "AccessMode of PV can be only ReadOnlyMany or ReadOnlyOnce")
		return
	}

	notMnt, err := k8smount.New("").IsLikelyNotMountPoint(req.TargetPath)
	if err != nil {
		if !os.IsNotExist(err) {
			err = status.Error(codes.Internal, err.Error())
			return
		}

		if err = os.MkdirAll(req.TargetPath, 0755); err != nil {
			err = status.Error(codes.Internal, err.Error())
			return
		}

		notMnt = true
	}

	if !notMnt {
		return &csi.NodePublishVolumeResponse{}, nil
	}

	// For PVs, VolumeId is the image. For ephemeral volumes, it is a string.
	image := req.VolumeId

	if len(req.VolumeContext[ctxKeyVolumeHandle]) > 0 {
		image = req.VolumeContext[ctxKeyVolumeHandle]
	} else if len(req.VolumeContext[ctxKeyImage]) > 0 {
		image = req.VolumeContext[ctxKeyImage]
	}

	namedRef, err := docker.ParseDockerRef(image)
	if err != nil {
		klog.Errorf("unable to normalize image %q: %s", image, err)
		return
	}

	pullAlways := strings.ToLower(req.VolumeContext[ctxKeyPullAlways]) == "true"

	po := &pullexecutor.PullOptions{
		Context:     ctx,
		NamedRef:    namedRef,
		PullAlways:  pullAlways,
		Image:       image,
		PullSecrets: req.Secrets,
	}

	if e := n.pullExecutor.StartPulling(po, valuesLogger); e != nil {
		err = status.Errorf(codes.Aborted, "unable to pull image %q: %s", image, e)
		return
	}

	if e := n.pullExecutor.WaitForPull(po); e != nil {
		err = status.Errorf(codes.DeadlineExceeded, e.Error())
		return
	}

	if mountstatus.Get(req.VolumeId) == mountstatus.Mounted {
		return &csi.NodePublishVolumeResponse{}, nil
	}

	o := &mountexecutor.MountOptions{
		Context:          ctx,
		NamedRef:         namedRef,
		VolumeId:         req.VolumeId,
		TargetPath:       req.TargetPath,
		VolumeCapability: req.VolumeCapability,
		ReadOnly:         req.Readonly,
	}

	if e := n.mountExecutor.StartMounting(o, valuesLogger); e != nil {
		err = status.Error(codes.Internal, e.Error())
		return
	}

	if e := n.mountExecutor.WaitForMount(o); e != nil {
		err = status.Errorf(codes.DeadlineExceeded, e.Error())
		return
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (n NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (resp *csi.NodeUnpublishVolumeResponse, err error) {
	klog.Infof("unmount request: %s", req.String())
	if len(req.VolumeId) == 0 {
		err = status.Error(codes.InvalidArgument, "VolumeId is missing")
		return
	}

	if len(req.TargetPath) == 0 {
		err = status.Error(codes.InvalidArgument, "TargetPath is missing")
		return
	}

	mnt, err := k8smount.New("").IsMountPoint(req.TargetPath)
	if err != nil || !mnt {
		return &csi.NodeUnpublishVolumeResponse{}, err
	}

	if err = n.mounter.Unmount(ctx, req.VolumeId, backend.MountTarget(req.TargetPath)); err != nil {
		// TODO(vadasambar): move this to mountexecutor once mountexecutor has `StartUnmounting` function
		metrics.OperationErrorsCount.WithLabelValues("StartUnmounting").Inc()
		err = status.Error(codes.Internal, err.Error())
		return
	}

	// Clear the mountstatus since the volume has been unmounted
	// Not doing this will make mount not work properly if the same volume is
	// attempted to mount twice
	mountstatus.Delete(req.VolumeId)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n NodeServer) NodeStageVolume(ctx context.Context, _ *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (n NodeServer) NodeUnstageVolume(ctx context.Context, _ *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (n NodeServer) NodeExpandVolume(ctx context.Context, _ *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
