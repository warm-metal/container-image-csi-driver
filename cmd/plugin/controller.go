package main

import (
    "context"
    "github.com/container-storage-interface/spec/lib/go/csi"
    csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
)

type ControllerServer struct {
    *csicommon.DefaultControllerServer
}

func (c ControllerServer) ControllerExpandVolume(ctx context.Context, request *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
    panic("implement me")
}

func (c ControllerServer) ControllerGetVolume(ctx context.Context, request *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
    panic("implement me")
}
