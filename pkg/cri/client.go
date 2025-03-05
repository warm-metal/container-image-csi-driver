package cri

import (
	"context"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

	// Wrap the dialer to match the expected context-aware signature
	contextDialer := func(ctx context.Context, addr string) (net.Conn, error) {
		return dialer(addr, connectionTimeout)
	}

	// Use grpc.Dial with insecure credentials
	conn, err := grpc.DialContext(
		ctx,
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(contextDialer),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMsgSize)),
		grpc.WithBlock(),
	)
	if err != nil {
		klog.Errorf("Connect remote image service %s failed: %v", addr, err)
		return nil, err
	}

	return cri.NewImageServiceClient(conn), nil
}
