package main

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/pkg/errors"
	csicommon "github.com/warm-metal/container-image-csi-driver/pkg/csi-common"
	"github.com/warm-metal/container-image-csi-driver/pkg/watcher"
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
		driver:  driver,
		watcher: watcher,
	}
}

type ControllerServer struct {
	driver  *csicommon.CSIDriver
	watcher *watcher.Watcher
	csi.UnimplementedControllerServer
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

// ControllerModifyVolume implements the required interface
func (cs *ControllerServer) ControllerModifyVolume(context.Context, *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities returns the capabilities of the controller service.
func (c *ControllerServer) ControllerGetCapabilities(_ context.Context, _ *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
					},
				},
			},
		},
	}, nil
}

// ValidateVolumeCapabilities validates the volume capabilities.
func (c *ControllerServer) ValidateVolumeCapabilities(_ context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	for _, cap := range req.VolumeCapabilities {
		if cap.AccessMode.Mode != csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY &&
			cap.AccessMode.Mode != csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
			return &csi.ValidateVolumeCapabilitiesResponse{
				Message: "Only ReadOnlyMany or ReadOnlyOnce access modes are supported",
			}, nil
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeContext:      req.VolumeContext,
			VolumeCapabilities: req.VolumeCapabilities,
			Parameters:         req.Parameters,
		},
	}, nil
}

// ListVolumes is not implemented.
func (c *ControllerServer) ListVolumes(_ context.Context, _ *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// GetCapacity is not implemented.
func (c *ControllerServer) GetCapacity(_ context.Context, _ *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// CreateSnapshot is not implemented.
func (c *ControllerServer) CreateSnapshot(_ context.Context, _ *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// DeleteSnapshot is not implemented.
func (c *ControllerServer) DeleteSnapshot(_ context.Context, _ *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ListSnapshots is not implemented.
func (c *ControllerServer) ListSnapshots(_ context.Context, _ *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// Remove exported method and keep only unexported one
func (cs *ControllerServer) mustEmbedUnimplementedControllerServer() {}
