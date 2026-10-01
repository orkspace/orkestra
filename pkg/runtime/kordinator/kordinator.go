package kordinator

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/event"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/informer"
	"github.com/orkspace/orkestra/pkg/runtime/informer/observe"
	"github.com/orkspace/orkestra/pkg/runtime/kordinator/vitals"
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
	orkHealth *vitals.RuntimeHealth

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
	crdHealthMap map[string]*vitals.CRDHealth,
	orkHealth *vitals.RuntimeHealth,
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
		if h, ok := k.crdHealthMap[gvk]; ok {
			h.SetQueueReg(k.queueReg)
		}

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
		k.orkHealth.SetAllOnline()
		k.orkHealth.SetKatalogReady()
	} else {
		k.allOnline.Store(false)
		k.orkHealth.SetAllNotOnline()
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
func (k *DependencyKordinator) dependenciesReady(crd orktypes.CRDEntry, nameToGVK map[string]string) bool {
	for depName, depCond := range crd.DependsOn {
		depGVK, ok := nameToGVK[depName]
		if !ok {
			logger.Error().Str("crd", crd.Name).Str("dependency", depName).Msg("dependency GVK not found")
			return false
		}
		switch strings.ToLower(depCond.Condition) {
		case string(orktypes.DependencyConditionHealthy):
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

// Name returns the name of the dependency kordinator
func (k *DependencyKordinator) Name() string {
	return "orkestra dependency kordinator"
}

// NameToCRD returns the CRD for a given name
func (k *DependencyKordinator) NameToCRD(name string) orktypes.CRDEntry {
	return k.depGraph.GetNode(name).CRD
}

// NameToGVK returns the GVK for a given name
func (k *DependencyKordinator) NameToGVK(name string) schema.GroupVersionKind {
	return k.depGraph.GetNode(name).CRD.GroupVersionKind
}

// GVKToCRD returns the CRD entry for a given gvk
func (k *DependencyKordinator) GVKToCRD(gvk schema.GroupVersionKind) orktypes.CRDEntry {
	entry, ok := k.katalog.Get(gvk.String())
	if !ok {
		return orktypes.CRDEntry{}
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
