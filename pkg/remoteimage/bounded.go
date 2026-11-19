package remoteimage

import (
	"context"

	"github.com/warm-metal/container-image-csi-driver/pkg/metrics"
	"golang.org/x/sync/semaphore"
)

// NewBoundedPuller wraps puller so that Pull only proceeds once a slot on sem
// has been acquired, bounding how many Pull calls run concurrently across
// all callers sharing sem. Acquiring is cancellable: if ctx passed to Pull is
// done before a slot frees up (e.g. the caller's own timeout, or the
// surrounding async-pull session timeout/shutdown), Acquire returns that
// context's error and the wrapped puller's Pull is never invoked.
//
// sem must not be nil; callers should only wrap with NewBoundedPuller when
// bounded parallelism is enabled (sem sized by --max-concurrent-image-pulls).
// When bounded parallelism is disabled, callers should use puller directly
// instead of wrapping it, so there is zero behavior change from today.
func NewBoundedPuller(puller Puller, sem *semaphore.Weighted) Puller {
	return &boundedPuller{Puller: puller, sem: sem}
}

type boundedPuller struct {
	Puller
	sem *semaphore.Weighted
}

// Pull acquires a slot on sem before delegating to the wrapped Puller's
// Pull, and releases the slot once that call returns (success or error).
// While waiting for a slot, the pull is reflected in the
// warm_metal_image_pull_queued gauge; while actually pulling, it is
// reflected in warm_metal_image_pull_inflight.
func (b *boundedPuller) Pull(ctx context.Context) error {
	metrics.ImagePullQueued.Inc()
	err := b.sem.Acquire(ctx, 1)
	metrics.ImagePullQueued.Dec()
	if err != nil {
		// ctx was cancelled/timed out while this pull was queued waiting for
		// a slot; the wrapped Pull is never attempted.
		return err
	}

	metrics.ImagePullInFlight.Inc()
	defer func() {
		metrics.ImagePullInFlight.Dec()
		b.sem.Release(1)
	}()

	return b.Puller.Pull(ctx)
}
