// pkg/informer/event.go
package informer

import (
	"context"

	"github.com/orkspace/orkestra/pkg/logger"
)

// handleEvent resolves the GVK from the informer event and routes the object
// to the appropriate workqueue.
//
// Pre-enqueue admission is handled by AllowEnqueue. Events that fail namespace,
// queue behaviour, or enqueue-gate conditions are dropped before entering the
// queue. Admitted events are then passed to enqueue, which selects the
// per-CRD queue or falls back to the default queue.
func (f *Factory) handleEvent(ctx context.Context, obj interface{}) {
	// Block until factory is ready — List/Watch have started
	<-f.ready

	gvk, err := gvkFromObj(obj, f.scheme)
	if err != nil {
		return
	}

	gvkStr := gvk.String()

	// ── Tier 2: Pre-enqueue namespace filter ─────────────────────────────
	// Check namespace restriction BEFORE the item enters the queue.
	// Items that fail this check are dropped — they do no work and create
	// no queue pressure. The reconciler check (Tier 3) remains as a safety
	// net for race conditions during startup.
	namespace := extractNamespace(obj)
	if !f.namespaceAllowed(gvkStr, namespace) {
		logger.Debug().
			Str("gvk", gvkStr).
			Str("namespace", namespace).
			Msg("informer: event dropped — namespace not allowed")
		return
	}

	f.enqueue(gvkStr, obj, nil)
}

// handleUpdate handles an informer update from oldObj to newObj.
//
// Update events are the point at which event-time sentinel values can be
// computed because both the previous and current objects are available.
// The computed sentinel context is passed through AllowEnqueue and, if the
// event is admitted, through enqueue into the workqueue.
//
// Whether the event retains its identity through queue deduplication is
// determined by the CRD's eventAware configuration in enqueue; it does not
// affect whether the event is admitted here.
func (f *Factory) handleUpdate(
	ctx context.Context,
	gvkStr string,
	oldObj, newObj interface{},
) {
	<-f.ready

	sentinels := f.ComputeSentinels(gvkStr, oldObj, newObj)
	wq, _ := f.queueRegistry.For(gvkStr)

	if !f.allowEnqueue(ctx, gvkStr, newObj, wq, sentinels, EnqueueOptions{
		Source:     EnqueueSourcePrimary,
		SourceName: gvkStr,
	}) {
		return
	}

	f.enqueue(gvkStr, newObj, sentinels)
}
