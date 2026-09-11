package kordinator

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"errors"

	"github.com/orkspace/orkestra/domain"

	"github.com/orkspace/orkestra/pkg/event"
	"github.com/orkspace/orkestra/pkg/katalog"
	orktypes "github.com/orkspace/orkestra/pkg/types"

	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/informer"
	"github.com/orkspace/orkestra/pkg/runtime/informer/observe"
	"github.com/orkspace/orkestra/pkg/runtime/queue"
)

var _ domain.Komponent = (*Kontroller)(nil)

// Every map has the same key GVK
type Kontroller struct {
	kube             *kubeclient.Kubeclient
	informerFactory  *informer.Factory
	observer         *observe.Observer
	event            *event.Event
	katalog          *ResourceKatalog
	kat              *katalog.Katalog
	queueRegistry    *queue.QueueRegistry
	defaultWorkqueue *queue.Workqueue
	failureThreshold map[string]int

	hs           domain.Health
	crdHealthMap map[string]*CRDHealth
	orkHealth    *OrkestraHealth

	defaultWorkers int
	startedKtrl    atomic.Bool
	started        map[string]bool
	deactivated    map[string]bool
	cancelFuncs    map[string]context.CancelFunc
	wgs            map[string]*sync.WaitGroup
	mu             sync.RWMutex
	reconcilers    map[string]domain.Reconciler
	crds           []orktypes.CRDEntry

	// Error rate
	total  map[string]int
	failed map[string]int
}

func NewKontroller(
	kube *kubeclient.Kubeclient,
	informerFactory *informer.Factory,
	observer *observe.Observer,
	katalog *ResourceKatalog,
	kat *katalog.Katalog,
	event *event.Event,
	hs domain.Health,
	crdHealthMap map[string]*CRDHealth,
	orkHealth *OrkestraHealth,
	queueRegistry *queue.QueueRegistry,
	defaultWorkqueue *queue.Workqueue,
	defaultWorkers int,
) *Kontroller {
	k := &Kontroller{
		kube:             kube,
		informerFactory:  informerFactory,
		observer:         observer,
		katalog:          katalog,
		kat:              kat,
		event:            event,
		hs:               hs,
		defaultWorkqueue: defaultWorkqueue,
		queueRegistry:    queueRegistry,
		defaultWorkers:   defaultWorkers,
		crdHealthMap:     crdHealthMap,
		started:          make(map[string]bool),
		deactivated:      make(map[string]bool),
		cancelFuncs:      make(map[string]context.CancelFunc),
		total:            make(map[string]int),
		failed:           make(map[string]int),
		wgs:              make(map[string]*sync.WaitGroup),
		reconcilers:      make(map[string]domain.Reconciler),
		failureThreshold: make(map[string]int),
	}

	// Load registry entries
	for gvk, entry := range katalog.Entries() {
		k.crds = append(k.crds, entry.CRD)
		k.failureThreshold[gvk] = entry.CRD.OperatorBox.Reconciler.Queue.FailureThreshold
	}

	return k
}

func (k *Kontroller) Start(ctx context.Context) error {
	// CRD checks now carried out in informer
	// Kontroller just accepts that crds have been checked, listens to know if any is missing
	// Then marks as degraded
	for _, crd := range k.crds {
		gvk := crd.GroupVersionKind.String()

		if !k.informerFactory.IsMissing(gvk) {
			continue
		}
		logger.Warn().Str("gvk", gvk).Msg("CRD missing — marking as degraded")
		k.crdHealthMap[gvk].RecordStartupFailure(errors.New("CRD not found"), crd.OperatorBox.Reconciler.Queue.FailureThreshold)
	}

	// All CRDs confirmed (filtered by informer) — now sync caches
	logger.Debug().Msg("waiting for all informer caches to sync...")
	if !k.informerFactory.WaitForCacheSync(ctx) {
		return fmt.Errorf("failed to sync one or more informer caches")
	}
	logger.Info().Msg("all informer caches synced")

	// Build reconcilers now — kube, ev, and REST clients are all live
	logger.Debug().Msg("building reconcilers...")
	for gvk, entry := range k.katalog.Entries() {
		rec := entry.ReconcilerFactory() // ← safe here, manager has started everything
		k.mu.Lock()
		k.reconcilers[gvk] = rec
		k.mu.Unlock()
		logger.Debug().Str("gvk", gvk).Msg("reconciler built")
	}

	return nil
}

// MissingCRDs returns a map of missing crds keyed by gvk
func (k *Kontroller) MissingCRDs() map[string]*informer.InformerEntry {
	return k.informerFactory.Missing()
}

// Set the controller ready
func (k *Kontroller) SetReady(h domain.Health) {
	h.SetReady()
}

// Set the controller to degraded
func (k *Kontroller) Degraded(h domain.Health) {
	h.Degraded()
}

// Healthy mark on startup
func (k *Kontroller) Started() bool { return k.startedKtrl.Load() }

// Shutdown gracefully stops orkestra
func (k *Kontroller) Shutdown(ctx context.Context) {}

// Controller name
func (k *Kontroller) Name() string {
	return "orkestra kontroller"
}

// Handle failure writes for concurrency
func (k *Kontroller) failedReconcile(gvk string) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.failed[gvk]++
}

// Handle success writes for concurrency
func (k *Kontroller) successReconcile(gvk string) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.total[gvk]++
}
