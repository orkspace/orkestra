package queue

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/logger"
	"k8s.io/client-go/util/workqueue"
)

// QueueItem identifies a unit of work in the controller queue.
//
// EventID controls queue identity:
//   - EventID == 0: normal work. Items for the same Key/GVK compare equal
//     and are coalesced by the client-go workqueue.
//   - EventID != 0: event-aware work. Each event receives a unique EventID,
//     so events for the same Key/GVK remain distinct in the queue.
//
// Sentinel values are intentionally not stored on QueueItem. They are
// event-time payload and are kept separately by Workqueue so that merely
// having sentinel values does not disable normal queue deduplication.
type QueueItem struct {
	Key     string
	GVK     string
	EventID uint64
}

// queueItemIdentity is the key used to associate event-time sentinel values
// with a QueueItem.
//
// EventID is zero for normal/coalescing work and non-zero for event-aware
// work. This mirrors the equality semantics of QueueItem.
type queueItemIdentity struct {
	Key     string
	GVK     string
	EventID uint64
}

type Workqueue struct {
	name         string
	queue        workqueue.TypedRateLimitingInterface[QueueItem]
	queueCfg     domain.Workqueue
	evaluateCond *BehaviourEval // whether or not to evaluate additional conditions in informer before enqueuing
	maxDepth     atomic.Int32   // 0 = unlimited; enforced atomically in Enqueue
	started      atomic.Bool

	// nextEventID provides a unique identity for event-aware queue items.
	// A non-zero EventID makes otherwise identical events distinct to the
	// client-go workqueue and therefore prevents them from being coalesced.
	nextEventID atomic.Uint64

	// sentinelMu protects sentinels because informer and worker goroutines
	// access the queue concurrently.
	sentinelMu sync.RWMutex

	// sentinels stores event-time sentinel values separately from QueueItem.
	//
	// This separation is important: sentinel availability must not itself
	// change queue deduplication semantics. Only EventID determines whether
	// an item is event-aware.
	sentinels map[queueItemIdentity]map[string]string
}

type BehaviourEval struct {
	OnLimit     atomic.Bool
	OnThreshold atomic.Bool
}

type WorkqueueInfo struct {
	Depth        int
	Limit        int
	DepthReached bool
}

func NewWorkqueue(name string) *Workqueue {
	if name == "" {
		name = "default workqueue"
	}

	return &Workqueue{
		name:      name,
		queue:     workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[QueueItem]()),
		sentinels: make(map[queueItemIdentity]map[string]string),
	}
}

// Queue is the TypedRateLimitingInterface used by all orkestra operators.
// It is an interface that rate limits items being added to the queue.
//
// https://pkg.go.dev/k8s.io/client-go@v0.36.1/util/workqueue#TypedRateLimitingInterface
func (q *Workqueue) Queue() workqueue.TypedRateLimitingInterface[QueueItem] {
	if q == nil {
		return nil
	}
	return q.queue
}

// Forget marks an item as completely processed and releases any sentinel
// payload associated with it.
//
// Sentinel payload must not be removed on Get/Done because the same QueueItem
// may be returned to the queue through AddRateLimited or AddAfter.
func (q *Workqueue) Forget(item QueueItem) {
	if q == nil || q.queue == nil {
		return
	}

	q.queue.Forget(item)

	identity := queueItemIdentity{
		Key:     item.Key,
		GVK:     item.GVK,
		EventID: item.EventID,
	}

	q.sentinelMu.Lock()
	delete(q.sentinels, identity)
	q.sentinelMu.Unlock()
}

// SetQueueDepth adjusts the queue depth limit at runtime.
// 0 means unlimited. Safe to call concurrently with Enqueue.
func (q *Workqueue) SetQueueDepth(n int) {
	q.maxDepth.Store(int32(n))
}

// Methods to implement the komponent interface
var _ domain.Komponent = (*Workqueue)(nil)

// Start is called by orkestra.Start() after all komoonents are registered
func (q *Workqueue) Start(ctx context.Context) error {
	logger.Debug().Str("name", q.name).Msg("workqueue started")
	q.started.Store(true)
	return nil
}

// Started returns true if default workqueue has started
// Used by orkestra for status check
func (q *Workqueue) Started() bool { return q.started.Load() }

// Shutdown drains the default workqueue
// This is called by orkestra.Shutdown() for graceful degradation
func (q *Workqueue) Shutdown(ctx context.Context) {
	if q.queue != nil {
		q.queue.ShutDown()
	}
}

// GetWithContext returns an item from the work queue.
// Context (e.g., timeout, cancellation).
// Future use
func (q *Workqueue) GetWithContext(ctx context.Context) (QueueItem, bool) {
	// Channel to receive the result of the blocking Get() call
	type result struct {
		item     QueueItem
		shutdown bool
	}
	resultCh := make(chan result, 1)

	// Run the blocking Get() in a goroutine
	go func() {
		item, shutdown := q.queue.Get()
		resultCh <- result{item, shutdown}
	}()

	// Wait for either context cancellation or a result
	select {
	case <-ctx.Done():
		// Context cancelled. The Get() goroutine is still running and will
		// eventually send to resultCh, but we're no longer listening.
		// We must drain that result to prevent a goroutine leak.
		go func() {
			<-resultCh
			// Optionally re-queue the item if we want to preserve it,
			// but since we're deactivating, we can just discard.
		}()
		return QueueItem{}, true // shutdown == true signals exit
	case res := <-resultCh:
		return res.item, res.shutdown
	}
}

// Name returns the name of the default workqueue
func (q *Workqueue) Name() string {
	return q.name
}

// Depth returns the length of the default workqueue
func (q *Workqueue) Depth() int {
	return q.queue.Len()
}

// IsUnlimited returns true when maxDepth is zero
func (q *Workqueue) IsUnlimited() bool {
	return q.MaxDepth() == 0
}

// DepthReached returns true when current depth is greater or equals maxDepth
func (q *Workqueue) DepthReached() bool {
	return q.Depth() >= q.MaxDepth()
}

// QueueInfo returns the current workqueue info
func (q *Workqueue) QueueInfo() (info *WorkqueueInfo) {
	if q == nil {
		return nil
	}
	info = &WorkqueueInfo{
		Depth:        q.Depth(),
		Limit:        q.MaxDepth(),
		DepthReached: q.Depth() >= q.MaxDepth(),
	}
	return info
}

// BehaviourCond returns the pending behaviour evaluation flags for this queue item.
func (q *Workqueue) BehaviourCond() *BehaviourEval {
	if q == nil || q.evaluateCond == nil {
		return nil
	}
	return q.evaluateCond
}

// OnLimitCond reports whether onLimit when/or conditions should be evaluated by the informer.
func (q *Workqueue) OnLimitCond() bool {
	cond := q.BehaviourCond()
	if cond == nil {
		return false
	}
	return cond.OnLimit.Load()
}

// OnThresholdCond reports whether onThreshold when/or conditions should be evaluated by the informer.
func (q *Workqueue) OnThresholdCond() bool {
	cond := q.BehaviourCond()
	if cond == nil {
		return false
	}
	return cond.OnThreshold.Load()
}

// NeedsBehaviourEval reports true if there are pending behaviour conditions delegated to the informer.
func (q *Workqueue) NeedsBehaviourEval() bool {
	return q.OnLimitCond() || q.OnThresholdCond()
}

// MaxDepth returns the current maximum queue depth (0 = unlimited).
func (q *Workqueue) MaxDepth() int { return int(q.maxDepth.Load()) }

// Get retrieves the next item from the queue.
func (q *Workqueue) Get() (QueueItem, bool) {
	return q.queue.Get()
}

// Done marks the item as processed and removes it from the queue.
func (q *Workqueue) Done(item QueueItem) {
	q.queue.Done(item)
}

// Add inserts an item into the queue.
func (q *Workqueue) Add(item QueueItem) {
	q.queue.Add(item)
}

// AddAfter inserts an item into the queue after the specified duration.
func (q *Workqueue) AddAfter(item QueueItem, duration time.Duration) {
	q.queue.AddAfter(item, duration)
}

// AddRateLimited inserts an item into the queue with rate limiting.
func (q *Workqueue) AddRateLimited(item QueueItem) {
	q.queue.AddRateLimited(item)
}

// Len returns the current number of items in the queue.
func (q *Workqueue) Len() int {
	return q.queue.Len()
}
