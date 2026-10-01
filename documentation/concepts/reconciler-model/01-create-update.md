# Create and Update

When you create or update a CR, Orkestra runs the full reconcile pipeline. Here is what happens, step by step.

---

## 1. Orkestra detects the change

Each CRD gets its own informer — a dedicated watch stream from the API server. When you create, update, or delete a CR, the watch event is placed into that CRD's workqueue immediately. CRDs do not share a queue or an informer.

## 2. A worker picks up the task

Each CRD runs its own worker pool. Workers pull items from that CRD's queue concurrently. The pool size is configurable per CRD via `workers:`. If one worker is busy, another picks up the next item.

## 3. Orkestra reads the CR from its cache

Orkestra reads from its local informer cache, not the API server. The cache is always up-to-date via the watch stream. This makes reconciliation fast and eliminates unnecessary API server load.

## 4. Deletion check

If the CR has a deletion timestamp, reconciliation switches to the delete path. See [Delete](02-delete.md).

## 5. Finalizer

Orkestra adds its finalizer on the first reconcile. This prevents the CR from being garbage-collected before Orkestra has run cleanup.

## 6. Tracking labels and annotations

Orkestra adds management metadata to the CR:

```yaml
labels:
  orkestra.orkspace.io/managed: "true"
annotations:
  orkestra.orkspace.io/managed-by: website-katalog
  orkestra.orkspace.io/managed-since: 2026-03-25T10:30:45Z
```

## 7. Normalize

Template expressions in the `normalize:` block run here, expanding defaults and coercing field formats before anything else sees the CR spec.

## 8. Mutation

Declared field defaults are applied — fields that should have a value if none was provided.

## 9. Validation

Declarative validation rules run. A failure stops the pipeline, records the error on the CR status, and does not requeue unless the CR changes.

## 10. Template reconciliation

`onCreate` templates run on the first reconcile for a CR. `onReconcile` templates run on every reconcile. Conditions (`when:`, `or:`) are evaluated, `forEach:` expansions are applied, and the resolved resources are created or updated.

## 11. Hooks (if declared)

If a Go hook is registered for this operatorBox, it runs after template reconciliation by default. The hook has full access to the (normalized, mutated, validated) CR and the Kubernetes client. Set `runHooksFirst: true` in the Katalog to reverse the order — the hook runs first, then templates.

## 12. Drift correction

Resources marked `reconcile: true` are checked on every reconcile, not just on creation. If someone manually changes a resource, Orkestra notices and restores the declared state.

## 13. Status patch

Declarative `status.fields` are resolved and patched onto the CR. Child resource state (Layer 3) is read and surfaced. The `Ready` condition is written as `True`.

## 14. Metrics and events

Reconcile duration, queue depth, and worker utilization are recorded. A `Reconciled` event is emitted on the CR.

## 15. Health update

The operatorBox's `CRDHealth` is updated: consecutive failure counter resets, success count increments, `lastReconcile` timestamp written.

---

## On failure

Any step failure stops the pipeline at that point. The `Ready` condition is written as `False` with the error message. The consecutive failure counter increments. After the configured `failureThreshold` is crossed, the operatorBox enters degraded state. The CR is requeued with backoff.
