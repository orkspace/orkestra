# Reconcile Pipeline

When a queue item is dequeued, the operatorBox reconcile pipeline runs in this order:

```text
informer cache → DeepCopy → normalize → mutation → validation
    → OnReconcile hook (Go) or runTemplateReconcile (declarative)
        → cross-CRD observation  (.cross.*)
        → external HTTP calls    (.external.*)
        → forEach expansion      (list field: .item=element | map field: .item=key, .value=value)
        → onCreate resource groups
        → onReconcile resource groups
    → patchStatusWithChildren
```

Each step receives the output of the previous step.

**Normalize** produces the canonical spec. Template expressions in the `normalize:` block run here, expanding defaults and coercing field formats before anything else sees the data.

**Mutation** applies declared defaults — fields that should have a value if none was provided.

**Validation** enforces constraints. A validation failure stops the pipeline and records the error on the CR status. It does not requeue unless the CR changes.

**Template reconcile** is the declarative path. `onCreate`, `onReconcile`, and `onDelete` hook blocks execute here. Each hook evaluates its `when:` conditions against the normalized, mutated, validated spec and dispatches the declared resource creates, updates, or deletes.

**patchStatusWithChildren** is always the last step. It writes the operatorBox health state, child resource status, and any declared `status:` field mappings back to the CR.

---

## Error behavior

A pipeline step failure records the error, increments the consecutive-failure counter in `CRDHealth`, and requeues with backoff. After the configured `consecutiveFailures` threshold is crossed, the operatorBox enters degraded state. Degraded operatorBoxes are visible in the Control Center.

A panic anywhere in the pipeline is caught by `safeReconcile`, recorded as a failure, and does not affect any other operatorBox.
