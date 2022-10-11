package main

import (
	goflag "flag"
	"fmt"
	"net/url"

	"github.com/container-storage-interface/spec/lib/go/csi"
	flag "github.com/spf13/pflag"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/backend/containerd"
	"github.com/warm-metal/csi-driver-image/pkg/backend/crio"
	"github.com/warm-metal/csi-driver-image/pkg/cri"
	"github.com/warm-metal/csi-driver-image/pkg/secret"
	csicommon "github.com/warm-metal/csi-drivers/pkg/csi-common"
	"k8s.io/klog/v2"

	"time"
)

const (
	driverName    = "csi-image.warm-metal.tech"
	driverVersion = "v1.0.0"

	containerdScheme = "containerd"
	criOScheme       = "cri-o"
)

var (
	endpoint = flag.String("endpoint", "unix:///csi/csi.sock",
		"The endpoint of the CSI driver usually shared with the driver registrar.")
	nodeID         = flag.String("node", "", "The node name that driver currently runs on.")
	containerdSock = flag.String(
		"containerd-addr", "",
		"The unix socket of containerd. Deprecated. Use --runtime-addr instead.")
	runtimeAddr = flag.String(
		"runtime-addr", "",
		fmt.Sprintf("The unix socket of the container runtime. Currently both containerd and cri-o are supported."+
			"Users need to replace the leading %q with %q or %q to indicate the working runtime.",
			"unix", containerdScheme, criOScheme),
	)
	imageCredentialProviderConfigFile = flag.String("image-credential-provider-config", "",
		"The path to the credential provider plugin config file.")
	imageCredentialProviderBinDir = flag.String("image-credential-provider-bin-dir", "",
		"The path to the directory where credential provider plugin binaries are located.")
)

func main() {
	klog.InitFlags(nil)
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	if err := flag.Set("logtostderr", "true"); err != nil {
		panic(err)
	}

	flag.Parse()
	defer klog.Flush()

	driver := csicommon.NewCSIDriver(driverName, driverVersion, *nodeID)
	driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
	})
	driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_UNKNOWN,
	})

	if len(*runtimeAddr) == 0 {
		if len(*containerdSock) == 0 {
			klog.Fatalf("The unit socket of container runtime is required.")
		}

		klog.Warning("--containerd-addr is deprecated. Use --runtime-addr instead.")
		addr, err := url.Parse(*containerdSock)
		if err != nil {
			klog.Fatalf("invalid runtime address: %s", err)
		}
		addr.Scheme = containerdScheme
		*runtimeAddr = addr.String()
	}

	var mounter *backend.SnapshotMounter
	if len(*runtimeAddr) > 0 {
		addr, err := url.Parse(*runtimeAddr)
		if err != nil {
			klog.Fatalf("invalid runtime address: %s", err)
		}

		klog.Infof("runtime %s at %q", addr.Scheme, addr.Path)
		switch addr.Scheme {
		case containerdScheme:
			mounter = containerd.NewMounter(addr.Path)
		case criOScheme:
			mounter = crio.NewMounter(addr.Path)
		default:
			klog.Fatalf("unknown container runtime %q", addr.Scheme)
		}

		addr.Scheme = "unix"
		*runtimeAddr = addr.String()
	}

	criClient, err := cri.NewRemoteImageService(*runtimeAddr, time.Second)
	if err != nil {
		klog.Fatalf(`unable to connect to cri daemon "%s": %s`, *endpoint, err)
	}

	server := csicommon.NewNonBlockingGRPCServer()
	server.Start(*endpoint,
		csicommon.NewDefaultIdentityServer(driver),
		&controllerServer{csicommon.NewDefaultControllerServer(driver)},
		&nodeServer{
			DefaultNodeServer: csicommon.NewDefaultNodeServer(driver),
			mounter:           mounter,
			imageSvc:          criClient,
			secretCache:       secret.CreateCacheOrDie(*imageCredentialProviderConfigFile, *imageCredentialProviderBinDir),
		},
	)
	server.Wait()
}
