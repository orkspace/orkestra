// pkg/kordinator/dependency_kordinator.go
/*
╔═══════════════════════════════════════════════════════════════════════════════╗
║                    CRD Lifecycle Management Flow                               ║
╚═══════════════════════════════════════════════════════════════════════════════╝

The retryMissingCRDs goroutine runs forever, handling both activation and deactivation
of CRDs. This is the self‑healing core of Orkestra.

─────────────────────────────────────────────────────────────────────────────────
SCENARIO 1: CRD MISSING AT STARTUP
─────────────────────────────────────────────────────────────────────────────────

Startup:
  - CRD A is missing from the cluster
  - Kordinate loop processes A → startedCh["A"] remains open
  - Kordinate continues (does NOT block) because missing CRDs are skipped
  - Retry loop starts in background

Retry loop (every postStartRetryInterval):
  - Phase 1: checks missing map
    - finds A is missing
    - calls utils.WaitForCRD() → false
    - remains in missing map
  - Phase 2: checks running CRDs (none)
  - Phase 3: checks deferred (not‑started) CRDs (A not ready due to missing)
  - Phase 4: allReady? false

Later:
  - User applies CRD A to cluster
  - Retry loop runs again
  - Phase 1: utils.WaitForCRD() → true
  - activateCRD(A) is called:
    - starts informer (entry.Informer.Run)
    - starts workers
    - closes startedCh["A"] ← UNBLOCKS dependents in Kordinate
  - Phase 3: allCRDsPresent() may still be false if other CRDs missing

Result: CRD A becomes operational without restarting Orkestra.

─────────────────────────────────────────────────────────────────────────────────
SCENARIO 2: CRD DELETED AFTER STARTUP
─────────────────────────────────────────────────────────────────────────────────

Running state:
  - CRD A is active (workers running, informer watching)
  - Retry loop runs periodically

User deletes CRD A:
  - Informer reflector starts logging "failed to list... the server could not find..."
  - Retry loop runs:
    - Phase 1: missing map (A not there)
    - Phase 2: checks running CRDs
      - calls crdExists(A) → false
      - deactivateCRD(A) is called:
        - stopCRDWorkers(A) → stops workers, drains queue
        - removes from started map
        - marks as missing in informerFactory
        - health.SetStarted(false)
        - DOES NOT close startedCh[A]
  - Phase 4: allReady becomes false

Result: CRD A becomes degraded, workers stop, but dependents continue (degraded).
No more reflector errors.

─────────────────────────────────────────────────────────────────────────────────
SCENARIO 3: CRD REAPPEARS AFTER BEING DELETED
─────────────────────────────────────────────────────────────────────────────────

After deactivation:
  - CRD A is in missing map
  - Retry loop runs:
    - Phase 1: missing map contains A
    - utils.WaitForCRD() → true
    - activateCRD(A) is called (same as scenario 1)
  - Workers restart, informer starts
  - startedCh[A] is closed again (it was never closed during deactivation,
    but we create a new channel? No — startedCh persists. Actually we don't close it
    during deactivation, so it remains open. We need to be careful: during activation,
    we should close it regardless of whether it was closed before.)

    In activateCRD, we already handle this with select/default:
        select {
        case <-ch:
            // already closed
        default:
            close(ch)
        }

Result: CRD A becomes operational again without restarting Orkestra.

─────────────────────────────────────────────────────────────────────────────────
SCENARIO 4: DEPENDENCY CHAIN WITH DELAYED ACTIVATION (FIXED BEHAVIOR)
─────────────────────────────────────────────────────────────────────────────────

StartupOrder: [A, B, C] where B depends on A:started, C depends on B:healthy

Initial state:
  - A: present
  - B: present
  - C: present

Kordinate loop:
  - A → starts, closes startedCh["A"]
  - B → dependenciesReady? true (A started) → starts, closes startedCh["B"]
  - C → dependenciesReady? false (B not yet healthy) → SKIPS (does NOT block)
  - Main loop finishes.

Retry loop (periodic):
  - Phase 3: checks not‑started CRDs
  - C: dependenciesReady? false (B still not healthy) → skip
  - Later, B becomes healthy → health checker closes healthyCh["B"]
  - Next retry tick:
    - Phase 3: C dependenciesReady? true → activateCRD(C)
    - Workers start, C becomes operational

Result: A and B start immediately; C starts only when B becomes healthy,
without blocking the main goroutine or starving other CRDs.

─────────────────────────────────────────────────────────────────────────────────
SCENARIO 5: DEPENDENCY CHAIN WITH DELETION IN THE MIDDLE
─────────────────────────────────────────────────────────────────────────────────

Running state:
  - A, B, C all active and healthy
  - D depends on C:started

User deletes CRD C:
  - Retry loop detects C missing
  - deactivateCRD(C):
    - stops workers
    - marks missing
    - DOES NOT close startedCh[C] (it's already closed from startup)

Result:
  - C is degraded, workers stopped
  - D continues running (degraded) because its dependency C is not ready
  - Health endpoints show C as degraded, D as degraded

Later, C is recreated:
  - activateCRD(C):
    - starts workers
    - attempts to close startedCh[C] (already closed, safe)
  - D's health becomes healthy again automatically

─────────────────────────────────────────────────────────────────────────────────
KEY DESIGN DECISIONS
─────────────────────────────────────────────────────────────────────────────────

1. startedCh is NEVER closed during deactivation.
   Reason: Dependents should continue running (degraded) rather than block.
   If we closed the channel, dependents would think the dependency is ready
   when it's actually missing.

2. retry loop runs FOREVER, not just at startup.
   Reason: CRDs can be deleted at any time. We need continuous monitoring.

3. activateCRD closes startedCh safely using select/default.
   Reason: startedCh may already be closed from initial startup or previous activation.

4. deactivateCRD does NOT remove from startedCh map.
   Reason: The channel is still needed for future activations.
   The channel is never closed, so it remains in the map.

5. health.SetStarted(false) on deactivation.
   Reason: Allows health endpoint to show the CRD as not started.

6. Main Kordinate loop NEVER BLOCKS on dependency conditions.
   Reason: Dependencies with "healthy" requirement may take arbitrary time.
   Blocking would starve other CRDs that are ready to start. Instead, we skip
   CRDs whose dependencies aren't ready and rely on the retry loop to activate
   them later when conditions are satisfied.

╚═══════════════════════════════════════════════════════════════════════════════╝
*/
package kordinator

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/event"
	"github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	ork_autoscaler "github.com/orkspace/orkestra/pkg/runtime/autoscaler"
	"github.com/orkspace/orkestra/pkg/runtime/informer"
	"github.com/orkspace/orkestra/pkg/runtime/informer/observe"
	"github.com/orkspace/orkestra/pkg/runtime/queue"
)

// DependencyKordinator extends the base Kontroller with dependency‑aware startup.
// It ensures CRDs start in topological order and shut down in reverse order.
type DependencyKordinator struct {
	*Kontroller

	depGraph       *katalog.DependencyGraph
	defaultWorkers int
	startedAt      time.Time
	queueReg       *queue.QueueRegistry
	drainTimeout   time.Duration

	// Orkestra and katalog health
	anyOnline atomic.Bool
	allOnline atomic.Bool
	orkHealth *OrkestraHealth

	// startedCh[gvk] is closed when a CRD has fully started its workers.
	startedCh map[string]chan struct{}

	// healthyCh[gvk] is closed after the CRD handles first reconciliation.
	healthyCh map[string]chan struct{}

	// missingChildGVKs tracks GVKs declared in onReconcile.custom / onCreate.custom
	// blocks that are not yet available as CRDs in the cluster.
	missingChildGVKs map[string]schema.GroupVersionKind
}

// NewDependencyKordinator constructs a dependency‑aware kordinator.
// It embeds the base Kontroller and handles dependencies in the correct order.
func NewDependencyKordinator(
	kube *kubeclient.Kubeclient,
	factory *informer.Factory,
	observer *observe.Observer,
	katalog *ResourceKatalog,
	kat *katalog.Katalog,
	events *event.Event,
	hs domain.Health,
	queueRegistry *queue.QueueRegistry,
	defaultWorkqueue *queue.Workqueue,
	crdHealthMap map[string]*CRDHealth,
	orkHealth *OrkestraHealth,
	defaultWorkers int,
	depGraph *katalog.DependencyGraph,
	drainTimeout time.Duration,
) *DependencyKordinator {

	kord := &DependencyKordinator{
		Kontroller: NewKontroller(
			kube, factory, observer, katalog, kat,
			events, hs, crdHealthMap, orkHealth,
			queueRegistry, defaultWorkqueue, defaultWorkers,
		),
		orkHealth:      orkHealth,
		depGraph:       depGraph,
		defaultWorkers: defaultWorkers,
		queueReg:       queueRegistry,
		drainTimeout:   drainTimeout,
		startedCh:      make(map[string]chan struct{}),
		healthyCh:      make(map[string]chan struct{}),
	}

	kord.anyOnline.Store(false)
	return kord
}

// Kordinate starts CRDs in dependency order and blocks until leadership is lost.
// When leadership ends, it shuts down CRDs in reverse dependency order.
//
// The startup loop is non‑blocking: if a CRD's dependencies are not yet
// satisfied (e.g., waiting for "healthy"), the CRD is skipped. The background
// retry loop will activate it later when dependencies become ready.
func (k *DependencyKordinator) Kordinate(ctx context.Context) {
	logger.Info().Str("component", k.Name()).Msg("starting")
	k.startedAt = time.Now()

	// Mark as ready immediately - the kordinator can serve requests
	k.orkHealth.SetOrkReady()
	k.orkHealth.SetIsKonductor(true)

	// Track allOnline
	k.allOnline.Store(false)
	var totalCRDs, onlineCRDs int

	// Startup order
	startupOrder := k.depGraph.StartupOrder()
	logger.Info().Str("order", strings.Join(startupOrder, " → ")).Msg("startup order")

	totalCRDs = len(startupOrder)

	// Build name → GVK mapping
	nameToGVK := make(map[string]string)
	for _, name := range startupOrder {
		node := k.depGraph.GetNode(name)
		if node == nil {
			continue
		}
		nameToGVK[name] = node.CRD.GroupVersionKind.String()
	}

	// Create started + healthy channels for all CRDs
	for _, name := range startupOrder {
		node := k.depGraph.GetNode(name)
		if node == nil {
			continue
		}
		gvk := node.CRD.GroupVersionKind.String()
		k.startedCh[gvk] = make(chan struct{})
		k.healthyCh[gvk] = make(chan struct{})
	}

	// Collect custom child CRDs and detect which are missing at startup.
	// These are GVKs declared in onReconcile.custom / onCreate.custom blocks across all CRDs.
	k.missingChildGVKs = collectCustomChildGVKs(k.katalog)
	for gvkStr, gvk := range k.missingChildGVKs {
		gvkCopy := gvk
		ok, _ := k.crdExists(&gvkCopy)
		if ok {
			delete(k.missingChildGVKs, gvkStr)
		} else {
			logger.Warn().Str("gvk", gvkStr).Msg("custom child CRD not available at startup — will retry")
		}
	}

	// START RETRY LOOP ONCE, BEFORE ANY BLOCKING
	go k.retryMissingCRDs(ctx)

	// Start dependency health checker (runs until ctx is cancelled)
	go k.dependencyHealthChecker(ctx)

	// Process CRDs in dependency order — but do NOT block on unsatisfied conditions.
	// Any CRD that cannot start immediately will be picked up by the retry loop.
	for _, name := range startupOrder {
		node := k.depGraph.GetNode(name)
		if node == nil {
			continue
		}
		crd := node.CRD
		gvk := crd.GroupVersionKind.String()

		// Check if dependencies are satisfied RIGHT NOW
		if !k.dependenciesReady(crd, nameToGVK) {
			logger.Info().Str("crd", name).Msg("dependencies not ready — deferring activation")
			continue // do NOT block; let retry loop handle it
		}

		// Check if CRD exists in cluster
		if k.informerFactory.IsMissing(gvk) {
			logger.Debug().Str("crd", name).Str("gvk", gvk).Msg("CRD missing — workers not started, waiting for retry")
			// DO NOT close startedCh or healthyCh — dependents must block
			continue
		}

		// CRD exists — start workers
		workers := k.katalog.GetWorkers(gvk, k.defaultWorkers)
		logger.Info().Str("gvk", gvk).Int("workers", workers).Msg("starting workers")
		k.startCRDWorkers(ctx, gvk, workers)

		// Update health
		k.crdHealthMap[gvk].queueReg = k.queueReg

		// Signal dependents: STARTED ONLY
		close(k.startedCh[gvk])
		logger.Info().Str("crd", name).Str("gvk", gvk).Int("workers", workers).Msg("workers started")

		// DO NOT close healthyCh here.
		// healthyCh will be closed by the health checker when the CRD becomes healthy.

		k.anyOnline.Store(true)
		onlineCRDs++
	}

	// Mark controller started
	k.startedKtrl.Store(true)
	if k.anyOnline.Load() {
		logger.Info().Str("component", k.Name()).Int("crds_online", onlineCRDs).Msg("started")
	} else {
		logger.Warn().Str("component", k.Name()).Msg("started — all CRDs missing, waiting for retry loop")
	}

	// Compute final katalog health
	if onlineCRDs == totalCRDs {
		k.allOnline.Store(true)
		k.orkHealth.allOnline.Store(true)
		k.orkHealth.SetKatalogReady()
	} else {
		k.allOnline.Store(false)
		k.orkHealth.allOnline.Store(false)
		k.orkHealth.SetKatalogDegraded()
	}

	// Block until leadership lost
	<-ctx.Done()
	logger.Info().Msg("leadership lost — beginning dependency-aware shutdown")
	k.hs.Unhealthy()
	k.orkHealth.SetIsKonductor(false)
	k.orkHealth.SetOrkDegraded()

	// Shut down CRDs in reverse dependency order
	shutdownOrder := k.depGraph.ShutdownOrder()
	logger.Info().Str("order", strings.Join(shutdownOrder, " → ")).Msg("shutdown order")
	for _, name := range shutdownOrder {
		logger.Info().Str("crd", name).Msg("shutting down CRD")
		gvk := k.depGraph.GetNode(name).CRD.GroupVersionKind.String()
		k.stopCRDWorkers(ctx, gvk)
	}

	logger.Info().Str("component", k.Name()).Msg("drained and stopped")
}

// dependenciesReady returns true if all declared dependencies are currently
// satisfied (i.e., the required channel is already closed).
// This check is non‑blocking.
func (k *DependencyKordinator) dependenciesReady(crd types.CRDEntry, nameToGVK map[string]string) bool {
	for depName, depCond := range crd.DependsOn {
		depGVK, ok := nameToGVK[depName]
		if !ok {
			logger.Error().Str("crd", crd.Name).Str("dependency", depName).Msg("dependency GVK not found")
			return false
		}
		switch strings.ToLower(depCond.Condition) {
		case string(types.DependencyConditionHealthy):
			select {
			case <-k.healthyCh[depGVK]:
				// channel closed → dependency healthy
			default:
				return false
			}
		default: // started
			select {
			case <-k.startedCh[depGVK]:
				// channel closed → dependency started
			default:
				return false
			}
		}
	}
	return true
}

// startCRDWorkers starts a worker pool for a specific CRD and is invoked in dependency order.
// autoscalerRunner is a local interface satisfied by GenericReconciler when
// autoscale: is declared. Defined here (not imported from reconciler) to avoid
// import cycles — reconciler already imports kordinator.
type autoscalerRunner interface {
	RunAutoscaler(ctx context.Context)
}

// queueInjector is a local interface for injecting the per-CRD workqueue into
// the reconciler after construction. Avoids import cycles.
type queueInjector interface {
	SetQueue(wq *queue.Workqueue)
}

// resyncLoopStarter is a local interface for starting the adjustable resync
// goroutine inside the reconciler. Avoids import cycles.
type resyncLoopStarter interface {
	StartResyncLoop(ctx context.Context)
}

// workerSpawner is a local interface for injecting the goroutine-spawn function
// into the reconciler. When the autoscaler calls ResizeWorkers(n) with n > old,
// the reconciler calls the injected function to start the additional goroutines.
// Avoids import cycles (reconciler does not import kordinator).
type workerSpawner interface {
	SetSpawnWorker(fn func())
}

// autoMetricsExporter is a local interface for reading the live AutoMetrics from
// a reconciler, so it can be registered in the cross-metrics registry at startup.
type autoMetricsExporter interface {
	GetAutoMetrics() *ork_autoscaler.AutoMetrics
}

// workerInfoProvider is a local interface for reading a live WorkerInfo snapshot
// from a reconciler. Used to populate the /katalog/{crd} handler response.
type workerInfoProvider interface {
	WorkerInfo(configuredResync string, configuredWorkers, configuredQueueDepth int) *ork_autoscaler.WorkerInfo
}

// rollbackNotifierSetter is a local interface for injecting CRDHealth rollback
// callbacks into the reconciler. Called once after reconciler construction.
type rollbackNotifierSetter interface {
	SetRollbackNotifiers(onTrigger, onClear func())
}

func (k *DependencyKordinator) startCRDWorkers(ctx context.Context, gvk string, workers int) {
	entry, ok := k.katalog.Get(gvk)
	if !ok {
		logger.Fatal().Str("gvk", gvk).Msg("no katalog entry found")
		return
	}

	// Compute the goroutine count: start max(baseline, override) goroutines so
	// that the autoscaler can scale up without spawning new goroutines at runtime.
	// The semaphore inside the reconciler gates effective concurrency to `workers`.
	//
	// TODO: concept in development
	goroutines := workers
	if ob := entry.CRD.OperatorBox.Autoscale; ob != nil {
		if ob.Do.Workers != nil && *ob.Do.Workers > goroutines {
			goroutines = *ob.Do.Workers
		}
	}

	crdCtx, cancel := context.WithCancel(ctx)
	wg := &sync.WaitGroup{}

	//
	rec := entry.ReconcilerFactory()

	// Inject the per-CRD workqueue so SetQueueDepthLimit and the resync goroutine
	// can reach the right queue without importing the reconciler package.
	if wq, ok := k.queueReg.For(gvk); ok {
		if qi, ok := rec.(queueInjector); ok {
			qi.SetQueue(wq)
		}
	}

	// Register this operatorbox's AutoMetrics in the global cross-metrics registry
	// so other operatorboxes can reference it via cross.<kind>.metrics.* in autoscale conditions.
	if exporter, ok := rec.(autoMetricsExporter); ok {
		if m := exporter.GetAutoMetrics(); m != nil {
			// Register under the katalog CRD name (lowercase spec.crds key), matching
			// how cross: declarations reference other CRDs: cross.<crd-name>.metrics.*
			ork_autoscaler.GlobalCrossMetricsRegistry.Register(entry.CRD.Name, m)
		}
	}

	// Inject the goroutine-spawn function into the reconciler. When the autoscaler
	// scales UP (ResizeWorkers(n) with n > current), the reconciler calls this to
	// start the additional goroutines. This keeps goroutine count == semaphore capacity.
	workerCounter := atomic.Int64{}
	spawnWorker := func() {
		n := workerCounter.Add(1)
		wg.Add(1)
		workerID := fmt.Sprintf("%s-autoscale-worker-%d", gvk, n)
		workerID = strings.ReplaceAll(workerID, ",", "")
		workerID = strings.ReplaceAll(workerID, " ", "-")
		k.crdHealthMap[gvk].workerStates.Store(workerID, WorkerStateIdle)
		go func(id string) {
			defer wg.Done()
			k.runWorkerForGVK(crdCtx, gvk, id)
		}(workerID)
	}
	if ws, ok := rec.(workerSpawner); ok {
		ws.SetSpawnWorker(spawnWorker)
	}

	// Inject rollback notifiers so reconciler can update CRDHealth on rollback events.
	if rns, ok := rec.(rollbackNotifierSetter); ok {
		health := k.crdHealthMap[gvk]
		rns.SetRollbackNotifiers(
			health.RecordRollbackTriggered,
			health.RecordRollbackCleared,
		)
	}

	// Wire the WorkerInfo provider so the /katalog/{crd} handler can report
	// live concurrency metrics without importing the reconciler package.
	if wip, ok := rec.(workerInfoProvider); ok {
		k.crdHealthMap[gvk].SetWorkerInfoFn(func() *ork_autoscaler.WorkerInfo {
			info := wip.WorkerInfo(entry.CRD.OperatorBox.Reconciler.Resync.String(), workers, entry.CRD.OperatorBox.Reconciler.Queue.MaxDepth)
			return info
		})
	}

	// Wire AutoMetrics as a map so the /katalog/{crd} response includes a "metrics"
	// key. Cross-binary autoscale conditions use this endpoint as their source.endpoint
	// HTTP fallback — same resolution path as readCross uses for cross-CRD observation.
	if exporter, ok := rec.(autoMetricsExporter); ok {
		if m := exporter.GetAutoMetrics(); m != nil {
			k.crdHealthMap[gvk].SetAutoMetricsFn(m.AsMap)
		}
	}

	// Start autoscaler goroutine if the reconciler supports it.
	if runner, ok := rec.(autoscalerRunner); ok {
		go runner.RunAutoscaler(crdCtx)
	}

	// Start adjustable resync goroutine if autoscale is declared.
	if entry.CRD.OperatorBox.Autoscale != nil {
		if rl, ok := rec.(resyncLoopStarter); ok {
			go rl.StartResyncLoop(crdCtx)
		}
	}

	k.mu.Lock()
	k.reconcilers[gvk] = rec
	k.cancelFuncs[gvk] = cancel
	k.wgs[gvk] = wg
	k.crdHealthMap[gvk].SetStarted()

	// Start only the declared baseline goroutines. The autoscaler scales up by
	// calling spawnWorker (injected above) rather than pre-allocating max goroutines.
	k.crdHealthMap[gvk].SetTotalWorkers(int32(workers))
	k.crdHealthMap[gvk].gvk = gvk
	k.started[gvk] = true
	k.total[gvk]++
	k.mu.Unlock()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		workerID := fmt.Sprintf("%s-worker-%d", gvk, i)
		workerID = strings.ReplaceAll(workerID, ",", "")
		workerID = strings.ReplaceAll(workerID, " ", "-")
		k.crdHealthMap[gvk].workerStates.Store(workerID, WorkerStateIdle)
		go func(id string) {
			defer wg.Done()
			k.runWorkerForGVK(crdCtx, gvk, id)
		}(workerID)
	}

	// Start secondary observers declared by the operator box.
	k.observer.Observe(crdCtx, entry.CRD)
}

// stopCRDWorkers cancels the CRD context and waits for all workers to drain.
func (k *DependencyKordinator) stopCRDWorkers(ctx context.Context, gvk string) {
	k.mu.RLock()
	cancel, okCancel := k.cancelFuncs[gvk]
	wg, okWG := k.wgs[gvk]
	k.mu.RUnlock()

	// Step 1: signal workers to stop accepting new work
	if okCancel {
		cancel()
	}

	// Step 2: shut down the queue — this unblocks any worker
	// blocked on queue.GetWithContext() waiting for the next item.
	// Without this, workers that finished their reconcile and
	// are waiting for work will never exit.
	if wq, ok := k.queueReg.For(gvk); ok {
		wq.Shutdown(ctx)
	}

	if !okWG {
		return
	}

	// Step 3: Reset worker counts after shutdown
	if health, ok := k.crdHealthMap[gvk]; ok {
		health.ResetWorkerCounts()
		health.workerStates.Range(func(key, value interface{}) bool {
			health.workerStates.Store(key, WorkerStateStopped)
			return true
		})
	}

	// Step 4: wait for workers to drain — with a timeout.
	// The timeout is the safety net for stuck reconciles, not
	// an execution budget for normal shutdown.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info().Str("gvk", gvk).Msg("workers drained cleanly")
	case <-time.After(k.drainTimeout):
		logger.Warn().Str("gvk", gvk).
			Dur("timeout", k.drainTimeout).
			Msg("drain timeout exceeded — workers may still be running. " +
				"Consider increasing SHUTDOWN_TIMEOUT if reconciles call slow external APIs.")
	}
}

// Name returns the name of the dependency kordinator
func (k *DependencyKordinator) Name() string {
	return "orkestra dependency kordinator"
}

// NameToCRD returns the CRD for a given name
func (k *DependencyKordinator) NameToCRD(name string) types.CRDEntry {
	return k.depGraph.GetNode(name).CRD
}

// NameToGVK returns the GVK for a given name
func (k *DependencyKordinator) NameToGVK(name string) schema.GroupVersionKind {
	return k.depGraph.GetNode(name).CRD.GroupVersionKind
}

// GVKToCRD returns the CRD entry for a given gvk
func (k *DependencyKordinator) GVKToCRD(gvk schema.GroupVersionKind) types.CRDEntry {
	entry, ok := k.katalog.Get(gvk.String())
	if !ok {
		return types.CRDEntry{}
	}
	return entry.CRD
}

// NameToGVKMap returns a map of names to gvk string
func (k *DependencyKordinator) NameToGVKMap() map[string]string {
	nameToGVK := make(map[string]string)
	for _, name := range k.depGraph.StartupOrder() {
		node := k.depGraph.GetNode(name)
		if node != nil {
			nameToGVK[name] = node.CRD.GroupVersionKind.String()
		}
	}
	return nameToGVK
}
