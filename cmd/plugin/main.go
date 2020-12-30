package main

import (
	"flag"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/glog"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	"github.com/warm-metal/csi-driver-image/pkg/backend/containerd"
)

var (
	endpoint       = flag.String("endpoint", "unix:///csi/csi.sock", "endpoint")
	nodeID         = flag.String("node", "", "node ID")
	containerdSock = flag.String(
		"containerd-addr", "unix:///var/run/containerd/containerd.sock", "endpoint of containerd")
	defaultContainerdNamespace = flag.String(
		"containerd-default-namespace", "k8s",
		`the default namespace containerd used in the cluster. It usually is "docker" if docker is used as runtime, or "k8s" if CRI is used.`)
)

const (
	driverName    = "csi-image.warm-metal.tech"
	driverVersion = "v1.0.0"
)

func main() {
	if err := flag.Set("logtostderr", "true"); err != nil {
		panic(err)
	}

	defer glog.Flush()
	flag.Parse()
	driver := csicommon.NewCSIDriver(driverName, driverVersion, *nodeID)
	driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
	})

	server := csicommon.NewNonBlockingGRPCServer()
	server.Start(*endpoint,
		csicommon.NewDefaultIdentityServer(driver),
		nil,
		//&ControllerServer{csicommon.NewDefaultControllerServer(driver)},
		&nodeServer{
			DefaultNodeServer: csicommon.NewDefaultNodeServer(driver),
			mounter: containerd.NewMounter(*containerdSock, *defaultContainerdNamespace),
		},
	)
	server.Wait()
}
