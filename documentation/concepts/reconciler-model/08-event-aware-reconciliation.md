# Event-Aware Reconciliation

Normally, a workqueue coalesces repeated events for the same resource. If the same CR changes several times before it is processed, those changes can collapse into one queued item.

For most reconciliation, that is exactly what you want.

Sometimes it is not.

An event-aware reconciler can preserve distinct events so that each admitted event becomes independently addressable work.

```text
normal reconciliation

event 1 ──┐
event 2 ──┼──► same key ──► one queued item
event 3 ──┘
```

With event awareness:

```text
event-aware reconciliation

event 1 ──► key + event identity ──► queued item 1
event 2 ──► key + event identity ──► queued item 2
event 3 ──► key + event identity ──► queued item 3
```

## Enabling event awareness

Event awareness is declared on the `reconcileGate`:

```yaml
operatorBox:
  preReconcile:
    reconcileGate:
      eventAware: true
```

The setting describes the reconciliation semantics. It does not expose queue implementation details to the configuration.

The runtime resolves the effective `operatorBox` for the resource, determines whether the effective reconcile gate is event-aware, and uses that information when constructing the queue item.

This is particularly important for per-target reconciliation: a target-specific operator box can change the effective reconciliation behaviour without requiring another runtime implementation.

## Event awareness starts before reconciliation

An important distinction is that `eventAware` is **not implemented by the reconcile gate itself**.

The reconcile gate only declares the behaviour.

The event travels through the runtime first:

```text
Kubernetes event
      │
      ▼
Informer / Watch Informer
      │
      ▼
event facts + sentinels
      │
      ▼
enqueue admission
      │
      ▼
Workqueue
      │
      │ event identity preserved
      ▼
preReconcile
      │
      ▼
reconcileGate
      │
      ▼
Reconciler
```

The gate does not need to know whether the queue represents event identity with an event ID, a boolean, or another future queue strategy.

That is a queue concern.

## Primary and secondary events

Event awareness applies to the **resource being reconciled**, not to the informer that happened to produce the event.

That means both primary and secondary events can participate in event-aware reconciliation.

```text
                  effective operatorBox
                           │
                    reconcileGate
                           │
                       eventAware
                           │
              ┌────────────┴────────────┐
              │                         │
       primary informer            watch informer
              │                         │
              └────────────┬────────────┘
                           │
                           ▼
                    common enqueue path
                           │
                           ▼
                      workqueue
```

A secondary watch first resolves the affected primary resource.

After that, it follows the same enqueue path as a primary event.

The watch informer does not need to implement event-aware queue behaviour itself.

Its responsibility is simply:

```text
watch event
    ↓
resolve affected primary key
    ↓
hand off to common enqueue path
```

The common enqueue layer applies the effective primary resource configuration.

## Why this matters

Without event awareness, two events can collapse because their normal queue identity is equivalent:

```text
(key, GVK)
```

With event awareness enabled, the runtime can preserve their event identity:

```text
(key, GVK, event identity)
```

This allows the runtime to retain distinct event context through queueing rather than treating every change to the same resource as interchangeable.

The distinction is especially useful when event-specific information matters to reconciliation, including event-derived sentinel values.

## Event-aware sentinels

Sentinels can carry facts derived from the transition between an old and new object.

For example:

```text
old object ─────┐
                ├──► sentinel computation ──► event context
new object ─────┘
```

When ordinary queue coalescing is used, later events for the same resource may replace or merge the pending event context according to queue semantics.

With event-aware queue identity, distinct events can retain their own sentinel payload:

```text
event 1
  ├── queue identity 1
  └── sentinels 1

event 2
  ├── queue identity 2
  └── sentinels 2
```

The reconciler therefore receives work corresponding to the individual admitted event rather than an arbitrarily coalesced representation of several events.

## Event awareness is not a second reconciliation model

Event-aware reconciliation does not create another reconciler implementation.

The same reconciliation models remain available:

```text
                         event-aware work
                               │
                               ▼
                         preReconcile
                               │
                               ▼
                         MuxReconciler
                               │
              ┌────────────────┼────────────────┐
              │                │                │
         declarative         hybrid (hooks)    Constructor — Reconcile()
```

`eventAware` changes **how work is represented and preserved before reconciliation**, not how the reconciler itself is implemented.

The reconciler still receives a normal reconciliation request and performs its work.

## The important boundary

The most important architectural property is that event awareness crosses several layers without coupling them together.

The informer knows that an event occurred.

The enqueue layer knows how the effective configuration should affect queue identity.

The queue knows how to preserve distinct work items.

The reconcile gate knows that the effective configuration is event-aware.

The reconciler only knows that it has work to perform.

```text
Informer
  │
  │ event
  ▼
Enqueue
  │
  │ effective event-aware configuration
  ▼
Queue
  │
  │ distinct work identity
  ▼
ReconcileGate
  │
  │ admission
  ▼
Reconciler
  │
  │ business logic
  ▼
result
```

No layer needs to understand the implementation details of the layers around it.

That is the purpose of `eventAware`: **preserve the identity of meaningful events through the runtime without making reconciliation itself aware of queue mechanics.**
