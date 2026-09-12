# observe

The `observe` package manages secondary Kubernetes resource observation for Orkestra operators.

It watches resources declared by an operator and turns relevant changes into enqueue requests for the primary CR's workqueue. The package does **not** reconcile resources or make reconciliation decisions.

## Responsibility

The observation flow is:

```text
Kubernetes resource event
        │
        ▼
   Observer
        │
        ├── WatchEntry
        │      │
        │      └── resolve primary CR key(s)
        │
        └── EventEntry
               │
               └── match Event → resolve primary CR key(s)
        │
        ▼
AllowAndEnqueueKey
        │
        ▼
primary CR workqueue
        │
        ▼
normal reconciliation
```

Observation therefore ends at the existing informer enqueue boundary. Once a primary CR key has been resolved and admitted to the queue, the normal runtime reconciliation path takes over.

The observer never calls a reconciler directly.

## What is observed

The package currently supports two declaration types.

### Secondary resource watches

`operatorBox.observe.watch` declares Kubernetes resources whose changes should cause reconciliation.

```yaml
operatorBox:
  watch:
    - apiVersion: apps/v1
      kind: Deployment
      on:
        - update
      keyFrom:
        label: orkestra.example.com/owner
```

Watch entries support the existing secondary-resource routing semantics:

* `keyFrom.label`
* `keyFrom.name`
* owner-reference resolution
* broadcast when allowed
* namespace and name constraints
* enqueue gates
* watch event filtering

Resources declared by constructors and hooks may also become implicit watches. These use owner-reference routing and do not broadcast.

### Kubernetes Events

`operatorBox.events` declares Kubernetes Events that should wake the operator.

```yaml
operatorBox:
  events:
    - reason: DatabaseReady
      type: Normal
      regarding:
        apiVersion: databases.example.com/v1
        kind: Database
      keyFrom:
        name: my-database
```

Events are treated as **triggers and context, not as the source of truth**.

Event-specific fields such as `reason`, `action`, `type`, `reportingController`, `reportingInstance`, `regarding`, and `related` determine whether an Event matches an `EventEntry`.

Once matched, `KeyFrom` determines which primary CR key(s) are enqueued.

The Event's `regarding` object is **not converted into an owner reference**. `regarding` and `related` are Event matching constraints; primary-key routing remains explicitly controlled by the Event declaration.

## Primary CR key resolution

The observer separates **key resolution** from **enqueue admission**.

For a secondary watch, the resolver determines the affected primary CR key(s) using the declared routing configuration:

```text
keyFrom.label
      ↓
keyFrom.name
      ↓
ownerReference
      ↓
broadcast
```

The exact routing options available depend on the declaration and whether broadcast is allowed.

The resolver only answers:

> Which primary CR key(s) are affected?

It does not evaluate enqueue gates or manipulate the workqueue directly.

## Enqueue boundary

After primary keys have been resolved, observation delegates to the shared informer enqueue machinery:

```go
Informer.AllowAndEnqueueKey(...)
```

This preserves the existing runtime semantics for:

* namespace admission
* enqueue gates
* sentinel evaluation
* event-aware queues
* queue coalescing
* queue identity
* secondary-resource context

This is intentional: watch and Event observation are different **sources of triggers**, but they share the same queue and reconciliation path.

## Package structure

```text
pkg/runtime/informer/observe/
├── observer.go
├── watch.go
├── event.go
└── resolve.go
```

### `observer.go`

Defines the `Observer` and its runtime dependencies.

`Observer.Observe()` starts the secondary observers declared for a CRD.

### `watch.go`

Creates and manages dynamic informers for `operatorBox.observe.watch` entries and implicit watches derived from managed resources.

It handles Kubernetes watch events and passes affected primary keys to the shared informer enqueue machinery.

### `event.go`

Creates the Kubernetes Event observer and evaluates `EventEntry` declarations.

Matching Events are routed to primary CR keys using the same key-resolution concepts used by secondary watches.

### `resolve.go`

Contains primary-key resolution shared by secondary observation.

It deliberately does not enqueue or reconcile.

## Design boundary

The package follows this rule:

> **Observe → resolve → enqueue. Never observe → reconcile.**

The observer is therefore independent of reconciliation policy and implementation.

A secondary resource changing does not directly cause a reconciler invocation. Instead:

1. Kubernetes produces an observation.
2. `observe` determines whether the observation is relevant.
3. `observe` resolves the affected primary CR key(s).
4. The existing informer layer admits and enqueues those keys.
5. The normal runtime worker reconciles the primary CR.

This keeps secondary observation consistent with the primary informer-driven reconciliation model.
