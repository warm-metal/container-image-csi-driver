package main

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/pkg/errors"
	"github.com/warm-metal/container-image-csi-driver/pkg/watcher"
	csicommon "github.com/warm-metal/csi-drivers/pkg/csi-common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// GiB = 1024 * 1024 * 1024
	GiB = 1024 * 1024 * 1024
	// Default volume size = 1GB
	defaultVolumeSize = 1 * GiB
)

func NewControllerServer(driver *csicommon.CSIDriver, watcher *watcher.Watcher) *ControllerServer {
	return &ControllerServer{
		DefaultControllerServer: csicommon.NewDefaultControllerServer(driver),
		watcher:                 watcher,
	}
}

type ControllerServer struct {
	*csicommon.DefaultControllerServer
	watcher *watcher.Watcher
}

func (c ControllerServer) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (c ControllerServer) ControllerGetVolume(context.Context, *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (c *ControllerServer) DeleteVolume(_ context.Context, _ *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	return &csi.DeleteVolumeResponse{}, nil
}

func (c ControllerServer) CreateVolume(_ context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	volumeSize := int64(defaultVolumeSize)
	if req.GetCapacityRange() != nil {
		volumeSize = req.GetCapacityRange().GetRequiredBytes()
	}

	volumeID, err := c.watcher.GetImage(req.Name)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get volume handle")
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volumeID,
			CapacityBytes: volumeSize,
		},
	}, nil
}
