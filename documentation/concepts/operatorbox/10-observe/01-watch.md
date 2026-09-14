# Arbitrary Watch

`operatorBox.observe.watch` declares secondary Kubernetes resources whose changes should re-enqueue the primary CR for reconciliation.

No Go code is required. The feature is purely declarative.

For an introduction to observation and how it relates to Events and enrichment, see [Observe](00-observe.md).

---

## Why it exists

Many real operators react to resources they do not own:

* An app operator that reads a shared `ConfigMap` of feature flags — if the ConfigMap changes, every CR must reconcile.
* A database operator that watches `Nodes` — node capacity changes affect pod scheduling, so each database CR must re-evaluate.
* A workload operator that watches a `Secret` managed by cert-manager — when the Secret rotates, the operator must restart the relevant workload.

Without arbitrary watch, authors work around this by polling in a hook, or by adding a finalizer or ownerReference to an object they do not logically own. Both are fragile.

With `operatorBox.observe.watch`, Orkestra manages the secondary informer and routes events back to the right primary CR.

---

## How it works

For each entry in `operatorBox.observe.watch`, Orkestra starts a dynamic informer for that resource type.

Events that arrive during the initial cache sync (the List phase) are dropped. Only events that arrive after sync trigger re-enqueues.

```yaml
operatorBox:
  observe:
    watch:
      - apiVersion: v1
        kind: ConfigMap
        name: feature-flags
        namespace: config
        on: [update]
```

When `config/feature-flags` is updated, the informer produces an update observation. Orkestra resolves which primary CR(s) should be enqueued and adds them to the workqueue.

---

## Key resolution

Orkestra resolves the primary CR key from the watched object using a four-step chain (first match wins).

### 1. `keyFrom.label`

The watched object carries a label whose value is the primary CR key.

```yaml
operatorBox:
  observe:
    watch:
      - apiVersion: v1
        kind: Secret
        namespace: certs
        keyFrom:
          label: app.kubernetes.io/cr-owner
```

If the Secret has:

```text
app.kubernetes.io/cr-owner: default/myapp
```

then `default/myapp` is enqueued.

### 2. `keyFrom.name`

A fixed primary CR name.

```yaml
operatorBox:
  observe:
    watch:
      - apiVersion: v1
        kind: ConfigMap
        name: global-config
        namespace: config
        keyFrom:
          name: my-operator
          namespace: default
```

Every matching observation enqueues `default/my-operator`.

### 3. `ownerReference`

If no `keyFrom` is set, Orkestra checks whether the watched object has an ownerReference whose API version and kind match the primary CRD.

The named owner is enqueued.

### 4. Broadcast

If no routing rule matches, Orkestra enqueues all currently known primary CRs of this type.

This is useful for genuinely shared resources such as cluster-wide configuration or Nodes.

---

## Event filtering with `on:`

By default, all three observation types trigger re-enqueues:

```yaml
on: [create, update, delete]
```

Restrict the observation lifecycle with `on:`:

```yaml
operatorBox:
  observe:
    watch:
      - apiVersion: apps/v1
        kind: Deployment
        on: [update]
```

Only update observations enqueue the primary CR.

---

## Interaction with `preReconcile.enqueueGate`

A watch-triggered enqueue goes through the same `preReconcile.enqueueGate` as any other primary CR update.

The gate evaluates the primary CR. The watched object is the observation source, not the reconciliation object.

Primary-CR sentinels such as `generationChanged` and `labelsChanged` therefore describe the primary CR's own state transition, not the watched object's delta.

If you need to gate on the watched object's state, use a `when:` condition inside the reconcile template that reads the appropriate cross-CRD or external field.

---

## Schema reference

→ [operatorBox.observe.watch schema](../../../reference/schema/02-katalog/27-watch.md)
