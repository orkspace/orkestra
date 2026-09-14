# Compute — how sentinels flow through the system

## The problem

`enqueueGate` runs inside the informer's `UpdateFunc`, where both `oldObj` and `newObj` are available. `reconcileGate` runs at dequeue time, where only the current object is available — `oldObj` is gone.

Sentinels solve this by computing the comparison at event time and carrying the result through the queue so both gates can use the same values.

---

## The flow

```
informer UpdateFunc
  │
  ├── sentinel.Compute(declared, oldObj, newObj)
  │     returns map[string]string{"generationChanged": "true", ...}
  │
  ├── QueueItem{Key: "ns/name", SentinelMap: result}
  │     enqueued with the sentinel values attached
  │
  └── enqueueGate evaluated here (oldObj still in scope)
        ↓
workqueue
  ↓
kordinator dequeue
  │
  └── reconcileGate evaluated here
        SentinelMap is read from QueueItem
        oldObj is not available — sentinels carry the event-time result
```

---

## `Compute`

```go
func Compute(declared []string, oldObj, newObj metav1.Object) map[string]string
```

Only the sentinels listed in `declared` are computed. If `declared` is empty, `Compute` returns `nil` immediately — no allocation on the common path.

The result maps each declared name to `"true"` or `"false"`. An unknown name maps to `""` (empty string — not `"false"`). Validators reject unknown names before runtime so this case does not occur in practice.

---

## Where `declared` comes from

The runtime collects sentinel names from two places in the Katalog:

- `operatorBox.preReconcile.sentinels` — primary CRD event sentinels
- `operatorBox.observe.watch[*].enqueueGate.sentinels` — per-watch-entry sentinels

Both are passed to `Compute` at event time for the relevant watch source.

---

## Import boundaries

This package imports only `reflect` and `k8s.io/apimachinery/pkg/apis/meta/v1`.

- `pkg/types` imports this package for the typed `Sentinel` constants (no cycle: `sentinel` does not import `pkg/types`).
- `pkg/runtime/informer` imports this package for `Compute` (no cycle: `sentinel` does not import `pkg/runtime`).

If you add a new sentinel that requires a type from outside stdlib or apimachinery, move the computation into the informer package and keep this package as the name registry only.

→ [03-adding-a-sentinel.md](03-adding-a-sentinel.md) — step-by-step guide to extending the sentinel set
