# Bounded Parallelism for Image Pulls

## Problem

The node plugin has no limit on how many distinct images it will pull in
parallel on a single node. Every `NodePublishVolume` call that needs a pull
triggers its own concurrent CRI `PullImage` call, in both of the driver's
pull code paths:

- **Sync mode** (`cmd/plugin/node_server.go`): `NodePublishVolume` calls
  `puller.Pull(ctx)` directly. Kubelet can have many such calls in flight on
  one node at once.
- **Async mode** (`pkg/remoteimageasync/puller.go`): `RunPullerLoop` spawns a
  new goroutine per distinct image with no cap. The existing `sessionMap`
  only dedupes concurrent requests for the *same* image — it does nothing to
  limit how many *different* images pull at once.

When several pods with large, distinct images land on the same node at once
(a node replacement, a bulk deploy, a batch of scheduled jobs), all of their
pulls compete for the node's network/disk bandwidth simultaneously. Each pull
slows down roughly in proportion to how many are running concurrently. If
that pushes pulls past kubelet's mount-RPC timeout, kubelet cancels and
retries — but since all the pulls started together and are equally starved,
they tend to miss the timeout together too, so kubelet retries them all
together, reproducing the same contention (thundering herd) without making
progress.

Confirmed in production: a single node was observed running up to 56
concurrent pull sessions at once, with 44% of distinct images exceeding even
a 10-minute `--async-pull-timeout` without completing — not because
async-pull/session-continuation was malfunctioning (every timeout fired at
precisely 600.00s, as designed), but purely from bandwidth contention with
nothing bounding how many pulls run at once.

## Solution

Add `--max-concurrent-image-pulls` (default `0`, unbounded) and wrap the
`Puller` with a decorator that acquires a slot on a shared, node-wide,
context-aware weighted semaphore before allowing the real pull to proceed,
releasing the slot when the pull returns.

```
NodePublishVolume
      │
      ▼
NodeServer.newPuller(namedRef, keyring)
      │
      ├─ pullSemaphore == nil (flag disabled, default) ─► remoteimage.Puller (unwrapped)
      │
      └─ pullSemaphore != nil (flag enabled) ──────────► remoteimage.BoundedPuller
                                                               │ wraps Puller + shared *semaphore.Weighted
                                                               ▼
                                            both the sync path (puller.Pull(ctx))
                                            and the async path (asyncImagePuller.StartPull(...))
                                            call Pull() on this same wrapped value
```

`NodeServer.newPuller(...)` is the single point where both pull paths obtain
their `Puller`, so one flag and one semaphore bound both without any change
to the existing session/dedup logic in `pkg/remoteimageasync`. When the flag
is `0` (the default), `newPuller` returns the `Puller` unwrapped — zero
behavior change for existing deployments.

### Why a decorator over a worker pool

A fixed-size worker pool inside `RunPullerLoop` was considered, but it would
only bound the async path — the sync path's direct `puller.Pull(ctx)` call
would remain fully unbounded, requiring a second mechanism to cover it. The
decorator approach covers both paths with a single wrap point because both
already go through the same `Puller` interface.

## Algorithm

The semaphore is `golang.org/x/sync/semaphore.Weighted` (promoted here from
an indirect to a direct dependency; no new transitive deps). Every pull
calls `Acquire(ctx, 1)`. Internally this package maintains a
`container/list`-based FIFO queue of blocked callers:

- If a slot is free **and** nobody is already queued, `Acquire` grants the
  slot immediately — no queueing overhead when demand is below the limit.
- Otherwise, the caller is appended to the **tail** of the FIFO wait list and
  blocks on either its own `ctx.Done()` or a per-waiter `ready` channel.
- On `Release`, the semaphore walks the wait list **from the front** and
  hands the freed slot to the single oldest waiter — strict FIFO, no
  starvation, no priority.
- If a queued caller's `ctx` is cancelled (caller timeout, async-session
  timeout, or process shutdown) before it reaches the front of the queue, it
  is removed from the list and `Acquire` returns `ctx.Err()` — the wrapped
  `Pull` is never invoked for that caller, and no slot is leaked.

A slot is only released once the real `PullImage` call actually returns —
timing out the *outer* async-pull session does not free the slot early; the
real call must return first (with whatever error the CRI client surfaces for
a cancelled context).

## Implementation

**`pkg/remoteimage/bounded.go`** (new)

```go
func NewBoundedPuller(puller Puller, sem *semaphore.Weighted) Puller {
	return &boundedPuller{Puller: puller, sem: sem}
}

type boundedPuller struct {
	Puller
	sem *semaphore.Weighted
}

func (b *boundedPuller) Pull(ctx context.Context) error {
	metrics.ImagePullQueued.Inc()
	err := b.sem.Acquire(ctx, 1)
	metrics.ImagePullQueued.Dec()
	if err != nil {
		return err // never acquired a slot; wrapped Pull is never called
	}

	metrics.ImagePullInFlight.Inc()
	defer func() {
		metrics.ImagePullInFlight.Dec()
		b.sem.Release(1)
	}()

	return b.Puller.Pull(ctx)
}
```

`boundedPuller` embeds the real `Puller`, so every method other than `Pull`
(`ImageWithTag`, `ImageWithoutTag`, `ImageSize`) passes through unmodified.

**`cmd/plugin/node_server.go`**

- `NodeServer` gains a `pullSemaphore *semaphore.Weighted` field, constructed
  in `NewNodeServer` only when `maxConcurrentImagePulls > 0`.
- `NodeServer.newPuller(namedRef, keyring)` constructs the real `Puller` and
  wraps it with `remoteimage.NewBoundedPuller` only if `pullSemaphore != nil`.
- The one call-site change in `NodePublishVolume`:
  `puller := remoteimage.NewPuller(...)` → `puller := n.newPuller(...)`.
  Everything downstream (the sync-vs-async branch) is unchanged.

**`cmd/plugin/main.go`**

```go
maxConcurrentImagePulls = flag.Int("max-concurrent-image-pulls", 0,
	"Maximum number of image pulls that may run concurrently on this node. "+
		"Pulls beyond this limit wait for a free slot instead of all running "+
		"at once and competing for the node's bandwidth/disk I/O. "+
		"0 (default) means unbounded, preserving prior behavior.")
```

**`pkg/metrics/metrics.go`** — two new unlabeled gauges:

| Metric | Meaning |
|---|---|
| `warm_metal_image_pull_inflight` | Pulls currently holding a slot (actively pulling) |
| `warm_metal_image_pull_queued` | Pulls currently waiting for a free slot |

Both stay at `0` when the flag is disabled. Unlabeled (not per-image) by
design — a node has one global in-flight/queued count, and labeling by image
would risk unbounded cardinality.

**Helm chart** (`charts/warm-metal-csi-driver`) — new `maxConcurrentImagePulls`
value (default `0`), rendered as `--max-concurrent-image-pulls` only when
`> 0`, mirroring the existing `enableAsyncPull`/`asyncPullTimeout` conditional
pattern in `templates/nodeplugin.yaml`.

## Edge Case: Queued Pulls That All Time Out

With `--max-concurrent-image-pulls=5` and more than 5 images requested at
once, the 6th+ requests sit in the FIFO queue and **never call the real
`Pull`** until a slot frees up. If every one of the 5 running pulls is
genuinely too slow to finish (e.g. a registry outage), each one occupies its
slot for the full `--async-pull-timeout` before releasing it on timeout —
only then does the next queued pull get its turn and start its own full
timeout window. In the worst case, a request queued behind a full round of
doomed pulls doesn't even attempt its real pull until
`(queue_position / limit) * async_pull_timeout` has elapsed.

This is the accepted tradeoff: a pathological "everything is broken" queue
drains slowly, in exchange for the common case — most pulls aren't
pathologically broken — succeeding far more often, since the pulls that do
get a slot now have an unshared bandwidth allocation instead of being
starved alongside dozens of others.

A related consequence: time spent queued counts against
`--async-pull-timeout`'s budget, since that timeout context is created
before the (possibly bounded) `Pull` is called in
`pkg/remoteimageasync/puller.go`. A pull that waits 6 minutes in queue only
gets the remaining 4 minutes to actually pull before its session is declared
timed out. This was accepted as the simplest v1 behavior — a single budget
covering queue-wait plus pull-time — rather than threading the semaphore
further into `remoteimageasync` to start the timeout only once a slot is
acquired.

## Testing

- `pkg/remoteimage/bounded_test.go` — unit tests on the decorator in
  isolation: asserts the configured limit is never exceeded across many
  simultaneous pulls (high-water-mark counter), and that a pull cancelled
  while still queued never invokes the wrapped `Pull` and does not leak its
  semaphore slot.
- `cmd/plugin/bounded_pull_test.go` — integration-style tests through the
  real `NewNodeServer`/`NodePublishVolume` wiring: with
  `maxConcurrentImagePulls=2` and 6 concurrent requests for distinct images,
  observed max concurrent `PullImage` calls is exactly 2 and all 6 still
  succeed; with `0` (default), all 6 run concurrently, confirming unbounded
  behavior is preserved.
- `GOOS=linux go build ./...`, `go vet ./...`, `gofmt -l` all clean.
- Three existing `NewNodeServer(...)` call sites in `node_server_test.go`
  updated for the new trailing parameter, passed `0` to preserve their exact
  prior (unbounded) behavior.

## Results

| Outcome | Status |
|---|---|
| Sync pull path bounded | ✅ |
| Async pull path bounded | ✅ (same semaphore, no changes to session/dedup logic) |
| Default behavior unchanged | ✅ (flag defaults to `0`/unbounded) |
| Same-image deduplication | ✅ Unaffected — happens one layer above, before the semaphore is touched |
| Observability | ✅ Two new gauges for in-flight/queued counts |

## References

- Jira: [KCS-2882](https://acquia.atlassian.net/browse/KCS-2882)
- `golang.org/x/sync/semaphore` — [package source](https://pkg.go.dev/golang.org/x/sync/semaphore)
