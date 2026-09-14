// pkg/runtime/informer/observe/watch.go
//
// Secondary watch informers for operatorBox.observe.watch entries and managed resources.
//
// Two sources produce watch informers:
//
//  1. operatorBox.observe.watch — explicit entries declared by the operator author. Full
//     control: on:, enqueueGate:, keyFrom:, index:.
//
//  2. constructor.resources / hooks.resources — owned resource types. Treated as
//     implicit watch entries: all events, owner-reference key resolution, no index.
//     Mirrors what Owns() does in controller-runtime — cache-backed reads and
//     re-enqueue when an owned resource changes. Explicit watch: entries take
//     priority when the same type appears in both lists.
//
// Key resolution order (first match wins):
//  1. keyFrom.label  — the watched object has a label whose value is the primary CR key.
//  2. keyFrom.name   — a fixed primary CR name declared in the watch entry.
//  3. ownerReference — the watched object is owned by a primary CR of this CRD.
//  4. broadcast      — none of the above matched; enqueue all known primary CRs.
//     Right for shared resources (ConfigMap, Secret) that affect every CR equally.
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

// watchOptions contains the stable runtime context for one secondary watch.
//
// Event-specific data such as oldObj, newObj, and computed sentinels stays
// outside the options so the options describe the watch itself rather than
// an individual event.
type watchOptions struct {
	entry            orktypes.WatchEntry
	gvr              schema.GroupVersionResource
	crd              orktypes.CRDEntry
	queue            *queue.Workqueue
	broadcastAllowed bool
}

func (o *Observer) observeWatches(ctx context.Context, crd orktypes.CRDEntry) {
	if !crd.WithWatchEntries() && !crd.WithAnyManagedResources() {
		return
	}

	wq, ok := o.queueFor(crd)
	if !ok {
		logger.Warn().
			Str("gvk", crd.GVKString()).
			Msg("observe: no queue registered for primary CRD — skipping watches")
		return
	}

	// Track GVRs covered by explicit watch: entries so managed resources don't
	// register a second informer for the same type. Explicit watch: takes priority.
	covered := map[string]bool{}
	for _, w := range crd.WatchEntries() {
		gvr, ok := o.resolveGVR(w.ToManagedResource())
		if ok {
			covered[gvr.String()] = true
		}
	}

	// Explicit watch entries take priority.
	for _, entry := range crd.WatchEntries() {
		entry := entry

		gvr, ok := o.resolveGVR(entry.ToManagedResource())
		if !ok {
			logger.Warn().
				Str("apiVersion", entry.APIVersion).
				Str("kind", entry.Kind).
				Str("primary", crd.APITypes.Kind).
				Msg("observe: cannot resolve watch GVR — entry skipped")
			continue
		}

		covered[gvr.String()] = true

		o.startWatchInformer(ctx, watchOptions{
			entry:            entry,
			gvr:              gvr,
			crd:              crd,
			queue:            wq,
			broadcastAllowed: true,
		})
	}

	// Managed resources become implicit watches.
	// They use the same observation and enqueue path as explicit watch entries,
	// but are restricted to owner-reference key resolution rather than broadcast.
	for _, resource := range crd.AllManagedResources() {
		gvr, ok := o.resolveGVR(resource)
		if !ok || covered[gvr.String()] {
			continue
		}

		covered[gvr.String()] = true

		entry := orktypes.WatchEntry{
			APIVersion: gvr.Group + "/" + gvr.Version,
			Kind:       resource.Kind,
		}

		if entry.APIVersion == "/" {
			entry.APIVersion = "v1"
		}

		o.startWatchInformer(ctx, watchOptions{
			entry:            entry,
			gvr:              gvr,
			crd:              crd,
			queue:            wq,
			broadcastAllowed: false,
		})
	}
}

func (o *Observer) startWatchInformer(ctx context.Context, opts watchOptions) {
	// Each secondary watch gets its own dynamic informer backed by the shared
	// informer factory so its cache/indexer is available to the runtime.
	lw := o.deps.Kube.NewDynamicListerWatcher(opts.entry.ToCRDInfo(opts.gvr), kubeclient.ListOptions{})

	// Build indexers: always include namespace; add any user-declared index: entries.
	indexers := cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
	for _, wi := range opts.entry.Index {
		parts := splitWatchField(wi.Field)
		indexers[wi.Name] = func(obj interface{}) ([]string, error) {
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return nil, nil
			}

			val, found, err := unstructured.NestedString(u.Object, parts...)
			if err != nil || !found || val == "" {
				return nil, err
			}

			return []string{val}, nil
		}
	}

	inf := cache.NewSharedIndexInformer(
		lw,
		&unstructured.Unstructured{},
		0, // no resync — primary CRD resync handles re-queuing
		indexers,
	)

	// Captured so each handler can check HasSynced; events fired during the
	// initial List phase (before sync) are dropped — same as controller-runtime.
	localInf := inf
	_, _ = inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if !localInf.HasSynced() {
				return
			}

			o.handleWatchEvent(ctx, opts, nil, obj, orktypes.ObserveEventCreate)
		},

		UpdateFunc: func(oldObj, newObj interface{}) {
			if !localInf.HasSynced() {
				return
			}

			o.handleWatchEvent(ctx, opts, oldObj, newObj, orktypes.ObserveEventUpdate)
		},

		DeleteFunc: func(obj interface{}) {
			if !localInf.HasSynced() {
				return
			}

			o.handleWatchEvent(ctx, opts, nil, domain.UnwrapCacheTombstone(obj), orktypes.ObserveEventDelete)
		},
	})

	o.deps.Informer.RegisterInformer(
		schema.GroupVersionKind{
			Group:   opts.gvr.Group,
			Version: opts.gvr.Version,
			Kind:    opts.entry.Kind,
		},
		inf,
	)

	// The observer owns the informer lifecycle; Kordinator only asks the
	// observer to establish secondary observation for the CRD.
	go inf.Run(ctx.Done())

	logger.Info().
		Str("primary", opts.crd.APITypes.Kind).
		Str("watched", opts.entry.Kind).
		Str("gvr", opts.gvr.String()).
		Msg("observe: watch informer started")
}

func (o *Observer) handleWatchEvent(ctx context.Context, opts watchOptions, oldObj, newObj interface{}, on orktypes.ObserveEvent) {
	if !opts.entry.ObserveOn(on.String()) {
		return
	}

	var sentinels map[string]string

	if opts.entry.HasWatchSentinels() {
		sentinels = o.deps.Informer.ComputeSentinels(
			opts.crd.GVKString(),
			oldObj,
			newObj,
			informer.ComputeSentinelsOptions{
				DeclaredSentinels: opts.entry.EnqueueGate.DeclaredSentinels(),
			},
		)
	}

	// Observation ends at the existing enqueue boundary. Once the affected
	// primary CR key is resolved, normal queue admission and reconciliation
	// semantics take over; the observer does not invoke the reconciler directly.
	keys := o.resolveWatchKeys(opts, newObj)
	for _, key := range keys {
		o.deps.Informer.AllowAndEnqueueKey(
			ctx,
			opts.crd.GVKString(),
			key,
			newObj,
			opts.queue,
			sentinels,
			informer.EnqueueOptions{
				WatchSecondaryGVK: opts.entry.GVKString(),
				Source:            informer.EnqueueSourceWatch,
				SourceName:        opts.entry.Name,
			},
		)
	}
}
