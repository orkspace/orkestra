// pkg/runtime/informer/observe/event.go
package observe

import (
	"context"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/informer"
	"github.com/orkspace/orkestra/pkg/runtime/queue"
	orktypes "github.com/orkspace/orkestra/pkg/types"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

var eventGVR = schema.GroupVersionResource{
	Group:    "events.k8s.io",
	Version:  "v1",
	Resource: "events",
}

var eventGVK = schema.GroupVersionKind{
	Group:   "events.k8s.io",
	Version: "v1",
	Kind:    "Event",
}

// eventOptions contains the stable runtime context for EventEntry matching
// and enqueueing for one primary CRD.
type eventOptions struct {
	entries map[string]orktypes.EventEntry
	crd     orktypes.CRDEntry
	queue   *queue.Workqueue
}

func (o *Observer) observeEvents(ctx context.Context, crd orktypes.CRDEntry) {
	entries := crd.EventEntries()
	if len(entries) == 0 {
		return
	}

	wq, ok := o.queueFor(crd)
	if !ok {
		logger.Warn().
			Str("gvk", crd.GVKString()).
			Msg("observe: no queue registered for primary CRD — skipping events")
		return
	}

	// One Event informer is sufficient for all EventEntry declarations
	// belonging to this observer.
	o.startEventInformer(ctx, eventOptions{
		crd:     crd,
		entries: entries,
		queue:   wq,
	})
}

func (o *Observer) startEventInformer(ctx context.Context, opts eventOptions) {
	info := kubeclient.CRDInfo{
		Group:   eventGVR.Group,
		Version: eventGVR.Version,
		Kind:    eventGVK.Kind,
		Plural:  eventGVR.Resource,
	}

	lw := o.deps.Kube.NewDynamicListerWatcher(info, kubeclient.ListOptions{})

	inf := cache.NewSharedIndexInformer(
		lw,
		&unstructured.Unstructured{},
		0,
		cache.Indexers{
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		},
	)

	localInf := inf

	_, _ = inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if !localInf.HasSynced() {
				return
			}
			o.handleEvent(ctx, opts, obj, orktypes.ObserveEventCreate)
		},

		UpdateFunc: func(_, obj interface{}) {
			if !localInf.HasSynced() {
				return
			}
			o.handleEvent(ctx, opts, obj, orktypes.ObserveEventUpdate)
		},

		DeleteFunc: func(obj interface{}) {
			if !localInf.HasSynced() {
				return
			}
			o.handleEvent(ctx, opts, obj, orktypes.ObserveEventDelete)
		},
	})

	// The Event informer is used only as an observation mechanism and is not
	// registered as the shared Event cache. Registering it under the Event GVK
	// could also cause collisions with another observer.
	go inf.Run(ctx.Done())

	logger.Info().
		Str("primary", opts.crd.APITypes.Kind).
		Str("gvr", eventGVR.String()).
		Msg("observe: event informer started")
}

func (o *Observer) handleEvent(ctx context.Context, opts eventOptions, obj interface{}, on orktypes.ObserveEvent) {
	event, ok := domain.ToUnstructured(obj)
	if !ok {
		return
	}

	for name, entry := range opts.entries {
		if !entry.ObserveOn(on.String()) || !entry.Matches(event) {
			continue
		}

		o.handleMatchingEvent(ctx, opts, name, entry, event)
	}
}

func (o *Observer) handleMatchingEvent(ctx context.Context, opts eventOptions, name string, entry orktypes.EventEntry, event *unstructured.Unstructured) {
	// Reuse WatchEntry routing semantics to resolve the primary CR key(s).
	// The Event itself remains the observed secondary object.
	watch := entry.ToWatchEntry(opts.crd)

	watchOpts := watchOptions{
		entry:            watch,
		crd:              opts.crd,
		queue:            opts.queue,
		broadcastAllowed: true,
	}

	keys := o.resolveWatchKeys(watchOpts, event)

	for _, key := range keys {
		// The Event is passed through as the observed object so the existing
		// enqueue path can apply the normal secondary-resource semantics.
		o.deps.Informer.AllowAndEnqueueKey(
			ctx,
			opts.crd.GVKString(),
			key,
			event,
			opts.queue,
			nil,
			informer.EnqueueOptions{
				WatchSecondaryGVK: eventGVK.String(),
				Source:            informer.EnqueueSourceEvent,
				SourceName:        name,
				Observation: &informer.ObservationContext{
					Events: map[string]interface{}{
						name: eventContext(event),
					},
				},
			},
		)
	}
}

// eventContext builds the resolver context for a Kubernetes Event.
func eventContext(event *unstructured.Unstructured) map[string]interface{} {
	return map[string]interface{}{
		"name":                event.GetName(),
		"namespace":           event.GetNamespace(),
		"reason":              event.Object["reason"],
		"action":              event.Object["action"],
		"type":                event.Object["type"],
		"reportingController": event.Object["reportingController"],
		"reportingInstance":   event.Object["reportingInstance"],
		"regarding":           event.Object["regarding"],
		"related":             event.Object["related"],
	}
}
