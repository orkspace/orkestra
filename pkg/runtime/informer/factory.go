// pkg/informer/factory.go
package informer

import (
	"context"

	"github.com/orkspace/orkestra/pkg/logger"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

// OwnerNameIndex is the name of the auto-registered owner-reference index on every
// informer created by this factory. Use it with indexer.ByIndex(OwnerNameIndex, ownerName).
const OwnerNameIndex = "orkestra.io/owner-name"

// OwnerNameIndexFunc indexes an unstructured object by the names of its owner references.
func OwnerNameIndexFunc(obj interface{}) ([]string, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, nil
	}
	refs := u.GetOwnerReferences()
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	return names, nil
}

// StoreFor returns the informer store for the given GVK, or nil if no informer
// is registered for that type. Used by orkadapter.ToClient to serve cached reads.
func (f *Factory) StoreFor(gvk schema.GroupVersionKind) cache.Store {
	f.mu.RLock()
	defer f.mu.RUnlock()
	entry, ok := f.informers[gvk.String()]
	if !ok || entry.Informer == nil {
		return nil
	}
	return entry.Informer.GetStore()
}

// IndexerFor returns the cache.Indexer for the given GVK, or nil if no informer
// is registered. The indexer supports ByIndex(OwnerNameIndex, ownerName) out of
// the box; additional indexes can be registered via AddIndexers on the informer.
func (f *Factory) IndexerFor(gvk schema.GroupVersionKind) cache.Indexer {
	f.mu.RLock()
	defer f.mu.RUnlock()
	entry, ok := f.informers[gvk.String()]
	if !ok || entry.Informer == nil {
		return nil
	}
	return entry.Informer.GetIndexer()
}

// RegisterInformer records an already-running informer under the given GVK so
// that StoreFor and IndexerFor can serve it. Used by the kordinator to expose
// watch-entry informers (which it owns and starts itself) to the kubeclient
// cache layer without going through the full ForListerWatcher path.
func (f *Factory) RegisterInformer(gvk schema.GroupVersionKind, inf cache.SharedIndexInformer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := gvk.String()
	if _, exists := f.informers[key]; exists {
		return // already registered — first registration wins
	}
	f.informers[key] = &InformerEntry{
		Informer: inf,
		GVK:      &gvk,
	}
}

// For creates or returns a SharedIndexInformer for the given object type.
// Uses the client provider to build the ListerWatcher via newListWatch.
// Each type gets exactly one informer — subsequent calls return the cached one.
// The informer is not started here — Start() starts all registered informers.
func (f *Factory) For(obj runtime.Object, ctx context.Context, opts Options) cache.SharedIndexInformer {
	f.mu.Lock()
	defer f.mu.Unlock()

	gvk, err := gvkFromObj(obj, f.scheme)
	if err != nil {
		return nil
	}

	return f.getOrCreate(gvk, f.newListWatch(obj, opts), obj, ctx, opts)
}

// ForListerWatcher creates or returns a SharedIndexInformer using an explicit
// ListerWatcher. Used for unstructured CRDs where the dynamic client provides
// the ListerWatcher directly, bypassing the typed client provider entirely.
// The scheme is never consulted — no conversion errors for unstructured types.
func (f *Factory) ForListerWatcher(lw cache.ListerWatcher, obj runtime.Object, ctx context.Context, opts Options) cache.SharedIndexInformer {
	f.mu.Lock()
	defer f.mu.Unlock()

	gvk, err := gvkFromObj(obj, f.scheme)
	if err != nil {
		return nil
	}
	return f.getOrCreate(gvk, lw, obj, ctx, opts)
}

// getOrCreate is the shared core — returns an existing informer or creates one.
// Called by both For and ForListerWatcher. Must be called with f.mu held.
func (f *Factory) getOrCreate(
	gvk *schema.GroupVersionKind,
	lw cache.ListerWatcher,
	obj runtime.Object,
	ctx context.Context,
	opts Options,
) cache.SharedIndexInformer {
	key := gvk.String()

	// Return existing informer
	if entry, ok := f.informers[key]; ok {
		return entry.Informer
	}

	// ALWAYS create the informer (even for missing CRDs)
	resync := opts.Resync
	if resync == 0 {
		resync = f.defaultResync
	}

	inf := cache.NewSharedIndexInformer(lw, obj, resync, cache.Indexers{
		OwnerNameIndex: OwnerNameIndexFunc,
	})

	gvkStr := gvk.String()
	// Ensure GVK is normalized for all CRDs
	inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			normalizeInformerObject(obj, gvk)
			f.handleEvent(ctx, obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			normalizeInformerObject(newObj, gvk)
			f.handleUpdate(ctx, gvkStr, oldObj, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			normalizeInformerObject(obj, gvk)
			f.handleEvent(ctx, obj)
		},
	})

	// Check if CRD exists
	crdExists := crdExists(gvk, f.restConfig)

	entry := &InformerEntry{
		Informer: inf,
		Name:     opts.Name,
		Resync:   resync,
		Missing:  !crdExists,
		GVK:      gvk,
	}

	f.informers[key] = entry

	if crdExists {
		delete(f.missing, key)
		if f.started.Load() {
			go inf.Run(ctx.Done())
		}
		logger.Info().Str("name", opts.Name).Str("gvk", gvk.String()).Msg("informer created and started")
	} else {
		f.missing[key] = entry
		logger.Warn().Str("name", opts.Name).Str("gvk", gvk.String()).Msg("CRD missing — informer created but not started")
	}

	return inf
}
