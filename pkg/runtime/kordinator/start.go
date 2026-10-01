package kordinator

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/orkspace/orkestra/pkg/logger"
	ork_autoscaler "github.com/orkspace/orkestra/pkg/runtime/autoscaler"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// startCRDWorkers activates a CRD: builds runtime state, wires health
// callbacks, builds and registers the reconciler, then starts all goroutines.
func (k *DependencyKordinator) startCRDWorkers(ctx context.Context, gvk string, workers int) {
	entry, ok := k.katalog.Get(gvk)
	if !ok {
		logger.Fatal().Str("gvk", gvk).Msg("no katalog entry found")
		return
	}

	crdCtx, cancel := context.WithCancel(ctx)
	wg := &sync.WaitGroup{}

	rt := k.buildCRDRuntime(crdCtx, gvk, workers, wg, entry.CRD)
	k.mu.Lock()
	k.runtimeMap[gvk] = rt
	k.mu.Unlock()

	k.wireCRDHealthCallbacks(gvk, rt, workers, entry.CRD)

	rec := entry.ReconcilerFactory()

	if rt.autoscaler != nil {
		go rt.autoscaler.Run(crdCtx)
	}
	if entry.CRD.OperatorBox.EffectiveAutoscale() != nil {
		k.startResyncLoop(crdCtx, gvk)
	}

	k.mu.Lock()
	k.reconcilers[gvk] = rec
	k.cancelFuncs[gvk] = cancel
	k.wgs[gvk] = wg
	k.crdHealthMap[gvk].SetStarted()
	k.crdHealthMap[gvk].SetTotalWorkers(int32(workers))
	k.crdHealthMap[gvk].SetGVK(gvk)
	k.started[gvk] = true
	k.total[gvk]++
	k.mu.Unlock()

	k.launchWorkers(crdCtx, gvk, workers, wg)
	k.observer.Observe(crdCtx, entry.CRD)
}

// buildCRDRuntime creates the per-CRD semaphore, AutoMetrics, spawnWorker
// closure, and (when declared) the autoscaler. The returned runtime is not yet
// registered in runtimeMap — the caller does that under the lock.
func (k *DependencyKordinator) buildCRDRuntime(
	crdCtx context.Context,
	gvk string,
	workers int,
	wg *sync.WaitGroup,
	crd orktypes.CRDEntry,
) *perCRDRuntime {
	sem := ork_autoscaler.NewResizableSemaphore(workers)
	autoMet := ork_autoscaler.NewAutoMetrics(sem)
	rt := &perCRDRuntime{
		sem:         sem,
		autoMetrics: autoMet,
	}

	workerCounter := atomic.Int64{}
	rt.spawnWorker = func() {
		n := workerCounter.Add(1)
		wg.Add(1)
		workerID := workerID(gvk, "autoscale-worker", int(n))
		k.crdHealthMap[gvk].MarkStartupWorkerIdle(workerID)
		go func(id string) {
			defer wg.Done()
			k.runWorkerForGVK(crdCtx, gvk, id)
		}(workerID)
	}

	wq, _ := k.queueReg.For(gvk)
	if crd.AutoscaleEnabled() {
		target := &kordinatorTarget{rt: rt, wq: wq, gvk: gvk}
		baseline := orktypes.AutoscaleBaseline{
			Workers:  workers,
			MaxDepth: crd.SetQueueDepth(0),
			Resync:   crd.SetResync(0),
		}
		rt.autoscaler = ork_autoscaler.NewAutoscaler(
			k.kube.Clientset(),
			crd.APITypes.Kind,
			crd.Box().EffectiveAutoscale(),
			baseline,
			target,
			autoMet,
			crd.Box().EffectiveCross(),
		)
	}

	ork_autoscaler.GlobalCrossMetricsRegistry.Register(crd.Name, autoMet)
	return rt
}

// wireCRDHealthCallbacks registers AutoMetrics and WorkerInfo providers into
// the CRD's health tracker so the /katalog endpoint can read them.
func (k *DependencyKordinator) wireCRDHealthCallbacks(
	gvk string,
	rt *perCRDRuntime,
	workers int,
	crd orktypes.CRDEntry,
) {
	k.crdHealthMap[gvk].SetAutoMetricsFn(rt.autoMetrics.AsMap)

	k.crdHealthMap[gvk].SetWorkerInfoFn(func() *ork_autoscaler.WorkerInfo {
		maxWorkers := workers
		if rt.autoscaler != nil {
			if snap := rt.autoscaler.Snapshot(); snap != nil && snap.EffectiveWorkers > maxWorkers {
				maxWorkers = snap.EffectiveWorkers
			}
		}
		info := ork_autoscaler.BuildWorkerInfo(
			rt.sem,
			rt.autoMetrics,
			workers,
			crd.SetQueueDepth(0),
			crd.SetResync(0).String(),
			maxWorkers,
			rt.autoscaler != nil,
			rt.autoscaler.Snapshot(),
		)
		return &info
	})
}

// launchWorkers spawns the baseline worker goroutines for a CRD.
func (k *DependencyKordinator) launchWorkers(ctx context.Context, gvk string, workers int, wg *sync.WaitGroup) {
	for i := 0; i < workers; i++ {
		wg.Add(1)
		id := workerID(gvk, "worker", i)
		k.crdHealthMap[gvk].MarkStartupWorkerIdle(id)
		go func(workerID string) {
			defer wg.Done()
			k.runWorkerForGVK(ctx, gvk, workerID)
		}(id)
	}
}

// workerID builds a worker identifier from a GVK, role, and index.
func workerID(gvk, role string, n int) string {
	id := fmt.Sprintf("%s-%s-%d", gvk, role, n)
	id = strings.ReplaceAll(id, ",", "")
	id = strings.ReplaceAll(id, " ", "-")
	return id
}
