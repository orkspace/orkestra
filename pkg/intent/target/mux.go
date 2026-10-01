package target

import (
	"context"
	"fmt"
	"sync"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/runtime/autoscaler"
	orkqueue "github.com/orkspace/orkestra/pkg/runtime/queue"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

// MuxReconciler dispatches Reconcile calls to per-target domain.Reconciler
// instances based on the serve-target annotation on the incoming CR.
//
// CRs with no target annotation (or an unknown target) are handled by the
// fallback reconciler — typically the CRD-level generic.Reconciler.
//
// All CRD-level infrastructure concerns (queue injection, autoscale, resync,
// metrics) are forwarded to the fallback reconciler so startCRDWorkers can
// inject them via the same interface checks it uses for a plain generic.Reconciler.
//
// The target cache is the only mutable state: it stores "ns/name" → target
// so that deletion reconcile cycles (where the object is gone and no annotation
// can be read) still route to the same reconciler that handled the last create.
type MuxReconciler struct {
	informer    cache.SharedIndexInformer
	targets     map[string]domain.Reconciler // target name → reconciler
	fallback    domain.Reconciler            // handles no-target / unknown-target CRs
	targetCache sync.Map                     // "ns/name" → string; evicted on reconcile-not-found
}

func NewMuxReconciler(
	informer cache.SharedIndexInformer,
	targets map[string]domain.Reconciler,
	fallback domain.Reconciler,
) *MuxReconciler {
	return &MuxReconciler{
		informer: informer,
		targets:  targets,
		fallback: fallback,
	}
}

var _ domain.Reconciler = (*MuxReconciler)(nil)

// Reconcile looks up the CR by key, resolves its target, and delegates to the
// matching per-target reconciler (or the fallback when no match is found).
func (m *MuxReconciler) Reconcile(ctx context.Context, req domain.Request) (domain.Result, error) {
	key := req.Key
	raw, exists, err := m.informer.GetIndexer().GetByKey(key)
	if err != nil {
		return domain.Result{}, fmt.Errorf("mux: getting %q from store: %w", key, err)
	}
	if !exists {
		return m.reconcileNotFound(ctx, key)
	}

	obj, ok := raw.(domain.Object)
	if !ok {
		return domain.Result{}, fmt.Errorf("mux: type assertion failed for %q (got %T)", key, raw)
	}

	target := ResolveTargetFromAnnotations(obj.GetAnnotations())
	m.targetCache.Store(key, target)
	return m.reconcilerFor(target).Reconcile(ctx, req)
}

// reconcilerFor returns the reconciler registered for target, or the fallback.
func (m *MuxReconciler) reconcilerFor(target string) domain.Reconciler {
	if target != "" {
		if rec, ok := m.targets[target]; ok {
			return rec
		}
	}
	return m.fallback
}

// reconcileNotFound routes deletion cycles to the reconciler that last handled
// this key. The cache entry is removed after routing so stale entries don't
// accumulate for long-lived operators.
func (m *MuxReconciler) reconcileNotFound(ctx context.Context, key string) (domain.Result, error) {
	target := ""
	if v, ok := m.targetCache.Load(key); ok {
		target, _ = v.(string)
	}
	defer m.targetCache.Delete(key)
	ns, name, _ := cache.SplitMetaNamespaceKey(key)
	return m.reconcilerFor(target).Reconcile(ctx, domain.Request{
		Key:            key,
		NamespacedName: apitypes.NamespacedName{Namespace: ns, Name: name},
	})
}

// ── CRD-level infrastructure forwarding ──────────────────────────────────────
// startCRDWorkers performs type assertions on the reconciler it receives from
// ReconcilerFactory(). MuxReconciler forwards each interface to the fallback so
// queue injection, autoscale, resync, and metrics all work as if the fallback
// were the direct reconciler.
//
// Autoscale, queue depth, and resync are CRD-level concerns — they govern the
// worker pool, not individual targets. Per-target reconcilers do not participate
// in these infrastructure calls today. Might become per-target tomorrow.

func (m *MuxReconciler) SetQueue(wq *orkqueue.Workqueue) {
	if qi, ok := m.fallback.(interface{ SetQueue(*orkqueue.Workqueue) }); ok {
		qi.SetQueue(wq)
	}
}

func (m *MuxReconciler) SetSpawnWorker(fn func()) {
	if ws, ok := m.fallback.(interface{ SetSpawnWorker(func()) }); ok {
		ws.SetSpawnWorker(fn)
	}
}

func (m *MuxReconciler) GetAutoMetrics() *autoscaler.AutoMetrics {
	if exporter, ok := m.fallback.(interface {
		GetAutoMetrics() *autoscaler.AutoMetrics
	}); ok {
		return exporter.GetAutoMetrics()
	}
	return nil
}

func (m *MuxReconciler) WorkerInfo(configuredResync string, configuredWorkers, configuredQueueDepth int) *autoscaler.WorkerInfo {
	if wip, ok := m.fallback.(interface {
		WorkerInfo(string, int, int) *autoscaler.WorkerInfo
	}); ok {
		return wip.WorkerInfo(configuredResync, configuredWorkers, configuredQueueDepth)
	}
	return nil
}

func (m *MuxReconciler) RunAutoscaler(ctx context.Context) {
	if runner, ok := m.fallback.(interface{ RunAutoscaler(context.Context) }); ok {
		runner.RunAutoscaler(ctx)
	}
}

func (m *MuxReconciler) StartResyncLoop(ctx context.Context) {
	if rl, ok := m.fallback.(interface{ StartResyncLoop(context.Context) }); ok {
		rl.StartResyncLoop(ctx)
	}
}
