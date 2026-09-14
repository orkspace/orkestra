# Kubernetes Events

`operatorBox.observe.events` declares Kubernetes Events that should cause a primary CR to be re-enqueued for reconciliation.

No Go code is required. Event observation is purely declarative.

Unlike `observe.watch`, which watches the state of arbitrary Kubernetes resources, `observe.events` watches Kubernetes `Event` objects and matches their contents.

---

## Why it exists

Kubernetes controllers and other components emit Events to describe important occurrences:

* a resource failed to schedule
* a deployment became unavailable
* a certificate was renewed
* a dependency became ready
* an external controller reported an error

An operator may need to react to those occurrences even when the Event is not an owned resource.

For example, an application operator can react when a dependency reports that it is ready:

```yaml
operatorBox:
  observe:
    events:
      dependencyReady:
        reason: DependencyReady
        type: Normal
        regarding:
          apiVersion: example.com/v1
          kind: Dependency
          name: database
```

When a matching Event is observed, Orkestra resolves the primary CR key and re-enqueues it.

---

## How it works

Orkestra observes Kubernetes `Event` objects and evaluates each declared Event entry against the observed Event.

```text
Kubernetes Event
      │
      ▼
Event informer
      │
      ▼
matching EventEntry
      │
      ▼
resolve primary CR key(s)
      │
      ▼
primary CR queue
      │
      ▼
normal reconciliation
```

The Event remains an Event throughout this process.

Its `regarding` and `related` objects are **matching constraints**, not ownerReferences and not automatic routing instructions.

Routing is determined separately using the observation's key-resolution rules.

---

## Event declarations

Events are declared as a map. The map key identifies the Event declaration and is also the name used when the matching Event is exposed as reconciliation context.

```yaml
operatorBox:
  observe:
    events:
      dbready:
        reason: DatabaseReady
        type: Normal
        regarding:
          apiVersion: databases.example.com/v1
          kind: Database
          name: my-db

      dbFailed:
        reason: DatabaseFailed
        type: Warning
        regarding:
          apiVersion: databases.example.com/v1
          kind: Database
          name: my-db
```

The declaration name is therefore distinct from the Kubernetes Event's own metadata name.

---

## Event matching

An Event declaration can match on:

* `reason`
* `action`
* `type`
* `reportingController`
* `reportingInstance`
* `regarding`
* `related`
* namespace
* `keyFrom`

Fields that are not specified are wildcards.

For example:

```yaml
operatorBox:
  observe:
    events:
      database-warning:
        type: Warning
        reason: DatabaseDegraded
```

matches Events with that type and reason regardless of their reporting instance or related object.

`regarding` and `related` can constrain the Kubernetes object referenced by the Event:

```yaml
regarding:
  apiVersion: databases.example.com/v1
  kind: Database
  name: my-db
```

These fields describe **what the Event is about**. They do not cause Orkestra to rewrite the Event or infer an ownerReference.

---

## Key resolution

Once an Event declaration matches, Orkestra resolves the primary CR using the same routing machinery used by secondary watches.

A declaration can use:

```yaml
keyFrom:
  label: app.kubernetes.io/cr-owner
```

or:

```yaml
keyFrom:
  name: my-operator
  namespace: default
```

The Event's referenced objects can also be used as matching constraints, but `regarding` and `related` are not themselves routing directives.

If no explicit routing rule resolves a key, the observation can use the normal broadcast fallback.

---

## Event lifecycle with `on:`

Event declarations support the same observation lifecycle as watches:

```yaml
operatorBox:
  observe:
    events:
      dbready:
        reason: DatabaseReady
        on: [create]
```

Supported values are:

```text
create
update
delete
```

If `on:` is omitted, all three observation types are considered.

This is useful when an Event declaration should react only to a particular lifecycle transition.

---

## Multiple matching declarations

A single Kubernetes Event can match more than one Event declaration.

For example:

```yaml
operatorBox:
  observe:
    events:
      allDatabaseEvents:
        regarding:
          kind: Database

      databaseWarnings:
        type: Warning
        regarding:
          kind: Database
```

One Event may therefore produce multiple Event contexts during the same observation.

Each matching declaration retains its own name:

```text
events.allDatabaseEvents
events.databaseWarnings
```

---

## Event context during reconciliation

A matching Event is observation context for the reconciliation attempt. The declaration name identifies the context.

Conceptually:

```text
.events.dbready.reason
.events.dbready.type
.events.dbready.reportingController
.events.dbready.regarding
.events.dbready.related
```

This allows both enqueue and reconciliation decisions to reason about the same observed Event without putting Event-specific logic into the workqueue.

The Event context describes the Event occurrence that caused or contributed to the reconciliation attempt. It does not mean that the Event is stored as the primary CR's latest Event.

---

## Interaction with `preReconcile.enqueueGate`

An Event-triggered enqueue enters the same `preReconcile.enqueueGate` as every other reconciliation trigger.

The Event is available as observation context, while the primary CR remains the object being reconciled.

This keeps Event observation separate from queue scheduling and reconciliation itself:

```text
Event
  │
  ▼
EventEntry match
  │
  ▼
primary CR key
  │
  ▼
queue
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

---

## `observe.events` versus `enrich: events`

These features are complementary but serve different purposes.

`observe.events` reacts **to an Event occurring**:

```yaml
operatorBox:
  observe:
    events:
      dbFailed:
        reason: DatabaseFailed
```

`enrich: events` fetches Events **during reconciliation**:

```yaml
operatorBox:
  enrich:
    - events
```

Use `observe.events` when an Event should trigger reconciliation.

Use `enrich: events` when reconciliation needs to inspect Event data.

You can use both when you need both behaviors.

---

## Schema reference

→ [operatorBox.observe.events schema](../../../reference/schema/02-katalog/28-events.md)
