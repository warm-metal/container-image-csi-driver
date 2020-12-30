package main

import (
    "context"
    "github.com/container-storage-interface/spec/lib/go/csi"
    "github.com/golang/glog"
    csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
    "github.com/warm-metal/csi-driver-image/pkg/backend"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    k8smount "k8s.io/utils/mount"
    "os"
)

type nodeServer struct {
    *csicommon.DefaultNodeServer
    mounter backend.Mounter
}

// FIXME options: forcePull, credential

func (n nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (resp *csi.NodePublishVolumeResponse, err error) {
    glog.Infof("request: %s", req.String())
    if len(req.VolumeId)  == 0 {
        err = status.Error(codes.InvalidArgument, "VolumeId is missing")
        return
    }

    if len(req.TargetPath)  == 0 {
        err = status.Error(codes.InvalidArgument, "TargetPath is missing")
        return
    }

    //if len(req.VolumeContext)  == 0 {
    //    err = status.Error(codes.InvalidArgument, "VolumeContext.image is missing")
    //    return
    //}

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

    if err = n.mounter.Mount(ctx, req.VolumeId, req.TargetPath); err != nil {
        err = status.Error(codes.Internal, err.Error())
        return
    }

    return &csi.NodePublishVolumeResponse{}, nil
}

func (n nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (resp *csi.NodeUnpublishVolumeResponse, err error) {
    if len(req.VolumeId)  == 0 {
        err = status.Error(codes.InvalidArgument, "VolumeId is missing")
        return
    }

    if len(req.TargetPath)  == 0 {
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
