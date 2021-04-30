package main

import (
	"context"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/remoteimage"
	"github.com/warm-metal/csi-driver-image/pkg/secret"
	"github.com/warm-metal/csi-drivers/pkg/csi-common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
	"k8s.io/klog/v2"
	k8smount "k8s.io/utils/mount"
	"os"
	"strings"
)

type nodeServer struct {
	*csicommon.DefaultNodeServer
	mounter     backend.Mounter
	imageSvc    cri.ImageServiceClient
	secretCache secret.Cache
}

const (
	ctxKeyVolumeHandle    = "volumeHandle"
	ctxKeyImage           = "image"
	ctxKeyPullAlways      = "pullAlways"
	ctxKeySecret          = "secret"
	ctxKeySecretNamespace = "secretNamespace"
	ctxKeyPodName         = "csi.storage.k8s.io/pod.name"
	ctxKeyPodNamespace    = "csi.storage.k8s.io/pod.namespace"
)

func (n nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (resp *csi.NodePublishVolumeResponse, err error) {
	klog.Infof("request: %s", req.String())
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
	opts := backend.MountOptions{
		ReadOnly: req.Readonly,
		VolumeId: req.VolumeId,
	}
	secretName := ""
	secretNamespace := ""
	pod := ""
	namespace := ""

	if len(req.VolumeContext) > 0 {
		if len(req.VolumeContext[ctxKeyVolumeHandle]) > 0 {
			image = req.VolumeContext[ctxKeyVolumeHandle]
		} else if len(req.VolumeContext[ctxKeyImage]) > 0 {
			image = req.VolumeContext[ctxKeyImage]
		}

		opts.PullAlways = strings.ToLower(req.VolumeContext[ctxKeyPullAlways]) == "true"
		namespace = req.VolumeContext[ctxKeyPodNamespace]
		pod = req.VolumeContext[ctxKeyPodName]
		secretName = req.VolumeContext[ctxKeySecret]
		secretNamespace = req.VolumeContext[ctxKeySecretNamespace]
		if len(secretName) > 0 && len(secretNamespace) == 0 {
			secretNamespace = namespace
		}
	}

	keyring, err := n.secretCache.GetDockerKeyring(ctx, secretName, secretNamespace, pod, namespace)
	if err != nil {
		err = status.Errorf(codes.Aborted, "unable to fetch keyring: %s", err)
		return
	}

	if err = n.mounter.Mount(
		ctx, remoteimage.NewPuller(n.imageSvc, image, keyring), req.VolumeId, image, req.TargetPath, &opts,
	); err != nil {
		err = status.Error(codes.Internal, err.Error())
		return
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (n nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (resp *csi.NodeUnpublishVolumeResponse, err error) {
	if len(req.VolumeId) == 0 {
		err = status.Error(codes.InvalidArgument, "VolumeId is missing")
		return
	}

	if len(req.TargetPath) == 0 {
		err = status.Error(codes.InvalidArgument, "TargetPath is missing")
		return
	}

	if err = n.mounter.Unmount(ctx, req.VolumeId, req.TargetPath); err != nil {
		err = status.Error(codes.Internal, err.Error())
		return
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n nodeServer) NodeStageVolume(ctx context.Context, request *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (n nodeServer) NodeUnstageVolume(ctx context.Context, request *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (n nodeServer) NodeExpandVolume(ctx context.Context, request *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
