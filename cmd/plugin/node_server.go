package main

import (
	"context"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/remoteimage"
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
	mounter  backend.Mounter
	imageSvc cri.ImageServiceClient
}

const (
	ctxKeyVolumeHandle      = "volumeHandle"
	ctxKeyImage             = "image"
	ctxKeyPullAlways        = "pullAlways"
	ctxKeySecret            = "secret"
	ctxKeyPodNamespace      = "csi.storage.k8s.io/pod.namespace"
	ctxKeyPodServiceAccount = "csi.storage.k8s.io/serviceAccount.name"
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
	}
	secret := ""
	namespace := ""
	sa := ""

	if len(req.VolumeContext) > 0 {
		if len(req.VolumeContext[ctxKeyVolumeHandle]) > 0 {
			image = req.VolumeContext[ctxKeyVolumeHandle]
		} else if len(req.VolumeContext[ctxKeyImage]) > 0 {
			image = req.VolumeContext[ctxKeyImage]
		}

		opts.PullAlways = strings.ToLower(req.VolumeContext[ctxKeyPullAlways]) == "true"
		secret = req.VolumeContext[ctxKeySecret]
		if len(secret) > 0 {
			namespace = req.VolumeContext[ctxKeyPodNamespace]
			sa = req.VolumeContext[ctxKeyPodServiceAccount]
		}
	}

	if err = n.mounter.Mount(
		ctx, remoteimage.NewPuller(n.imageSvc, image, secret, namespace, sa),
		req.VolumeId, image, req.TargetPath, &opts,
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
