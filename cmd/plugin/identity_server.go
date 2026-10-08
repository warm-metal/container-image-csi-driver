package main

import (
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

// runtimeProbeTimeout bounds the container runtime check in Probe. It must stay below the
// --probe-timeout of the liveness-probe sidecar.
const runtimeProbeTimeout = 3 * time.Second

type IdentityServer struct {
	version string
	// imageSvc is set in node mode: Probe then also checks that the container runtime answers.
	imageSvc cri.ImageServiceClient
	csi.UnimplementedIdentityServer
}

func NewIdentityServer(version string) *IdentityServer {
	return &IdentityServer{
		version: version,
	}
}

// NewNodeIdentityServer returns an identity server whose Probe fails while the container
// runtime is unreachable. The runtime socket is a file bind mount: after a runtime restart
// the plugin keeps the old inode and every call is "connection refused". Failing Probe makes
// the liveness probe restart the container, which mounts the new socket.
func NewNodeIdentityServer(version string, imageSvc cri.ImageServiceClient) *IdentityServer {
	return &IdentityServer{
		version:  version,
		imageSvc: imageSvc,
	}
}

func (ids *IdentityServer) GetPluginInfo(_ context.Context, _ *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          driverName,
		VendorVersion: ids.version,
	}, nil
}

func (ids *IdentityServer) Probe(ctx context.Context, _ *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	if ids.imageSvc != nil {
		ctx, cancel := context.WithTimeout(ctx, runtimeProbeTimeout)
		defer cancel()
		if _, err := ids.imageSvc.ImageFsInfo(ctx, &cri.ImageFsInfoRequest{}); err != nil {
			klog.Errorf("probe: container runtime is unreachable: %v", err)
			return nil, status.Errorf(codes.FailedPrecondition, "container runtime is unreachable: %v", err)
		}
	}

	return &csi.ProbeResponse{
		Ready: &wrapperspb.BoolValue{Value: true},
	}, nil
}

func (ids *IdentityServer) GetPluginCapabilities(_ context.Context, _ *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}, nil
}

// Keep only unexported method
func (is *IdentityServer) mustEmbedUnimplementedIdentityServer() {}
