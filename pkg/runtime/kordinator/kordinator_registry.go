// pkg/config/pkg/kordinator/registry.go
package kordinator

import (
	"sync"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/runtime/kordinator/contract"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/client-go/tools/cache"
)

type ResourceKatalog struct {
	mu      sync.Mutex
	entries map[string]contract.RegistryEntry
}

// compile time check
var _ contract.RuntimeResourceKatalog = (*ResourceKatalog)(nil)

func NewKordinatorRegistry() *ResourceKatalog {
	return &ResourceKatalog{
		entries: make(map[string]contract.RegistryEntry),
	}
}

func (r *ResourceKatalog) Register(
	gvk string,
	crd orktypes.CRDEntry,
	inf cache.SharedIndexInformer,
	rec func() domain.Reconciler,
) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries[gvk] = contract.RegistryEntry{
		CRD:               crd,
		Informer:          inf,
		ReconcilerFactory: rec,
	}
}

func (r *ResourceKatalog) Unregister(gvk string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.entries, gvk)
}

func (r *ResourceKatalog) Get(gvk string) (contract.RegistryEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.entries[gvk]
	return entry, ok
}

func (r *ResourceKatalog) ListGVKs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var gvkList []string
	for gvk := range r.entries {
		gvkList = append(gvkList, gvk)
	}
	return gvkList
}

func (r *ResourceKatalog) GetWorkers(gvk string, defaultWorkers int) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.entries[gvk]
	if !ok {
		return defaultWorkers
	}
	return entry.CRD.OperatorBox.Reconciler.Workers
}

func (r *ResourceKatalog) Entries() map[string]contract.RegistryEntry {
	return r.entries
}
