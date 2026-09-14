package informer

import (
	"context"
	"fmt"
	"strings"

	"errors"

	"github.com/orkspace/orkestra/domain"

	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// common
var (
	gvkFromObj = utils.GvkFromObject
	crdExists  = utils.CheckCRDExists
	merge      = utils.Merge
)

// newListWatch returns a ListWatch for the given object type.
// Both List and Watch block on f.ready so they never run before Start().
// When opts.Namespace is set (Tier 1 single-namespace filter), the ListerWatcher
// is scoped to that namespace — the informer never sees events from other namespaces.
func (f *Factory) newListWatch(obj runtime.Object, opts Options) *cache.ListWatch {
	return &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			<-f.ready

			if opts.LabelSelector != "" {
				merge(&options.LabelSelector, opts.LabelSelector, ",")
			}
			if opts.FieldSelector != "" {
				merge(&options.FieldSelector, opts.FieldSelector, ",")
			}

			client, err := f.clientProvider.For(obj)
			if err != nil {
				return nil, fmt.Errorf("list: failed to get client for %T: %w", obj, err)
			}
			if opts.Namespace != "" {
				return client.ListInNamespace(ctx, opts.Namespace, options)
			}
			return client.List(ctx, options)
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			<-f.ready

			if opts.LabelSelector != "" {
				merge(&options.LabelSelector, opts.LabelSelector, ",")
			}
			if opts.FieldSelector != "" {
				merge(&options.FieldSelector, opts.FieldSelector, ",")
			}

			client, err := f.clientProvider.For(obj)
			if err != nil {
				return nil, fmt.Errorf("watch: failed to get client for %T: %w", obj, err)
			}
			if opts.Namespace != "" {
				return client.WatchInNamespace(ctx, opts.Namespace, options)
			}
			return client.Watch(ctx, options)
		},
	}
}

// Start signals readiness and starts all registered informers.
// Must be called exactly once — enforced by ErrFactoryAlreadyStarted.
func (f *Factory) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.started.Load() {
		return errors.New("factory already started")
	}

	// Signal readiness first — unblocks any List/Watch already waiting
	close(f.ready)

	logger.Info().Int("count", len(f.informers)).Msg("starting informers")

	for t, entry := range f.informers {
		if entry == nil || entry.Informer == nil || entry.Missing {
			logger.Warn().Str("gvk", t).Msg("informer entry nil or missing — skipping")
			entry.WasNeverStarted = true
			continue
		}
		// Each informer gets its own correct name
		logger.Debug().
			Str("name", entry.Name).
			Str("type", t).
			Dur("resync", entry.Resync).
			Msg("starting informer")
		go entry.Informer.Run(ctx.Done())
	}

	f.started.Store(true)
	logger.Info().Msg("informer factory started and ready")
	return nil
}

// WaitForCacheSync blocks until all informer caches have synced or ctx is done.
// It only waits on informers that were successfully created and started.
// Informers for missing CRDs (where ListWatch failed) should never be added
// to the factory, so they are naturally excluded here.
func (f *Factory) WaitForCacheSync(ctx context.Context) bool {
	// Wait for factory to be marked ready (Start() has been called).
	select {
	case <-f.ready:
	case <-ctx.Done():
		return false
	}

	f.mu.RLock()
	syncFuncs := make([]cache.InformerSynced, 0, len(f.informers))

	for _, entry := range f.informers {
		if entry == nil || entry.Missing {
			entry.WasNeverStarted = true
			logger.Warn().
				Str("name", entry.Name).
				Msg("skipping cache sync — CRD missing or not registered")
			continue
		}
		syncFuncs = append(syncFuncs, entry.Informer.HasSynced)
		logger.Debug().
			Str("name", entry.Name).
			Bool("synced", entry.Informer.HasSynced()).
			Int("count", len(syncFuncs)).
			Msg("informer cache sync state")
	}

	f.mu.RUnlock()

	// If there are no informers at all, treat as trivially synced.
	if len(syncFuncs) == 0 {
		logger.Warn().Msg("no informers registered — treating cache sync as successful")
		return true
	}

	return cache.WaitForCacheSync(ctx.Done(), syncFuncs...)
}

// Status returns a summary of all informers and their sync state.
// Used by the health server /katalog endpoint.
func (f *Factory) Status() string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	names := make([]string, 0, len(f.informers))
	for _, entry := range f.informers {
		if entry != nil {
			synced := "not synced"
			if entry.Informer.HasSynced() {
				synced = "synced"
			}
			names = append(names, fmt.Sprintf("%s(%s)", entry.Name, synced))
		}
	}
	return strings.Join(names, ", ")
}

// IsReady returns true if the factory has been started.
func (f *Factory) IsReady() bool {
	select {
	case <-f.ready:
		return true
	default:
		return false
	}
}

// Registered CRDs
func (f *Factory) Registered() map[string]*InformerEntry {
	f.mu.RLock()
	defer f.mu.RUnlock()

	out := make(map[string]*InformerEntry, len(f.informers))
	for k, v := range f.informers {
		out[k] = v
	}
	return out
}

// Missing CRDs on startup
func (f *Factory) Missing() map[string]*InformerEntry {
	f.mu.RLock()
	defer f.mu.RUnlock()

	out := make(map[string]*InformerEntry, len(f.missing))
	for k, v := range f.missing {
		out[k] = v
	}
	return out
}

// SetMissing CRDs on startup
func (f *Factory) SetMissingOnStartup(missing map[string]*InformerEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.missing = missing
}

// RemoveMissing CRDs in between runs
func (f *Factory) RemoveMissing(gvkStr string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.missing, gvkStr)
}

// IsMissing CRDs on startup
func (f *Factory) IsMissing(key string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.missing[key]
	return ok
}

// ── Komponent ─────────────────────────────────────────────────────────────────

var _ domain.Komponent = (*Factory)(nil)

func (f *Factory) Started() bool { return f.started.Load() }

func (f *Factory) Shutdown(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Informers stop when their ctx.Done() channel closes — handled by caller.
	// We just mark the factory as stopped.
	f.started.Store(false)
}

func (f *Factory) Name() string { return "informer factory" }
