# 02 — QueueRegistry

`QueueRegistry` maps GVK strings to `Workqueue` instances. It is the canonical
lookup point for "which queue does this CRD use?".

## Structure

```go
type QueueRegistry struct {
    name    string
    queues  map[string]*Workqueue  // keyed by GVK string
    mu      sync.RWMutex
    started atomic.Bool
}
```

`mu` guards the `queues` map. Reads use `RLock`; writes (Register, Drain)
use `Lock`.

## Registration

Each CRD is registered before the kordinator starts workers:

```go
func (qr *QueueRegistry) Register(gvk string, q domain.Workqueue) *Workqueue {
    wq := NewWorkqueue()
    qr.queues[gvk] = wq
    if q != nil {
        wq.queueCfg = q
        wq.maxDepth.Store(int32(q.MaxQueueDepth()))
    }
    return wq
}
```

`Register` is called in `konstructRuntime` during the Katalog loading step,
one call per CRD with the CRD's `QueueConfig()`. `maxDepth` is read from the
config and stored as an atomic for lock-free enqueue checks. The returned
`*Workqueue` is passed to the informer's event handler so informer events go
directly into the right queue.

## Lookup

```go
func (qr *QueueRegistry) For(gvk string) (*Workqueue, bool)
```

Called in three places:

| Caller | Why |
|--------|-----|
| `runWorkerForGVK` | resolve the right queue to dequeue from |
| `processItemForGVK` | re-queue on failure / forget on success |
| `stopCRDWorkers` | shut down the queue to unblock idle workers |
| `startCRDWorkers` | inject the queue into the reconciler via `QueueInjector` |

When `For` returns `false`, callers fall back to `defaultWorkqueue` — the single
shared queue used before per-CRD registration runs.

## Queue depth

```go
func (qr *QueueRegistry) Depth(gvk string) int
```

Read by the Control Center's `/katalog/{crd}` handler. Not used in the hot
path — the worker loop reads `wq.Depth()` directly after each item for metrics.

## Lifecycle as an Orkestra component

`QueueRegistry` implements `domain.Komponent`. `Start` just sets the `started`
flag; the queues themselves are already created by `Register` calls before
`Start` runs. `Shutdown` calls `ShutDown()` on every registered queue, which
unblocks any worker goroutine blocked on `Queue.Get()`.

The separation between registration (before Start) and shutdown (after
leadership loss) means all queues are ready before any informer starts
delivering events.

---

**← Back** [01 — Workqueue](01-workqueue.md)
