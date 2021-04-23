package main

import (
	"flag"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/warm-metal/csi-driver-image/pkg/backend/containerd"
	"github.com/warm-metal/csi-driver-image/pkg/cri"
	"github.com/warm-metal/csi-drivers/pkg/csi-common"
	"k8s.io/klog/v2"

	"os"
	"time"
)

var (
	endpoint       = flag.String("endpoint", "unix:///csi/csi.sock", "endpoint")
	nodeID         = flag.String("node", "", "node ID")
	containerdSock = flag.String(
		"containerd-addr", "unix:///var/run/containerd/containerd.sock", "endpoint of containerd")
)

const (
	driverName    = "csi-image.warm-metal.tech"
	driverVersion = "v1.0.0"
)

func main() {
	klog.InitFlags(nil)
	if err := flag.Set("logtostderr", "true"); err != nil {
		panic(err)
	}

	defer klog.Flush()
	flag.Parse()
	driver := csicommon.NewCSIDriver(driverName, driverVersion, *nodeID)
	driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
	})
	driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_UNKNOWN,
	})

	criClient, err := cri.NewRemoteImageService(*containerdSock, time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, `fail to connect to cri daemon "%s": %s`, *endpoint, err)
		os.Exit(1)
	}

	server := csicommon.NewNonBlockingGRPCServer()
	server.Start(*endpoint,
		csicommon.NewDefaultIdentityServer(driver),
		&controllerServer{csicommon.NewDefaultControllerServer(driver)},
		&nodeServer{
			DefaultNodeServer: csicommon.NewDefaultNodeServer(driver),
			mounter:           containerd.NewMounter(*containerdSock),
			imageSvc:          criClient,
		},
	)
	server.Wait()
}
