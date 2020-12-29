package main

import (
	"flag"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/glog"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
)

var (
	endpoint = flag.String("endpoint", "unix:///csi/csi.sock", "endpoint")
	nodeID = flag.String("node", "", "node ID")
)

const (
	driverName = "csi-image.warm-metal.tech"
	driverVersion = "v1.0.0"
)

func main() {
	defer glog.Flush()
	flag.Parse()
	driver := csicommon.NewCSIDriver(driverName, driverVersion, *nodeID)
	//driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
	//	csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
	//	csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
	//	//csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
	//	//csi.ControllerServiceCapability_RPC_GET_CAPACITY,
	//	csi.ControllerServiceCapability_RPC_PUBLISH_READONLY,
	//	//csi.ControllerServiceCapability_RPC_LIST_VOLUMES_PUBLISHED_NODES,
	//	//csi.ControllerServiceCapability_RPC_VOLUME_CONDITION,
	//	//csi.ControllerServiceCapability_RPC_GET_VOLUME,
	//})

	driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
	})

	server := csicommon.NewNonBlockingGRPCServer()
	server.Start(*endpoint,
		csicommon.NewDefaultIdentityServer(driver),
		nil,
		//&ControllerServer{csicommon.NewDefaultControllerServer(driver)},
		&NodeServer{csicommon.NewDefaultNodeServer(driver)})
	server.Wait()
}
