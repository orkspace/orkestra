# Conditional Reconciliation

Conditional reconciliation lets you gate an entire reconcile cycle on conditions evaluated **before** the reconciler is called. When a CR does not satisfy the conditions, the reconciler is skipped entirely — no resources are created or deleted, and the CRD's health state stays idle (not degraded).

This is distinct from resource-level conditions (`when:` on individual resources inside `onCreate`/`onReconcile`). Those conditions are evaluated inside the reconciler. `operatorBox.preReconcile` is evaluated earlier — either at the informer level (`enqueueGate:`) before the item enters the queue, or at the kordinator level (`reconcileGate:`) before the reconciler is called.

---

## Declaring a pre-reconcile gate

```yaml
spec:
  crds:
    app:
      apiTypes:
        group: apps.demo.io
        kind: App
        version: v1alpha1
      operatorBox:
        preReconcile:
          reconcileGate:
            when:
              - field: "{{ .spec.enabled }}"
                equals: "true"
        onReconcile:
          deployments:
            - name: "{{ .metadata.name }}"
              image: "{{ .spec.image }}"
              reconcile: true
```

With this configuration:
- A CR with `spec.enabled: true` → reconciler runs, Deployment is created/updated.
- A CR with `spec.enabled: false` → reconciler is skipped, Deployment is not created. No error recorded. CRD health state is `gated`.

---

## Gate semantics

| Property | Behavior |
|---|---|
| **Scope** | Per-CR object — each CR's conditions are evaluated independently |
| **Phase** | Before the reconciler is called — after dequeue, before `safeReconcile` |
| **On gate** | Item is dropped from the queue without re-queuing. No error. No status write. |
| **Health state** | `gated` — healthy (not degraded), idle, with the gate reason reported |
| **On next change** | If the CR is updated (e.g. `spec.enabled` flipped to `true`), the object is re-enqueued and the gate is re-evaluated |

---

## Condition types

All condition operators available in resource-level `when:` blocks are available here — `equals`, `contains`, `exists`, `gt`/`lt`, `in`, `regex`, and more. See the [full operator reference](../../reference/schema/02-katalog/06-when-conditions.md#operators).

`or:` (OR semantics) is also supported alongside `when:` (AND semantics):

```yaml
preReconcile:
  reconcileGate:
    when:
      - field: "{{ .spec.enabled }}"
        equals: "true"
    or:
      - field: "{{ .spec.environment }}"
        equals: "production"
      - field: "{{ .spec.environment }}"
        equals: "staging"
```

Both blocks must pass for reconciliation to proceed.

---

## Gated state in Control Center

When a CRD is gated, the Control Center displays a **purple** badge and the gate reason:

```text
● gated   spec.enabled is false
```

The state is separate from healthy and degraded — it is idle, not an error. It clears automatically the next time a reconcile succeeds (e.g. the CR is updated with a passing value).

---

## Enqueue-level gate (`preReconcile.enqueueGate`)

`preReconcile.enqueueGate` fires even earlier — inside the informer's `handleEvent`, before the item enters the work queue. The object is silently dropped without ever reaching the kordinator.

```yaml
operatorBox:
  preReconcile:
    enqueueGate:
      when:
        - field: "{{ .spec.active }}"
          equals: "true"
```

Use `enqueueGate` when you want zero queue pressure for objects that should be completely ignored. Use `reconcileGate` when you want the kordinator to track the gated state and surface it as health (`gated`).

| | `preReconcile.enqueueGate` | `preReconcile.reconcileGate` |
|---|---|---|
| **Evaluated by** | Informer (`handleEvent`) | Kordinator (after dequeue) |
| **Phase** | Before queue entry | After dequeue |
| **Health on gate** | No effect | `gated` (idle) |
| **Caveat** | Object stays out until next watch event | Clears on next successful reconcile |

---

## `failPolicy` — gate behaviour on evaluation failure

When a gate includes `external:` calls, the evaluation can fail — the endpoint is down, the call times out. `failPolicy:` controls what the gate does in that case.

```yaml
preReconcile:
  reconcileGate:
    failPolicy: closed        # evaluation failure → hold back, do not reconcile
    external:
      - name: depHealth
        url: "{{ .spec.dependencyUrl }}/health"
    when:
      - field: external.depHealth.status
        equals: "200"
```

| Value | Behaviour |
|-------|-----------|
| `open` (default) | Evaluation failure passes the gate — CR is enqueued or reconciled as normal. Safe default for `enqueueGate`. |
| `closed` | Evaluation failure holds the gate — CR is dropped or held back. Use on `reconcileGate` when reconciling against an unknown state is worse than skipping a cycle. |

`ork validate` warns when `external:` calls are declared without an explicit `failPolicy:` — the default `open` may not be the intent for `reconcileGate`.

---

## Sentinels — gate on what changed

`preReconcile.enqueueGate` and `reconcileGate` evaluate the *current* state of the CR — they answer "does this object satisfy a condition right now?" Sentinels answer a different question: "did a specific thing change between the last version and this version?"

```yaml
operatorBox:
  preReconcile:
    sentinels:
      - generationChanged
      - labelsChanged
    enqueueGate:
      when:
        - field: "{{ generationChanged }}"
          equals: "true"
```

Sentinels are declared in `preReconcile.sentinels` and computed at the informer level — in the `UpdateFunc`, before the gate is evaluated. Each sentinel compares the old and new object and produces `"true"` or `"false"`. Declared sentinels become template functions available in `enqueueGate` and `reconcileGate` conditions.

### Core sentinels

| Sentinel | Fires when |
|---|---|
| `generationChanged` | `.metadata.generation` incremented (spec change on most CRDs) |
| `labelsChanged` | label set differs between old and new object |
| `annotationsChanged` | annotation set differs |
| `deletionStarted` | `deletionTimestamp` was nil, is now set |
| `finalizersChanged` | finalizer list differs |

### Full `metav1.Object` coverage

Every field Kubernetes exposes on a resource's metadata is observable as a sentinel. The additional sentinels below cover the remaining fields of the `metav1.Object` interface — most are rarely useful in normal gate logic but are available for defensive checks and anomaly detection.

| Sentinel | Fires when |
|---|---|
| `nameChanged` | object name differs (re-create seen as update — nearly always `false`) |
| `namespaceChanged` | namespace differs (Kubernetes does not move resources — nearly always `false`) |
| `generateNameChanged` | `generateName` prefix differs (set once at creation; does not change on updates) |
| `uidChanged` | UID differs — object was deleted and recreated with the same name |
| `resourceVersionChanged` | any write to the object — catches every update |
| `creationTimestampChanged` | creation timestamp differs (immutable; fires only on anomalous events) |
| `deletionGracePeriodSecondsChanged` | graceful deletion period changed |
| `ownerReferenceChanged` | owner reference list differs — adoption or orphaning event |
| `managedFieldsChanged` | managed fields differ — a field manager applied a change |

A sentinel that is not declared is not available in gate templates — `ork validate` rejects templates that reference undeclared sentinel names.


!!! tip "A sentinel is not a Note"
    Notes carry arbitrary values into the reconcile context and can be read anywhere in templates. A sentinel is narrower: it answers a yes/no question about what changed between two versions of the object, and it is only available in gate conditions — not in `onCreate`/`onReconcile` templates or `status:` field mappings. Use a Note when you need a value inside reconciliation; use a sentinel when you need to decide whether reconciliation should run at all.

### Why declare rather than use `.metadata.generation` directly

You could write `field: "{{ .metadata.generation }}"` and compare it to a static value, but generation is a monotonically increasing counter — there is no "previous value" available inside a stateless template. Sentinels are computed at event time, when both the old and new versions of the object are available side by side. That comparison window is gone by the time the object reaches a gate or reconciler, which is why sentinels must be declared and computed upfront.

### Sentinel scope

Sentinels are computed at the same level as `enqueueGate` — at event time, when both the old and new versions of the object are visible. The values travel with the queued item, so both `enqueueGate` and `reconcileGate` see them. A sentinel referenced in a `reconcileGate` reflects what changed when the object was last updated, not a recomputed comparison at reconcile time.

---

## Difference from resource-level conditions

| | `preReconcile.enqueueGate` or `reconcileGate` | `onCreate` / `onReconcile` resource `when:` |
|---|---|---|
| **Evaluated by** | Informer or kordinator | Reconciler (inside reconcile loop) |
| **Scope** | Entire reconcile cycle | Individual resource |
| **Effect** | Object never queued or reconciler never called | Resource is skipped; other resources still created |
| **Health on gate** | `enqueueGate`: no effect; `reconcileGate`: `gated` | No effect on health |
| **Error on gate** | None | None |
| **Re-queue** | No — waits for next CR change event | Normal re-queue |

Use pre-reconcile gates when the entire operator should stay dormant until a condition is met. Use resource-level conditions when most resources should be created but some are optional.

---

## Testing gates

### Simulate (with `--envtest`)

Use `absent:` to assert a resource was never created when the gate fires:

```yaml
# simulate-gated.yaml
crFiles:
  - cr-app-disabled.yaml   # spec.enabled: false
expect:
  crds:
    app:
      absent:
        - resource: deployments
```

Run with:

```bash
ork simulate -f simulate.yaml --envtest
```

### E2E

Assert the `/katalog` runtime endpoint reports `gated: true` after patching the CR:

```yaml
steps:
  - kubectl:
      patch:
        resource: apps
        name: my-app
        patch: '{"spec":{"enabled":false}}'
  - kubectl:
      port-forward:
        pod-selector: "app=orkestra-leader"
        port: 8080
      assert:
        path: /katalog
        jq: '.crds.app.gated'
        equals: "true"
```

---

## Try it

```bash
ork init --pack intermediate
cd 05-when-conditions/conditional-reconciliation
```

The pack includes an App CRD (gated by `spec.enabled`), a Route CRD (unconditional), a Simulate suite that tests both pass and gate scenarios, and a minimal E2E.
