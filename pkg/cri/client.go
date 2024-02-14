package cri

import (
	"context"
	"time"

	"google.golang.org/grpc"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/util"
)

const maxMsgSize = 1024 * 1024 * 16

func NewRemoteImageService(endpoint string, connectionTimeout time.Duration) (cri.ImageServiceClient, error) {
	addr, dialer, err := util.GetAddressAndDialer(endpoint)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectionTimeout)
	defer cancel()

	conn, err := grpc.DialContext(
		ctx, addr, grpc.WithInsecure(), grpc.WithContextDialer(dialer),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMsgSize)),
		grpc.WithBlock(),
	)

	if err != nil {
		klog.Errorf("Connect remote image service %s failed: %v", addr, err)
		return nil, err
	}

	return cri.NewImageServiceClient(conn), nil
}
