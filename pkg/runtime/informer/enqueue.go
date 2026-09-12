// pkg/informer/enqueue.go
//
// Pre-enqueue admission and queue routing for the informer factory.
//
// This file owns the boundary between informer events and workqueues:
// sentinel computation, pre-enqueue admission, and queue identity selection.
package informer

import (
	"context"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/queue"
	"github.com/orkspace/orkestra/pkg/runtime/sentinel"
)

// ComputeSentinelsOptions provides sentinel declarations for event evaluation.
type ComputeSentinelsOptions struct {
	// DeclaredSentinels is a list of sentinels to be used for event evaluation.
	DeclaredSentinels []string
}

type EnqueueSource string

const (
	EnqueueSourcePrimary EnqueueSource = "primary"
	EnqueueSourceWatch   EnqueueSource = "watch"
	EnqueueSourceEvent   EnqueueSource = "event"
)

func (e EnqueueSource) String() string {
	return string(e)
}

// EnqueueOptions provides context for event-specific enqueue evaluation.
type EnqueueOptions struct {
	// WatchSecondaryGVK identifies the secondary resource when the event
	// originates from a watch.
	WatchSecondaryGVK string

	// Source identifies what caused the enqueue.
	Source EnqueueSource

	// SourceName identifies the declaration that caused the enqueue when
	// available, such as an EventEntry or WatchEntry name.
	SourceName string

	// Observation carries observation data to make available to the resolver
	// when the queued item is evaluated.
	Observation *ObservationContext
}

// ObservationContext contains data observed alongside the resource that
// caused the enqueue.
type ObservationContext struct {
	// Events contains matched Kubernetes Event data keyed by EventEntry name.
	Events map[string]interface{}
}

// eventsFromObservation returns the observed Event data carried by the
// observation context, or nil when no observation context is present.
func eventsFromObservation(observation *ObservationContext) map[string]interface{} {
	if observation == nil {
		return nil
	}
	return observation.Events
}

// ComputeSentinels derives event-time sentinel values from an informer update.
//
// Declarations normally come from Katalog. Callers may provide declarations
// explicitly for secondary watches.
func (f *Factory) ComputeSentinels(
	gvkStr string,
	oldObj, newObj interface{},
	opts ...ComputeSentinelsOptions,
) map[string]string {
	if f.katalog == nil {
		return nil
	}

	oldDomain, okOld := domain.ToDomainObject(oldObj)
	newDomain, okNew := domain.ToDomainObject(newObj)
	if !okOld || !okNew {
		return nil
	}

	var declared []string
	if len(opts) > 0 && len(opts[0].DeclaredSentinels) > 0 {
		declared = opts[0].DeclaredSentinels
	} else {
		declared = f.katalog.GetPreReconcileSentinels(newDomain, gvkStr)
	}

	if len(declared) == 0 {
		return nil
	}

	return sentinel.Compute(declared, oldDomain, newDomain)
}

// queueFor returns the queue for a GVK, falling back to the default queue.
func (f *Factory) queueFor(gvkStr string) *queue.Workqueue {
	if f.queueRegistry != nil {
		if wq, ok := f.queueRegistry.For(gvkStr); ok {
			return wq
		}
	}

	if f.defaultWq == nil {
		logger.Warn().
			Str("gvk", gvkStr).
			Msg("no queue available for informer event")
		return nil
	}

	return f.defaultWq
}

// allowEnqueue evaluates pre-enqueue admission conditions for an event.
//
// Events that fail an applicable condition are dropped before entering the
// workqueue.
func (f *Factory) allowEnqueue(
	ctx context.Context,
	gvkStr string,
	obj interface{},
	wq *queue.Workqueue,
	sentinels map[string]string,
	opts EnqueueOptions,
) bool {
	if wq == nil {
		return false
	}

	// Namespace restriction.
	namespace := extractNamespace(obj)
	if !f.namespaceAllowed(gvkStr, namespace) {
		logger.Debug().
			Str("gvk", gvkStr).
			Str("namespace", namespace).
			Msg("informer: event dropped — namespace not allowed")
		return false
	}

	domObj, ok := domain.ToDomainObject(obj)
	if !ok {
		return true
	}

	if f.katalog == nil {
		return true
	}

	// Queue behaviour conditions.
	if wq.NeedsBehaviourEval() {
		if !f.katalog.EvaluateQueueBehaviourConditions(ctx, gvkStr, domObj, domain.EvaluateOptions{Sentinels: sentinels}) {
			return false
		}
	}

	// Enqueue-gate conditions.
	// Secondary informers evaluate their declared observation entry's enqueueGate.
	switch opts.Source {
	case EnqueueSourceWatch:
		if opts.WatchSecondaryGVK == "" {
			return true
		}

		return f.katalog.EvaluateWatchEnqueueFilter(
			ctx,
			gvkStr,
			opts.WatchSecondaryGVK,
			domObj,
			f.cs,
			domain.EvaluateOptions{
				Events:    eventsFromObservation(opts.Observation),
				Sentinels: sentinels,
			},
		)

	case EnqueueSourceEvent:
		if opts.SourceName == "" {
			return true
		}

		return f.katalog.EvaluateEventEnqueueFilter(
			ctx,
			gvkStr,
			opts.SourceName,
			domObj,
			f.cs,
			domain.EvaluateOptions{
				Events:    eventsFromObservation(opts.Observation),
				Sentinels: sentinels,
			},
		)
	}

	// Primary informers evaluate preReconcile.enqueueGate.
	if !f.katalog.EvaluateEnqueueFilter(ctx, gvkStr, domObj, f.cs, domain.EvaluateOptions{Sentinels: sentinels}) {
		logger.Debug().
			Str("gvk", gvkStr).
			Str("name", domObj.GetName()).
			Msg("informer: event dropped — enqueue filter")
		return false
	}

	return true
}

// enqueue routes an admitted primary event using the object's own key.
//
// The key is derived here and passed to the common enqueue path.
func (f *Factory) enqueue(
	gvkStr string,
	obj interface{},
	sentinels map[string]string,
) {
	domObj, ok := domain.ToDomainObject(obj)
	if !ok {
		return
	}

	key := domObj.GetNamespace() + "/" + domObj.GetName()
	if domObj.GetNamespace() == "" {
		key = domObj.GetName()
	}

	f.enqueueKey(gvkStr, key, obj, f.queueFor(gvkStr), sentinels, false, EnqueueOptions{
		Source: EnqueueSourcePrimary,
	})
}

// enqueueKey routes an admitted event using the supplied queue key.
//
// Primary events use their object's key; secondary watch events supply the
// key of the affected primary resource. Sentinel context is retained when
// present.
func (f *Factory) enqueueKey(
	gvkStr string,
	key string,
	obj interface{},
	wq *queue.Workqueue,
	sentinels map[string]string,
	secondary bool,
	opts EnqueueOptions,
) {
	if wq == nil {
		return
	}

	domObj, ok := domain.ToDomainObject(obj)
	if !ok {
		return
	}

	eventAware := false
	if f.katalog != nil {
		eventAware = f.katalog.IsEventAware(domObj, gvkStr)
	}

	if secondary {
		switch {
		case eventAware:
			wq.EnqueueWithKeyEventSentinels(key, gvkStr, sentinels)
		case sentinels != nil:
			wq.EnqueueWithKeySentinels(key, gvkStr, sentinels)
		default:
			wq.EnqueueWithKey(key, gvkStr)
		}
	} else {
		switch {
		case eventAware:
			wq.EnqueueWithEventSentinels(obj, gvkStr, sentinels)
		case sentinels != nil:
			wq.EnqueueWithSentinels(obj, gvkStr, sentinels)
		default:
			wq.Enqueue(obj, gvkStr)
		}
	}

	identity := "coalesced"
	if eventAware {
		identity = "event-aware"
	}

	logger.Debug().
		Str("gvk", gvkStr).
		Str("key", key).
		Str("queue", wq.Name()).
		Str("source", opts.Source.String()).
		Str("sourceName", opts.SourceName).
		Str("identity", identity).
		Bool("sentinels", sentinels != nil).
		Msg("informer: event enqueued")
}

// AllowAndEnqueueKey admits an event and enqueues it under the given key.
func (f *Factory) AllowAndEnqueueKey(
	ctx context.Context,
	gvkStr string,
	key string,
	obj interface{},
	wq *queue.Workqueue,
	sentinels map[string]string,
	opts EnqueueOptions,
) bool {
	if !f.allowEnqueue(ctx, gvkStr, obj, wq, sentinels, opts) {
		return false
	}

	f.enqueueKey(gvkStr, key, obj, wq, sentinels, true, opts)
	return true
}
