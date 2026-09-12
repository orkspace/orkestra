# Observe

`observe` tells Orkestra which external Kubernetes changes should cause a primary CR to be re-enqueued for reconciliation.

Normally, an operator reconciles when its own CR changes. `observe` extends that behavior to secondary signals that can affect the desired state.

```yaml
operatorBox:
  observe:
    watch:
      ...
    events:
      ...
```

Orkestra currently supports two observation mechanisms:

| Observation | Watches                        | Purpose                                           |
| ----------- | ------------------------------ | ------------------------------------------------- |
| `watch`     | Arbitrary Kubernetes resources | Reconcile when a secondary resource changes       |
| `events`    | Kubernetes `Event` objects     | Reconcile when a matching Kubernetes Event occurs |

No Go code is required. Both are declarative.

---

## Where observation sits

Observation happens outside the reconciliation loop. Informers observe Kubernetes resources and route relevant occurrences to the primary CR workqueue.

```text
Kubernetes
    │
    ├── primary CR informer
    │       │
    │       └── primary CR queue
    │
    └── observe
        ├── watch informers
        └── Event informer
                │
                ▼
        resolve primary CR key(s)
                │
                ▼
        enqueue primary CR
                │
                ▼
        preReconcile.enqueueGate/reconcileGate
                │
                ▼
        reconcile
```

Observation does not call the reconciler directly. Once an observation resolves a primary CR key, it enters the same queue and reconciliation path used by primary CR changes.

This means observation and primary CR changes converge at the same reconciliation boundary.

---

## The two kinds of observation

### `observe.watch`

`watch` observes arbitrary Kubernetes resources.

Use it when a secondary resource's **state** can affect the desired state of the primary CR.

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

When `config/feature-flags` changes, Orkestra resolves the affected primary CR and re-enqueues it.

Watch supports:

* `create`
* `update`
* `delete`
* `keyFrom.label`
* `keyFrom.name`
* owner-reference routing
* broadcast routing
* `enqueueGate`
* cache `index` declarations

See [Arbitrary Watch](01-watch.md).

### `observe.events`

`events` observes Kubernetes `Event` objects.

Use it when an Event emitted by Kubernetes or another controller represents a signal that should cause reconciliation.

```yaml
operatorBox:
  observe:
    events:
      dbReady:
        reason: DatabaseReady
        type: Normal
        regarding:
          kind: Database
          name: my-db
```

Event declarations match properties of the Kubernetes Event, such as:

* `reason`
* `action`
* `type`
* `reportingController`
* `reportingInstance`
* `regarding`
* `related`
* namespace
* routing through `keyFrom`

A matching Event is then routed to the primary CR queue using the same observation and enqueue machinery as a watch.

See [Kubernetes Events](02-events.md).

---

## Observation versus enrichment

`observe` and `enrich` solve different problems.

**Observation** asks:

> "What should cause this CR to reconcile?"

**Enrichment** asks:

> "What additional Kubernetes state should be fetched while reconciling?"

For example:

```yaml
operatorBox:
  observe:
    events:
      dbFailed:
        reason: DatabaseFailed

  enrich:
    - events
```

The first declaration causes reconciliation when a matching Event occurs.

The second fetches Event objects during reconciliation so templates can inspect them.

Observation is therefore **triggering**; enrichment is **data acquisition**.

---

## Observation and gates

An observed change does not bypass the normal reconciliation pipeline.

```text
secondary occurrence
        │
        ▼
resolve primary CR
        │
        ▼
primary CR queue
        │
        ▼
enqueueGate
        │
        ▼
reconcileGate
        │
        ▼
reconciler
```

The primary CR remains the unit of reconciliation.

For a watch, the watched object's change is not treated as a change to the primary CR. Primary-CR sentinels therefore continue to describe the primary CR's own state transition.

For Events, the matching Event can also be made available as observation context to the reconciliation resolver.

---

## Why `observe` exists

Without declarative observation, operators commonly resort to polling, unnecessary ownership relationships, or custom controller code to react to secondary changes.

`observe` makes those relationships explicit in the katalog:

```text
resource changes
       │
       ▼
 declarative observation
       │
       ▼
 primary CR re-enqueued
       │
       ▼
 normal reconciliation
```

The operator author declares **what should be observed and how it maps back to the primary CR**. Orkestra manages the informer and queue plumbing.

---

## Where to go next

* [Arbitrary Watch](01-watch.md) — observe arbitrary Kubernetes resources
* [Kubernetes Events](02-events.md) — observe Kubernetes Events
* [Enrich](../05-enrich/index.md) — fetch secondary state during reconciliation
