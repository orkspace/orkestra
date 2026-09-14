package queue

import (
	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/logger"
	"k8s.io/client-go/tools/cache"
)

// evaluateQueueBehaviour starts the queue behaviour evaluation when configured
// and delegates to the informer for when/or condition evaluation — the informer has
// access to the full preReconcile resolver context that the queue itself does not.
//
// Fast path: IsUnlimited() — where maxDepth == 0, all items are always enqueued.
func (q *Workqueue) evaluateQueueBehaviour(item QueueItem) bool {
	if q == nil {
		return true
	}

	cfg := q.queueCfg
	if cfg == nil {
		return true
	}
	if !cfg.IsUnlimited() {
		switch {
		case cfg.HasOnThreshold():
			if cfg.ThresholdReached(q.QueueInfo().Depth) {
				// Delegate to the informer to evaluate when/or conditions.
				// It has access to all preReconcile resolver context.
				if cfg.HasOnThresholdConditions() {
					q.evaluateCond.OnThreshold.Store(true)
					return true
				}
			}
		case q.QueueInfo().DepthReached:
			if cfg.HasOnLimitConditions() {
				// Delegate to the informer to evaluate when/or conditions.
				q.evaluateCond.OnLimit.Store(true)
				return true
			}
		}

		// No conditions declared — drop immediately.
		logger.Warn().
			Str("key", item.Key).
			Str("gvk", item.GVK).
			Int("limit", q.QueueInfo().Limit).
			Int("depth", q.QueueInfo().Depth).
			Int("threshold", cfg.ThresholdValue()).
			Msg("enqueue: queue depth threshold/limit reached — item dropped")
		return false
	}

	return true
}

// addWithEval runs evaluateQueueBehaviour and adds the item to the queue.
func (q *Workqueue) addWithEval(item QueueItem) {
	if q == nil || !q.evaluateQueueBehaviour(item) {
		return
	}
	q.queue.Add(item)
	logger.Debug().Str("key", item.Key).Str("gvk", item.GVK).Msg("enqueued")
}

// resolveKeyFromCache returns the obj key from cache
func resolveKeyFromCache(obj interface{}, gvk string) string {
	// Handle tombstone (deleted objects)
	key, err := cache.MetaNamespaceKeyFunc(domain.UnwrapCacheTombstone(obj))
	if err != nil {
		logger.Error().Err(err).Str("gvk", gvk).Msg("enqueue: failed to get key")
		return ""
	}
	return key
}

// cloneSentinels returns an independent copy of a sentinel map.
//
// Sentinel values are event context owned by the queue. Copying prevents the
// informer or worker from accidentally sharing mutable map state.
func cloneSentinels(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}

	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}

	return out
}

// enqueueWithSentinels stores sentinel payload and adds the corresponding
// queue item.
//
// eventAware controls queue identity, not sentinel availability:
//   - false: EventID remains zero and normal deduplication applies.
//   - true: a unique EventID is assigned and the event is preserved.
func (q *Workqueue) enqueueWithSentinels(
	key string,
	gvk string,
	sentinels map[string]string,
	eventAware bool,
) {
	var eventID uint64

	if eventAware {
		eventID = q.nextEventID.Add(1)
	}

	item := QueueItem{
		Key:     key,
		GVK:     gvk,
		EventID: eventID,
	}

	// Evaluate queue behaviour before adding the item. This ensures an item
	// rejected by queue limits never leaves an orphaned sentinel payload.
	if !q.evaluateQueueBehaviour(item) {
		return
	}

	identity := queueItemIdentity{
		Key:     key,
		GVK:     gvk,
		EventID: eventID,
	}

	q.sentinelMu.Lock()
	q.sentinels[identity] = cloneSentinels(sentinels)
	q.sentinelMu.Unlock()

	q.queue.Add(item)
}
