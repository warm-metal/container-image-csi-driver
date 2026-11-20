package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/distribution/reference"
	"github.com/stretchr/testify/assert"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend"
	csicommon "github.com/warm-metal/container-image-csi-driver/pkg/csi-common"
	"github.com/warm-metal/container-image-csi-driver/pkg/test/utils"
	"google.golang.org/grpc"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

// mutexMounter wraps utils.MockMounter with a mutex. utils.MockMounter
// is only ever exercised by today's existing tests one call at a time, so
// its internal map is unsynchronized; this test issues many concurrent
// NodePublishVolume calls (that is the whole point of a concurrency test),
// so it needs a thread-safe mounter. Scoped to this test file to avoid
// changing the shared test helper's behavior for other tests.
type mutexMounter struct {
	mu    sync.Mutex
	inner *utils.MockMounter
}

func (m *mutexMounter) Mount(ctx context.Context, volumeId string, target backend.MountTarget, image reference.Named, ro bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inner.Mount(ctx, volumeId, target, image, ro)
}

func (m *mutexMounter) Unmount(ctx context.Context, volumeId string, target backend.MountTarget) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inner.Unmount(ctx, volumeId, target)
}

func (m *mutexMounter) ImageExists(ctx context.Context, image reference.Named) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inner.ImageExists(ctx, image)
}

// trackedPullImage wraps utils.MockImageServiceClient to count concurrent
// PullImage calls, independent of the production boundedPuller decorator
// under test. Every other cri.ImageServiceClient method is delegated
// automatically via the embedded mock.
type trackedPullImage struct {
	*utils.MockImageServiceClient
	current   *atomic.Int32
	highWater *atomic.Int32
}

func (t *trackedPullImage) PullImage(ctx context.Context, in *criapi.PullImageRequest, opts ...grpc.CallOption) (*criapi.PullImageResponse, error) {
	cur := t.current.Add(1)
	defer t.current.Add(-1)
	for {
		hw := t.highWater.Load()
		if cur <= hw || t.highWater.CompareAndSwap(hw, cur) {
			break
		}
	}
	return t.MockImageServiceClient.PullImage(ctx, in, opts...)
}

// newNodeServerForConcurrencyTest builds a NodeServer wired exactly how
// NewNodeServer in main.go wires it, using the given
// maxConcurrentImagePulls, against a mock CRI client wrapped in
// trackedPullImage so tests can observe real PullImage concurrency.
func newNodeServerForConcurrencyTest(t *testing.T, maxConcurrentImagePulls int, pullDuration time.Duration) (ns *NodeServer, current, highWater *atomic.Int32) {
	criClient := &utils.MockImageServiceClient{
		PulledImages:  make(map[string]bool),
		ImagePullTime: pullDuration,
	}
	mounter := &mutexMounter{inner: &utils.MockMounter{
		ImageSvcClient: *criClient,
		Mounted:        make(map[string]bool),
	}}
	current = &atomic.Int32{}
	highWater = &atomic.Int32{}
	tracked := &trackedPullImage{
		MockImageServiceClient: criClient,
		current:                current,
		highWater:              highWater,
	}

	driver := csicommon.NewCSIDriver(driverName, driverVersion, "fake-node")
	assert.NotNil(t, driver)

	// 15m async timeout so none of the pulls in this test (which complete in
	// well under a second) ever hit the async-pull timeout path; this test
	// is only about the concurrency bound, not about async session timing.
	ns = NewNodeServer(driver, mounter, tracked, &testSecretStore{}, 15*time.Minute, maxConcurrentImagePulls)
	return ns, current, highWater
}

func publishDistinctImages(t *testing.T, ns *NodeServer, totalImages int, pathSuffix string) []error {
	var wg sync.WaitGroup
	errs := make([]error, totalImages)
	for i := 0; i < totalImages; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := &csi.NodePublishVolumeRequest{
				VolumeId:   fmt.Sprintf("docker.io/library/image-%d:latest", i),
				TargetPath: fmt.Sprintf("/tmp/concurrency-test-%s-%d-%d", pathSuffix, i, time.Now().UnixNano()),
				VolumeContext: map[string]string{
					ctxKeyPullAlways: "true",
				},
				VolumeCapability: &csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
					},
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := ns.NodePublishVolume(ctx, req)
			errs[i] = err
		}(i)
	}
	wg.Wait()
	return errs
}

// TestNodeServer_MaxConcurrentImagePulls_BoundsConcurrency verifies that
// when --max-concurrent-image-pulls is set, concurrent NodePublishVolume
// calls for distinct images never run more simultaneous PullImage calls
// than the configured limit, and that every call still eventually
// succeeds. This exercises the real NewNodeServer wiring end-to-end
// (NewNodeServer -> newPuller -> remoteimage.NewBoundedPuller), not just
// the decorator in isolation.
func TestNodeServer_MaxConcurrentImagePulls_BoundsConcurrency(t *testing.T) {
	const limit = 2
	const totalImages = 6
	const pullDuration = 150 * time.Millisecond

	ns, _, highWater := newNodeServerForConcurrencyTest(t, limit, pullDuration)

	errs := publishDistinctImages(t, ns, totalImages, "bounded")

	for i, err := range errs {
		assert.NoError(t, err, "NodePublishVolume %d should have succeeded", i)
	}
	assert.LessOrEqual(t, int(highWater.Load()), limit,
		"no more than %d PullImage calls should ever run concurrently, saw %d", limit, highWater.Load())
	assert.Equal(t, int32(limit), highWater.Load(),
		"with %d images competing for %d slots, the limit should actually be reached", totalImages, limit)

	klog.Infof("TestNodeServer_MaxConcurrentImagePulls_BoundsConcurrency: observed max concurrent PullImage calls = %d (limit=%d)", highWater.Load(), limit)
}

// TestNodeServer_MaxConcurrentImagePulls_ZeroMeansUnbounded verifies that
// the default (0) preserves today's behavior: all pulls are allowed to run
// at once with no queueing.
func TestNodeServer_MaxConcurrentImagePulls_ZeroMeansUnbounded(t *testing.T) {
	const totalImages = 6
	const pullDuration = 150 * time.Millisecond

	ns, _, highWater := newNodeServerForConcurrencyTest(t, 0, pullDuration)

	errs := publishDistinctImages(t, ns, totalImages, "unbounded")

	for i, err := range errs {
		assert.NoError(t, err, "NodePublishVolume %d should have succeeded", i)
	}
	assert.Equal(t, int32(totalImages), highWater.Load(),
		"with max-concurrent-image-pulls=0 (unbounded), all %d pulls should run concurrently", totalImages)
}
