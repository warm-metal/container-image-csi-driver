package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/warm-metal/csi-driver-image/pkg/test/utils"
	csicommon "github.com/warm-metal/csi-drivers/pkg/csi-common"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/credentialprovider"
)

func buildTestNodeServer(overrideTimeout *time.Duration) *NodeServer {
	criClient := &utils.MockImageServiceClient{
		PulledImages:  make(map[string]bool),
		ImagePullTime: time.Second * 5,
	}

	mounter := &utils.MockMounter{
		ImageSvcClient: *criClient,
		Mounted:        make(map[string]bool),
	}

	driver := csicommon.NewCSIDriver(driverName, driverVersion, "fake-node")
	if driver == nil {
		panic("driver was nil")
	}

	if overrideTimeout == nil {
		t := 2 * time.Minute
		overrideTimeout = &t
	}

	return NewNodeServer(driver, mounter, criClient, &testSecretStore{}, overrideTimeout)
}

func buildTestReq(image string, pullAlways bool, testPath string) *csi.NodePublishVolumeRequest {

	uid, err := uuid.NewRandom()
	if err != nil {
		panic(err)
	}

	return &csi.NodePublishVolumeRequest{
		VolumeId:   image,
		TargetPath: testPath,
		VolumeContext: map[string]string{
			// so that the test would always attempt to pull an image
			ctxKeyPullAlways: strconv.FormatBool(pullAlways),
			ctxKeyPodUid:     uid.String(),
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
			},
		},
	}
}

func buildTestNodeClient(sockPath string) csi.NodeClient {
	var conn *grpc.ClientConn

	addr, err := url.Parse(fmt.Sprintf("unix://%s", sockPath))
	if err != nil {
		panic(err)
	}

	conn, err = grpc.Dial(
		addr.Path,
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, targetPath string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", targetPath)
		}),
	)
	if err != nil {
		panic(err)
	}

	return csi.NewNodeClient(conn)
}

func TestNodePublishVolume(t *testing.T) {
	// based on kubelet's csi mounter plugin code
	// check https://github.com/kubernetes/kubernetes/blob/b06a31b87235784bad2858be62115049b6eb6bcd/pkg/volume/csi/csi_mounter.go#L111-L112
	timeout := 2 * time.Second

	req := buildTestReq("docker.io/library/redis:latest", true, "test-path")

	server := utils.BuildTestNonblockingGRPCServer()

	server.Start(
		nil,
		nil,
		buildTestNodeServer(nil))

	defer func() {
		klog.Info("server was stopped")
		server.Stop()
	}()

	nodeClient := buildTestNodeClient(server.SockPath())

	condFn := func() (done bool, err error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		resp, err := nodeClient.NodePublishVolume(ctx, req)
		if err != nil && strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
			klog.Errorf("context deadline exceeded; retrying: %v", err)
			return false, nil
		}
		if resp != nil {
			return true, nil
		}
		return false, fmt.Errorf("response from `NodePublishVolume` is nil")
	}

	err := wait.PollImmediate(
		timeout,
		30*time.Second,
		condFn)
	assert.NoError(t, err)
}

// // Check test/integration/node-server/README.md for how to run this test correctly
// func TestMetrics(t *testing.T) {

// 	// based on kubelet's csi mounter plugin code
// 	// check https://github.com/kubernetes/kubernetes/blob/b06a31b87235784bad2858be62115049b6eb6bcd/pkg/volume/csi/csi_mounter.go#L111-L112
// 	timeout := 10 * time.Second

// 	server := utils.BuildTestNonblockingGRPCServer()

// 	server.Start(
// 		nil,
// 		nil,
// 		buildTestNodeServer(nil))

// 	defer func() {
// 		klog.Info("server was stopped")
// 		server.Stop()
// 	}()

// 	metrics.StartMetricsServer(metrics.RegisterMetrics())

// 	nodeClient := buildTestNodeClient(server.SockPath())

// 	ctx, cancel := context.WithTimeout(context.Background(), 3*timeout)
// 	defer cancel()
// 	// wrong image id
// 	// adding INVALIDIMAGE suffix makes mock service client fail the pull
// 	wrongReq := buildTestReq("docker.io-doesnt-exist/library/redis-doesnt-exist:latest-INVALIDIMAGE",
// 		true, "wrong-test-path")

// 	r, err := nodeClient.NodePublishVolume(ctx, wrongReq)
// 	assert.Error(t, err)
// 	assert.Nil(t, r)

// 	req := buildTestReq("docker.io/library/redis:latest", true, "test-path")

// 	condFn := func() (done bool, err error) {
// 		ctx, cancel := context.WithTimeout(context.Background(), timeout)
// 		defer cancel()
// 		resp, err := nodeClient.NodePublishVolume(ctx, req)
// 		if err != nil && strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
// 			klog.Errorf("context deadline exceeded; retrying: %v", err)
// 			return false, nil
// 		}
// 		if resp != nil {
// 			return true, nil
// 		}
// 		return false, fmt.Errorf("response from `NodePublishVolume` is nil")
// 	}

// 	err = wait.PollImmediate(
// 		timeout,
// 		30*time.Second,
// 		condFn)
// 	assert.NoError(t, err)

// 	resp, err := http.Get("http://:8080/metrics")
// 	assert.NoError(t, err)
// 	assert.NotNil(t, resp)
// 	assert.Equal(t, http.StatusOK, resp.StatusCode)

// 	b1, err := io.ReadAll(resp.Body)
// 	assert.NoError(t, err)
// 	respBody := string(b1)
// 	assert.Contains(t, respBody, metrics.ImagePullTimeKey)
// 	assert.Contains(t, respBody, metrics.ImageMountTimeKey)
// 	assert.Contains(t, respBody, metrics.OperationErrorsCountKey)
// }

type testSecretStore struct{}

func (t *testSecretStore) GetDockerKeyring(ctx context.Context, secrets map[string]string) (credentialprovider.DockerKeyring, error) {
	return credentialprovider.UnionDockerKeyring{credentialprovider.NewDockerKeyring()}, nil
}
