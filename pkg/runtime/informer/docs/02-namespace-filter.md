# 02 — Namespace Filter

## The problem

Without namespace filtering, the reconciler is the only enforcement point. Every watch event from every namespace enters the queue. Workers dequeue items and immediately return nil because the namespace guard (`CheckNamespace`) rejects the CR. Under high event volume from restricted namespaces, the queue fills with no-op items — creating false queue pressure and burning CPU on serialization and context switches.

## Three-tier solution

```
API server watch stream
        │
        │  ← Tier 1: namespace-scoped ListerWatcher
        │        When allowedNamespaces has exactly ONE entry,
        │        the ListerWatcher is scoped to that namespace.
        │        The informer never receives events from other namespaces.
        │        Cache is clean. Zero overhead on the hot path.
        │
        ▼
informer cache (in-memory)
        │
        │  ← Tier 2: pre-enqueue drop in handleEvent
        │        For multiple allowed namespaces or any restricted namespaces,
        │        a NamespaceFilter is stored per GVK on the factory.
        │        handleEvent checks it before building the queue key.
        │        Items that fail are dropped — they never touch the queue.
        │
        ▼
per-CRD work queue
        │
        │  ← Tier 3: reconciler CheckNamespace
        │        Safety net for race conditions during startup,
        │        before the filter map is fully populated.
        │        Defense in depth — not the primary gate.
        │
        ▼
reconciler.Reconcile()
```

## NamespaceFilter

```go
type NamespaceFilter struct {
    AllowedNamespaces    []string
    RestrictedNamespaces []string
}
```

`Allows(namespace string) bool` implements this precedence:

1. If `AllowedNamespaces` is non-empty → namespace must be in the list.
2. Else if `RestrictedNamespaces` is non-empty → namespace must NOT be in the list.
3. Otherwise → allow all (filter is a no-op).

This mirrors the semantics of the existing reconciler `CheckNamespace` in `pkg/runtime/reconciler/run_namespace_guard.go`.

```
Allows("")           → true   (cluster-scoped resources pass; empty namespace = no restriction)
Allows("default")   + AllowedNamespaces=["default"]           → true
Allows("kube-system")+ AllowedNamespaces=["default"]          → false  ← dropped
Allows("kube-system")+ RestrictedNamespaces=["kube-system"]   → false  ← dropped
Allows("default")   + RestrictedNamespaces=["kube-system"]    → true
```

## Tier 1 — namespace-scoped ListerWatcher

Triggered when `IsSingleNamespace() == true`:

```go
func (f *NamespaceFilter) IsSingleNamespace() bool {
    return len(f.AllowedNamespaces) == 1 && len(f.RestrictedNamespaces) == 0
}
```

In `konstructRuntime`, when this is true, `opts.Namespace` is set to `filter.SingleNamespace()`. The ListerWatcher is then built with that namespace:

- **Dynamic CRDs** (`DynamicListerWatcher`): `CRDInfo.Namespace` is set to the single allowed namespace. The dynamic client scopes `Resource(gvr).Namespace(ns).List/Watch` to that namespace.
- **Typed CRDs** (`newListWatch`): `opts.Namespace` is forwarded into the closure. When non-empty, `client.ListInNamespace(ctx, opts.Namespace, options)` and `client.WatchInNamespace(...)` are called instead of the cluster-scoped variants.

The informer's in-memory cache will only ever contain CRs from that one namespace.

## Tier 2 — pre-enqueue drop

`RegisterNamespaceFilter` stores the filter on the factory before informers are started:

```go
func (f *Factory) RegisterNamespaceFilter(gvkStr string, filter *NamespaceFilter) {
    if filter == nil || !filter.IsActive() {
        return
    }
    f.mu.Lock()
    defer f.mu.Unlock()
    f.namespaceFilters[gvkStr] = filter
}
```

`handleEvent` then calls `namespaceAllowed` before touching the queue:

```go
namespace := extractNamespace(obj)
if !f.namespaceAllowed(gvkStr, namespace) {
    logger.Debug()...  // debug only — fires on every event under load
    return             // queue untouched
}
```

`namespaceAllowed` takes only a read lock for the map lookup, then releases it before the slice scan. Write locks are held only during registration (before `Start()`), so the hot path is never write-lock-contended.

## extractNamespace

```go
func extractNamespace(obj interface{}) string {
	rObj, ok := domain.ToObject(obj)

	if ok {
		if accessor, err := meta.Accessor(rObj); err == nil {
			return accessor.GetNamespace()
		}
    }
    return ""
}
```

`cache.DeletedFinalStateUnknown` is produced when the watch stream misses a deletion event — the informer wraps the last-known state. Unwrapping it first ensures tombstones are filtered correctly.

Cluster-scoped resources return `""`. `Allows("")` always returns `true` (no namespace restriction applies to cluster-scoped objects), so they are never dropped.

## Registration — wiring in konstructRuntime

```go
// After opts is built, before the informer is created:
if len(crd.AllowedNamespaces) > 0 || len(crd.RestrictedNamespaces) > 0 {
    filter := &informer.NamespaceFilter{
        AllowedNamespaces:    []string(crd.AllowedNamespaces),
        RestrictedNamespaces: []string(crd.RestrictedNamespaces),
    }
    if filter.IsSingleNamespace() {
        opts.Namespace = filter.SingleNamespace()   // Tier 1
        logger.Info()...
    }
    infFactory.RegisterNamespaceFilter(gvk, filter) // Tier 2
    logger.Info().Str("filter", informer.NamespaceFilterSummary(filter))...
}
```

`NamespaceFilterSummary` returns `"allowed: [ns1, ns2]"` or `"restricted: [ns3]"` or `"all namespaces"` for logging.

## Effect table

| Scenario | Queue items from restricted ns | Cache entries from restricted ns |
|---|---|---|
| No filter | many (all reconcile as no-ops) | many |
| Tier 2 only (multiple allowed or any restricted) | **zero** | still present |
| Tier 1 (single allowed) | **zero** | **zero** |

Tier 3 (reconciler `CheckNamespace`) remains unchanged in all scenarios.

---

**Next →** [03 — ListerWatch Construction](03-listerwatch.md)
