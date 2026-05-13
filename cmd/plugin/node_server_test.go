package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend/containerd"
	"github.com/warm-metal/container-image-csi-driver/pkg/cri"
	csicommon "github.com/warm-metal/container-image-csi-driver/pkg/csi-common"
	"github.com/warm-metal/container-image-csi-driver/pkg/metrics"
	"github.com/warm-metal/container-image-csi-driver/pkg/secret"
	"github.com/warm-metal/container-image-csi-driver/pkg/test/utils"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

func TestNodePublishVolumeAsync(t *testing.T) {
	criClient := &utils.MockImageServiceClient{
		PulledImages:  make(map[string]bool),
		ImagePullTime: time.Second * 5,
	}

	mounter := &utils.MockMounter{
		ImageSvcClient: *criClient,
		Mounted:        make(map[string]bool),
	}

	driver := csicommon.NewCSIDriver(driverName, driverVersion, "fake-node")
	assert.NotNil(t, driver)

	asyncImagePulls := 15 * time.Minute //TODO: determine intended value for this in the context of this test
	ns := NewNodeServer(driver, mounter, criClient, &testSecretStore{}, asyncImagePulls)

	// based on kubelet's csi mounter plugin code
	// check https://github.com/kubernetes/kubernetes/blob/b06a31b87235784bad2858be62115049b6eb6bcd/pkg/volume/csi/csi_mounter.go#L111-L112
	timeout := 100 * time.Millisecond

	volId := "docker.io/library/redis:latest"
	target := "test-path"
	req := &csi.NodePublishVolumeRequest{
		VolumeId:   volId,
		TargetPath: target,
		VolumeContext: map[string]string{
			// so that the test would always attempt to pull an image
			ctxKeyPullAlways: "true",
			// to see improved logs
			"pod-name":  "test-pod",
			"namespace": "test-namespace",
			"uid":       "test-uid",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
			},
		},
	}

	server := csicommon.NewNonBlockingGRPCServer()

	endpoint := "unix:///tmp/csi.sock"

	// automatically deleted when the server is stopped
	f, err := os.Create("/tmp/csi.sock")
	assert.NoError(t, err)
	assert.NotNil(t, f)

	defer os.Remove("/tmp/csi.sock")

	addr, err := url.Parse("unix:///tmp/csi.sock")
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {

		server.Start(endpoint,
			nil,
			nil,
			ns)
		// wait for the GRPC server to start
		wg.Done()
		server.Wait()
	}()

	// give some time for server to start
	time.Sleep(2 * time.Second)
	defer func() {
		klog.Info("server was stopped")
		server.Stop()
	}()

	wg.Wait()
	var conn *grpc.ClientConn

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

	assert.NoError(t, err)
	assert.NotNil(t, conn)

	nodeClient := csi.NewNodeClient(conn)
	assert.NotNil(t, nodeClient)

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

	err = wait.PollImmediate(
		timeout,
		30*time.Second,
		condFn)
	assert.NoError(t, err)

	// give some time before stopping the server
	time.Sleep(5 * time.Second)

	// unmount if the volume is already mounted
	c, ca := context.WithTimeout(context.Background(), time.Second*10)
	defer ca()

	err = mounter.Unmount(c, volId, backend.MountTarget(target))
	assert.NoError(t, err)
}

// Check test/integration/node-server/README.md for how to run this test correctly
func TestNodePublishVolumeSync(t *testing.T) {
	criClient := &utils.MockImageServiceClient{
		PulledImages:  make(map[string]bool),
		ImagePullTime: time.Second * 5,
	}
	mounter := &utils.MockMounter{
		ImageSvcClient: *criClient,
		Mounted:        make(map[string]bool),
	}

	driver := csicommon.NewCSIDriver(driverName, driverVersion, "fake-node")
	assert.NotNil(t, driver)

	asyncImagePulls := 0 * time.Minute //TODO: determine intended value for this in the context of this test
	ns := NewNodeServer(driver, mounter, criClient, &testSecretStore{}, asyncImagePulls)

	// based on kubelet's csi mounter plugin code
	// check https://github.com/kubernetes/kubernetes/blob/b06a31b87235784bad2858be62115049b6eb6bcd/pkg/volume/csi/csi_mounter.go#L111-L112
	timeout := 100 * time.Millisecond

	volId := "docker.io/library/redis:latest"
	target := "test-path"
	req := &csi.NodePublishVolumeRequest{
		VolumeId:   volId,
		TargetPath: target,
		VolumeContext: map[string]string{
			// so that the test would always attempt to pull an image
			ctxKeyPullAlways: "true",
			// to see improved logs
			"pod-name":  "test-pod",
			"namespace": "test-namespace",
			"uid":       "test-uid",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
			},
		},
	}

	server := csicommon.NewNonBlockingGRPCServer()

	endpoint := "unix:///tmp/csi.sock"

	// automatically deleted when the server is stopped
	f, err := os.Create("/tmp/csi.sock")
	assert.NoError(t, err)
	assert.NotNil(t, f)

	defer os.Remove("/tmp/csi/csi.sock")

	addr, err := url.Parse("unix:///tmp/csi.sock")
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {

		server.Start(endpoint,
			nil,
			nil,
			ns)
		// wait for the GRPC server to start
		wg.Done()
		server.Wait()
	}()

	// give some time for server to start
	time.Sleep(2 * time.Second)
	defer func() {
		klog.Info("server was stopped")
		server.Stop()
	}()

	wg.Wait()
	var conn *grpc.ClientConn

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

	assert.NoError(t, err)
	assert.NotNil(t, conn)

	nodeClient := csi.NewNodeClient(conn)
	assert.NotNil(t, nodeClient)

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

	err = wait.PollImmediate(
		timeout,
		30*time.Second,
		condFn)

	assert.Error(t, err)
	assert.ErrorContains(t, err, "timed out waiting for the condition")

	// give some time before stopping the server
	time.Sleep(5 * time.Second)

	// unmount if the volume is already mounted
	c, ca := context.WithTimeout(context.Background(), time.Second*10)
	defer ca()

	err = mounter.Unmount(c, volId, backend.MountTarget(target))
	assert.Error(t, err)
	assert.ErrorContains(t, err, "not found")
}

// Check test/integration/node-server/README.md for how to run this test correctly
func TestMetrics(t *testing.T) {
	socketAddr := "unix:///run/containerd/containerd.sock"
	addr, err := url.Parse(socketAddr)
	assert.NoError(t, err)

	criClient, err := cri.NewRemoteImageService(socketAddr, time.Minute)
	assert.NoError(t, err)
	assert.NotNil(t, criClient)

	mounter := containerd.NewMounter(addr.Path)
	assert.NotNil(t, mounter)

	driver := csicommon.NewCSIDriver(driverName, driverVersion, "fake-node")
	assert.NotNil(t, driver)

	asyncImagePulls := 15 * time.Minute //TODO: determine intended value for this in the context of this test
	ns := NewNodeServer(driver, mounter, criClient, &testSecretStore{}, asyncImagePulls)

	// based on kubelet's csi mounter plugin code
	// check https://github.com/kubernetes/kubernetes/blob/b06a31b87235784bad2858be62115049b6eb6bcd/pkg/volume/csi/csi_mounter.go#L111-L112
	timeout := 10 * time.Second

	server := csicommon.NewNonBlockingGRPCServer()

	addr, err = url.Parse(*endpoint)
	assert.NoError(t, err)

	os.Remove("/csi/csi.sock")

	// automatically deleted when the server is stopped
	f, err := os.Create("/csi/csi.sock")
	assert.NoError(t, err)
	assert.NotNil(t, f)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		metrics.StartMetricsServer(metrics.RegisterMetrics(), 8080)

		server.Start(*endpoint,
			nil,
			nil,
			ns)
		// wait for the GRPC server to start
		wg.Done()
		server.Wait()
	}()

	// give some time for server to start
	time.Sleep(2 * time.Second)
	defer func() {
		klog.Info("server was stopped")
		server.Stop()
	}()

	wg.Wait()
	var conn *grpc.ClientConn

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

	assert.NoError(t, err)
	assert.NotNil(t, conn)

	nodeClient := csi.NewNodeClient(conn)
	assert.NotNil(t, nodeClient)

	ctx, cancel := context.WithTimeout(context.Background(), 3*timeout)
	defer cancel()
	// wrong image id
	wrongVolId := "docker.io-doesnt-exist/library/redis-doesnt-exist:latest"
	wrongTargetPath := "wrong-test-path"
	wrongReq := &csi.NodePublishVolumeRequest{
		VolumeId:   wrongVolId,
		TargetPath: wrongTargetPath,
		VolumeContext: map[string]string{
			// so that the test would always attempt to pull an image
			ctxKeyPullAlways: "true",
			// to see improved logs
			"pod-name":  "test-pod",
			"namespace": "test-namespace",
			"uid":       "test-uid",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
			},
		},
	}

	r, err := nodeClient.NodePublishVolume(ctx, wrongReq)
	assert.Error(t, err)
	assert.Nil(t, r)

	volId := "docker.io/library/redis:latest"
	targetPath := "test-path"
	req := &csi.NodePublishVolumeRequest{
		VolumeId:   volId,
		TargetPath: targetPath,
		VolumeContext: map[string]string{
			// so that the test would always attempt to pull an image
			ctxKeyPullAlways: "true",
			// to see improved logs
			"pod-name":  "test-pod",
			"namespace": "test-namespace",
			"uid":       "test-uid",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
			},
		},
	}

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

	err = wait.PollImmediate(
		timeout,
		30*time.Second,
		condFn)
	assert.NoError(t, err)

	resp, err := http.Get("http://:8080/metrics")
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	b1, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	respBody := string(b1)
	assert.Contains(t, respBody, metrics.ImagePullTimeKey)
	assert.Contains(t, respBody, metrics.ImagePullTimeHist)
	assert.Contains(t, respBody, metrics.OperationErrorsCountKey)

	// give some time before stopping the server
	time.Sleep(5 * time.Second)

	// unmount if the volume is already mounted
	c, ca := context.WithTimeout(context.Background(), time.Second*10)
	defer ca()

	err = mounter.Unmount(c, volId, backend.MountTarget(targetPath))
	assert.NoError(t, err)
}

type testSecretStore struct{}

func (t *testSecretStore) GetDockerKeyring(ctx context.Context, secrets map[string]string) (secret.DockerKeyring, error) {
	return secret.NewDockerKeyring(), nil
}
