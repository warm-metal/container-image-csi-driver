package main

import (
    "context"
    "github.com/container-storage-interface/spec/lib/go/csi"
    csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    k8smount "k8s.io/utils/mount"
    "os"
)

type NodeServer struct {
    *csicommon.DefaultNodeServer
}

// FIXME options: forcePull

func (n NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (resp *csi.NodePublishVolumeResponse, err error) {
    if len(req.VolumeId)  == 0 {
        err = status.Error(codes.InvalidArgument, "VolumeId is missing")
        return
    }

    if len(req.TargetPath)  == 0 {
        err = status.Error(codes.InvalidArgument, "TargetPath is missing")
        return
    }

    if len(req.VolumeContext)  == 0 {
        err = status.Error(codes.InvalidArgument, "VolumeContext.image is missing")
        return
    }

    notMnt, err := k8smount.New("").IsLikelyNotMountPoint(req.TargetPath)
    if err != nil {
        if !os.IsNotExist(err) {
            err = status.Error(codes.Internal, err.Error())
            return
        }

        if err = os.MkdirAll(req.TargetPath, 0750); err != nil {
            err = status.Error(codes.Internal, err.Error())
            return
        }

        notMnt = true
    }

    if !notMnt {
        return &csi.NodePublishVolumeResponse{}, nil
    }

    image := req.VolumeContext["image"]
    if len(image) == 0 {
        err = status.Error(codes.InvalidArgument, "VolumeContext.image is missing")
        return
    }

    if err = mountContainerdImage(ctx, req.VolumeId, image, req.TargetPath); err != nil {
        err = status.Error(codes.Internal, err.Error())
        return
    }

    return &csi.NodePublishVolumeResponse{}, nil
}

func (n NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (resp *csi.NodeUnpublishVolumeResponse, err error) {
    if len(req.VolumeId)  == 0 {
        err = status.Error(codes.InvalidArgument, "VolumeId is missing")
        return
    }

    if len(req.TargetPath)  == 0 {
        err = status.Error(codes.InvalidArgument, "TargetPath is missing")
        return
    }

    if err = umountContainerdImage(ctx, req.VolumeId, req.TargetPath); err != nil {
        err = status.Error(codes.Internal, err.Error())
        return
    }

    return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n NodeServer) NodeStageVolume(ctx context.Context, request *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
    return nil, status.Error(codes.Unimplemented, "")
}

func (n NodeServer) NodeUnstageVolume(ctx context.Context, request *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
    return nil, status.Error(codes.Unimplemented, "")
}

func (n NodeServer) NodeExpandVolume(ctx context.Context, request *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
    return nil, status.Error(codes.Unimplemented, "")
}
