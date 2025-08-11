package main

import (
	"context"
	goflag "flag"
	"fmt"
	"net/url"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	flag "github.com/spf13/pflag"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend/containerd"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend/crio"
	"github.com/warm-metal/container-image-csi-driver/pkg/cri"
	csicommon "github.com/warm-metal/container-image-csi-driver/pkg/csi-common"
	"github.com/warm-metal/container-image-csi-driver/pkg/metrics"
	"github.com/warm-metal/container-image-csi-driver/pkg/secret"
	"github.com/warm-metal/container-image-csi-driver/pkg/watcher"
	"k8s.io/klog/v2"
)

const (
	driverName    = "container-image.csi.k8s.io"
	driverVersion = "v1.0.0"

	containerdScheme = "containerd"
	criOScheme       = "cri-o"

	nodeMode       = "node"
	controllerMode = "controller"
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
	icpConf = flag.String("image-credential-provider-config", "",
		"The path to the credential provider plugin config file.")
	icpBin = flag.String("image-credential-provider-bin-dir", "",
		"The path to the directory where credential provider plugin binaries are located.")
	nodePluginSA = flag.String("node-plugin-sa", "container-image-csi-driver",
		"The name of the ServiceAccount for pulling image.")
	enableCache = flag.Bool("enable-daemon-image-credential-cache", true,
		"Cache image pull secret from the daemon ServiceAccount.")
	asyncImagePullTimeout = flag.Duration("async-pull-timeout", 10*time.Minute,
		"Timeout for asynchronous image pulling. Only valid if --async-pull is enabled.")
	mode = flag.String("mode", nodeMode,
		fmt.Sprintf("Mode determines the role this instance plays. One of %q or %q.", nodeMode, controllerMode))
	watcherResyncPeriod = flag.Duration("watcher-resync-period", 10*time.Minute,
		"Resync period for the PVC watcher. Only valid in controller mode.")
	metricsPort = flag.Int("metrics-port", 8080,
		"Port for serving Prometheus metrics.")
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
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
	})

	if len(*mode) == 0 {
		klog.Fatalf("The mode of the driver is required.")
	}

	server := csicommon.NewNonBlockingGRPCServer()

	switch *mode {
	case nodeMode:
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

		var mounter backend.Mounter
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

		secretStore := secret.CreateStoreOrDie(*icpConf, *icpBin, *nodePluginSA, *enableCache)

		server.Start(*endpoint,
			NewIdentityServer(driverVersion),
			nil,
			NewNodeServer(driver, mounter, criClient, secretStore, *asyncImagePullTimeout))
	case controllerMode:
		watcher, err := watcher.New(context.Background(), *watcherResyncPeriod)
		if err != nil {
			klog.Fatalf("unable to create PVC watcher: %s", err)
		}

		defer watcher.Stop()

		server.Start(*endpoint,
			NewIdentityServer(driverVersion),
			NewControllerServer(driver, watcher),
			nil,
		)
	}

	metrics.StartMetricsServer(metrics.RegisterMetrics(), *metricsPort)
	server.Wait()
}
