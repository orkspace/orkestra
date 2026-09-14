// pkg/informer/namespace_filter.go
//
// NamespaceFilter — pre-enqueue namespace restriction for the informer factory.
//
// Problem: without this, namespace guards are enforced at the reconciler level.
// Every event from every namespace hits the queue. Workers dequeue and no-op.
// Under high event volume in restricted namespaces, the queue fills with
// items that do no real work, creating false queue pressure.
//
// Solution: two-tier filtering.
//
//	Tier 1 — Namespace-scoped ListerWatcher.
//	  When allowedNamespaces has exactly one entry, the ListerWatcher itself
//	  is scoped to that namespace. The informer never receives events from
//	  other namespaces. Cache is clean. Zero overhead.
//
//	Tier 2 — Pre-enqueue NamespaceFilter in handleEvent.
//	  For multiple allowed namespaces or any restricted namespaces, a
//	  NamespaceFilter is stored per GVK on the factory. handleEvent checks
//	  it before building the queue key. Items that fail the filter are
//	  dropped — they never enter the queue.
//
//	Tier 3 — Reconciler check.
//	  Remains as a safety net for race conditions where the filter map
//	  was not yet populated. Defense in depth.
//
// Priority: allowedNamespaces takes precedence over restrictedNamespaces
// when both are declared (same semantics as the existing reconciler guard).
package informer

import (
	"strings"

	"github.com/orkspace/orkestra/domain"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

// NamespaceFilter holds the namespace restriction configuration for one GVK.
// Stored on the Factory keyed by GVK string. Checked in handleEvent before
// the item enters the queue.
type NamespaceFilter struct {
	// AllowedNamespaces — when non-empty, only events from these namespaces
	// are enqueued. Everything else is dropped before reaching the queue.
	// Takes precedence over RestrictedNamespaces when both are set.
	AllowedNamespaces []string

	// RestrictedNamespaces — when non-empty, events from these namespaces
	// are dropped before reaching the queue. All other namespaces pass.
	RestrictedNamespaces []string
}

// Allows returns true when the namespace passes this filter.
//
// Logic (mirrors the existing CheckNamespace in pkg/runtime/reconciler):
//  1. If AllowedNamespaces is declared — namespace must be in the list.
//  2. Else if RestrictedNamespaces is declared — namespace must NOT be in the list.
//  3. Otherwise — allow all.
func (f *NamespaceFilter) Allows(namespace string) bool {
	if len(f.AllowedNamespaces) > 0 {
		for _, ns := range f.AllowedNamespaces {
			if ns == namespace {
				return true
			}
		}
		return false // not in the allowlist
	}

	if len(f.RestrictedNamespaces) > 0 {
		for _, ns := range f.RestrictedNamespaces {
			if ns == namespace {
				return false // explicitly restricted
			}
		}
	}

	return true // no restriction
}

// IsActive returns true when this filter has any namespace restrictions.
// Filters with no restrictions are no-ops and can be skipped.
func (f *NamespaceFilter) IsActive() bool {
	return f != nil && (len(f.AllowedNamespaces) > 0 || len(f.RestrictedNamespaces) > 0)
}

// IsSingleNamespace returns true when the filter allows exactly one namespace.
// Used to decide whether to use a namespace-scoped ListerWatcher (Tier 1).
func (f *NamespaceFilter) IsSingleNamespace() bool {
	return f != nil && len(f.AllowedNamespaces) == 1 && len(f.RestrictedNamespaces) == 0
}

// SingleNamespace returns the single allowed namespace when IsSingleNamespace is true.
func (f *NamespaceFilter) SingleNamespace() string {
	if !f.IsSingleNamespace() {
		return ""
	}
	return f.AllowedNamespaces[0]
}

// RegisterNamespaceFilter stores a namespace filter for a GVK on the factory.
// Called during CRD entry registration, before informers are started.
// Thread-safe — uses the factory's existing mutex.
func (f *Factory) RegisterNamespaceFilter(gvkStr string, filter *NamespaceFilter) {
	if filter == nil || !filter.IsActive() {
		return // nothing to store
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.namespaceFilters[gvkStr] = filter
}

// namespaceAllowed checks the namespace filter for the given GVK before enqueue.
// Returns true when the event should be enqueued (no filter, or filter passes).
// Returns false when the event should be dropped before touching the queue.
//
// Called inside handleEvent — must be fast. Map lookup is O(1). Slice scan
// over namespace lists is O(n) where n is typically 1–5. Acceptable.
func (f *Factory) namespaceAllowed(gvkStr, namespace string) bool {
	// f.mu is NOT held here — handleEvent is called from informer goroutines
	// and holding a write lock would block informer event processing.
	// We use a read lock for the map lookup only.
	f.mu.RLock()
	filter, ok := f.namespaceFilters[gvkStr]
	f.mu.RUnlock()

	if !ok || !filter.IsActive() {
		return true
	}

	return filter.Allows(namespace)
}

// extractNamespace extracts the namespace from an object passed to handleEvent.
// Handles both regular objects and DeletedFinalStateUnknown (tombstone) wrappers.
// Returns "" for cluster-scoped resources — Allows("") returns true so they pass.
func extractNamespace(obj interface{}) string {
	// Handle tombstone (deleted objects)
	obj = domain.UnwrapCacheTombstone(obj)

	if rObj, ok := obj.(runtime.Object); ok {
		if accessor, err := meta.Accessor(rObj); err == nil {
			return accessor.GetNamespace()
		}
	}

	return ""
}

// NamespaceFilterSummary returns a human-readable description of the filter
// for logging. Called once at informer registration, not on the hot path.
func NamespaceFilterSummary(f *NamespaceFilter) string {
	if f == nil || !f.IsActive() {
		return "all namespaces"
	}
	if len(f.AllowedNamespaces) > 0 {
		return "allowed: [" + strings.Join(f.AllowedNamespaces, ", ") + "]"
	}
	return "restricted: [" + strings.Join(f.RestrictedNamespaces, ", ") + "]"
}
