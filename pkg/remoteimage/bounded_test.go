package remoteimage

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/semaphore"
)

// mockPuller is a minimal Puller used only to exercise boundedPuller's
// concurrency control. It records, via the shared counters passed in, how
// many instances are inside Pull() at once (the "current" count), and the
// maximum value that count ever reached (the "high water mark").
type mockPuller struct {
	image     string
	duration  time.Duration
	current   *atomic.Int32
	highWater *atomic.Int32
	err       error
}

func (m *mockPuller) Pull(ctx context.Context) error {
	cur := m.current.Add(1)
	defer m.current.Add(-1)
	for {
		hw := m.highWater.Load()
		if cur <= hw || m.highWater.CompareAndSwap(hw, cur) {
			break
		}
	}

	select {
	case <-time.After(m.duration):
		return m.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *mockPuller) ImageWithTag() string    { return m.image }
func (m *mockPuller) ImageWithoutTag() string { return m.image }
func (m *mockPuller) ImageSize(ctx context.Context) (int, error) {
	return 0, nil
}

// TestBoundedPuller_RespectsConcurrencyLimit starts many pulls at once
// against pullers wrapped with the same semaphore and asserts that the
// number of pulls actually running concurrently (inside Pull()) never
// exceeds the configured limit, while all of them still eventually
// complete successfully.
func TestBoundedPuller_RespectsConcurrencyLimit(t *testing.T) {
	const limit = 2
	const totalPulls = 7

	sem := semaphore.NewWeighted(limit)
	var current, highWater atomic.Int32

	var wg sync.WaitGroup
	errs := make([]error, totalPulls)
	for i := 0; i < totalPulls; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mp := &mockPuller{
				image:     fmt.Sprintf("image-%d", i),
				duration:  100 * time.Millisecond,
				current:   &current,
				highWater: &highWater,
			}
			bp := NewBoundedPuller(mp, sem)
			errs[i] = bp.Pull(context.Background())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "pull %d should have succeeded", i)
	}
	assert.LessOrEqual(t, int(highWater.Load()), limit,
		"no more than %d pulls should ever run concurrently, saw %d", limit, highWater.Load())
	assert.Equal(t, int32(limit), highWater.Load(),
		"with %d pulls competing for %d slots, the limit should actually be reached", totalPulls, limit)
}

// TestBoundedPuller_QueuedPullReleasedByContextCancellation verifies that a
// pull blocked waiting for a free slot gives up cleanly (without ever
// calling the wrapped Puller's Pull) when its context is cancelled while
// still queued, and that doing so does not consume or leak a semaphore slot.
func TestBoundedPuller_QueuedPullReleasedByContextCancellation(t *testing.T) {
	sem := semaphore.NewWeighted(1)
	var current, highWater atomic.Int32

	// Occupy the only slot for longer than the queued caller will wait.
	holder := &mockPuller{image: "holder", duration: 500 * time.Millisecond, current: &current, highWater: &highWater}
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		err := NewBoundedPuller(holder, sem).Pull(context.Background())
		assert.NoError(t, err)
	}()

	time.Sleep(50 * time.Millisecond) // let holder acquire the slot first

	queuedCalled := &atomic.Bool{}
	queued := &trackingPuller{mockPuller: mockPuller{image: "queued", duration: 10 * time.Millisecond, current: &current, highWater: &highWater}, called: queuedCalled}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := NewBoundedPuller(queued, sem).Pull(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "queued pull should fail with the caller's context error")
	assert.False(t, queuedCalled.Load(), "the wrapped Pull must never be invoked for a pull that never acquired a slot")

	<-holderDone
	// The slot must be free again now that the holder finished; this would
	// deadlock/timeout if the earlier cancelled Acquire had leaked a slot.
	acquireCtx, acquireCancel := context.WithTimeout(context.Background(), time.Second)
	defer acquireCancel()
	assert.NoError(t, sem.Acquire(acquireCtx, 1), "semaphore slot should not have leaked")
	sem.Release(1)
}

// trackingPuller wraps mockPuller to record whether Pull was ever actually
// invoked, so tests can assert the wrapped puller was skipped entirely for
// callers that never acquired a slot.
type trackingPuller struct {
	mockPuller
	called *atomic.Bool
}

func (t *trackingPuller) Pull(ctx context.Context) error {
	t.called.Store(true)
	return t.mockPuller.Pull(ctx)
}
