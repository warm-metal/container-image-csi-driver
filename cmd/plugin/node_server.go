package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/containerd/containerd/reference/docker"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend"
	"github.com/warm-metal/container-image-csi-driver/pkg/metrics"
	"github.com/warm-metal/container-image-csi-driver/pkg/remoteimage"
	"github.com/warm-metal/container-image-csi-driver/pkg/remoteimageasync"
	"github.com/warm-metal/container-image-csi-driver/pkg/secret"
	csicommon "github.com/warm-metal/csi-drivers/pkg/csi-common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"

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

func NewNodeServer(driver *csicommon.CSIDriver, mounter backend.Mounter, imageSvc cri.ImageServiceClient, secretStore secret.Store, asyncImagePullTimeout time.Duration) *NodeServer {
	ns := NodeServer{
		DefaultNodeServer:     csicommon.NewDefaultNodeServer(driver),
		mounter:               mounter,
		imageSvc:              imageSvc,
		secretStore:           secretStore,
		asyncImagePullTimeout: asyncImagePullTimeout,
		asyncImagePuller:      nil,
	}
	if asyncImagePullTimeout >= time.Duration(30*time.Second) {
		klog.Infof("Starting node server in Async mode with %v timeout", asyncImagePullTimeout)
		ns.asyncImagePuller = remoteimageasync.StartAsyncPuller(context.TODO(), 100)
	} else {
		klog.Info("Starting node server in Sync mode")
		ns.asyncImagePullTimeout = 0 // set to default value
	}
	return &ns
}

type NodeServer struct {
	*csicommon.DefaultNodeServer
	mounter               backend.Mounter
	imageSvc              cri.ImageServiceClient
	secretStore           secret.Store
	asyncImagePullTimeout time.Duration
	asyncImagePuller      remoteimageasync.AsyncPuller
}

func (n NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (resp *csi.NodePublishVolumeResponse, err error) {
	valuesLogger := klog.LoggerWithValues(klog.NewKlogr(), "pod-name", req.VolumeContext["pod-name"], "namespace", req.VolumeContext["namespace"], "uid", req.VolumeContext["uid"])
	valuesLogger.Info("Incoming NodePublishVolume request", "request string", protosanitizer.StripSecrets(req))
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

		if err = os.MkdirAll(req.TargetPath, 0o755); err != nil {
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

	pullAlways := strings.ToLower(req.VolumeContext[ctxKeyPullAlways]) == "true"

	keyring, err := n.secretStore.GetDockerKeyring(ctx, req.Secrets)
	if err != nil {
		err = status.Errorf(codes.Aborted, "unable to fetch keyring: %s", err)
		return
	}

	namedRef, err := docker.ParseDockerRef(image)
	if err != nil {
		klog.Errorf("unable to normalize image %q: %s", image, err)
		return
	}

	//NOTE: we are relying on n.mounter.ImageExists() to return false when
	//      a first-time pull is in progress, else this logic may not be
	//      correct. should test this.
	if pullAlways || !n.mounter.ImageExists(ctx, namedRef) {
		klog.Errorf("pull image %q", image)
		puller := remoteimage.NewPuller(n.imageSvc, namedRef, keyring)

		if n.asyncImagePuller != nil {
			var session *remoteimageasync.PullSession
			session, err = n.asyncImagePuller.StartPull(image, puller, n.asyncImagePullTimeout)
			if err != nil {
				err = status.Errorf(codes.Aborted, "unable to pull image %q: %s", image, err)
				metrics.OperationErrorsCount.WithLabelValues("pull-async-start").Inc()
				return
			}
			if err = n.asyncImagePuller.WaitForPull(session, ctx); err != nil {
				err = status.Errorf(codes.Aborted, "unable to pull image %q: %s", image, err)
				metrics.OperationErrorsCount.WithLabelValues("pull-async-wait").Inc()
				return
			}
		} else {
			if err = puller.Pull(ctx); err != nil {
				err = status.Errorf(codes.Aborted, "unable to pull image %q: %s", image, err)
				metrics.OperationErrorsCount.WithLabelValues("pull-sync-call").Inc()
				return
			}
		}
	}

	ro := req.Readonly ||
		req.VolumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY ||
		req.VolumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	if err = n.mounter.Mount(ctx, req.VolumeId, backend.MountTarget(req.TargetPath), namedRef, ro); err != nil {
		err = status.Error(codes.Internal, err.Error())
		metrics.OperationErrorsCount.WithLabelValues("mount").Inc()
		return
	}

	valuesLogger.Info("Successfully completed NodePublishVolume request", "request string", protosanitizer.StripSecrets(req))

	return &csi.NodePublishVolumeResponse{}, nil
}

func (n NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (resp *csi.NodeUnpublishVolumeResponse, err error) {
	klog.Infof("unmount request: %s", protosanitizer.StripSecrets(req))
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
		metrics.OperationErrorsCount.WithLabelValues("unmount").Inc()
		err = status.Error(codes.Internal, err.Error())
		return
	}

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
