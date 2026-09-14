# Sentinel Names

Each sentinel is a named boolean computed at `UpdateFunc` time. The result is a string `"true"` or `"false"` stored in `QueueItem.SentinelMap` so it survives the queue.

---

## `generationChanged`

```go
old.GetGeneration() != new.GetGeneration()
```

The Kubernetes API server increments `metadata.generation` whenever the object's `spec` changes (for resources that track this). A `metadata.labels` or `metadata.annotations` change does not increment it.

**Use for**: skipping reconciliation when only non-spec fields changed — annotations written by the reconciler itself, status updates, or label patches that do not affect the desired state.

```yaml
preReconcile:
  sentinels: [generationChanged]
  enqueueGate:
    sentinels: [generationChanged]
```

**Note**: not all resources increment `generation`. Built-in types like `ConfigMap` and `Secret` do not. For those resources, `generationChanged` is always `"false"` on update events.

---

## `labelsChanged`

```go
!reflect.DeepEqual(old.GetLabels(), new.GetLabels())
```

True when the label map differs — any key added, removed, or changed in value.

**Use for**: re-running label-driven logic when a user re-labels a CR mid-lifecycle, or when a controller patches labels on a child resource you watch.

---

## `annotationsChanged`

```go
!reflect.DeepEqual(old.GetAnnotations(), new.GetAnnotations())
```

True when the annotation map differs.

**Use for**: annotations used as side-channel signals — e.g. a deployment tool writing a `deploy-timestamp` annotation that should trigger a reconcile.

Be careful: if your reconciler writes annotations back to the CR, every reconcile produces an update event. Without an additional guard (such as gating on `generationChanged` as well), this can produce a reconcile loop.

---

## `deletionStarted`

```go
old.GetDeletionTimestamp() == nil && new.GetDeletionTimestamp() != nil
```

True exactly once: the first event after a `kubectl delete` reaches the API server and `DeletionTimestamp` is set. By the time the object is dequeued for reconciliation, `DeletionTimestamp` is already non-nil — but this sentinel captures the transition at event time.

**Use for**: immediate enqueue of deletion logic without waiting for the normal reconcile cycle, or as a gate to short-circuit expensive reconcile work when the object is already terminating.

---

## `finalizersChanged`

```go
!reflect.DeepEqual(old.GetFinalizers(), new.GetFinalizers())
```

True when the finalizer list differs — any finalizer added or removed.

**Use for**: detecting when a finalizer has been removed externally (e.g. by a user force-removing it) so the reconciler can react before the object disappears.

---

## `nameChanged`

```go
old.GetName() != new.GetName()
```

True when the object's name differs between old and new.

**Use for**: almost never — Kubernetes does not rename resources in place. An update event with a changed name indicates a re-create (delete + create with the same key) seen as a single event, or a misconfigured informer. Useful as a defensive gate that fires an alert reconcile when this invariant is violated.

**Edge case**: In practice this is always `"false"` on normal update events. The Kubernetes API server rejects renames; if you see this as `"true"`, something unexpected has happened at the informer level.

---

## `namespaceChanged`

```go
old.GetNamespace() != new.GetNamespace()
```

True when the namespace differs between old and new.

**Use for**: same class of anomaly detection as `nameChanged` — Kubernetes does not move resources between namespaces. Useful as a defensive sentinel on cluster-scoped operators watching namespaced resources via a shared informer.

**Edge case**: Cluster-scoped resources have an empty namespace; this sentinel is always `"false"` for them.

---

## `generateNameChanged`

```go
old.GetGenerateName() != new.GetGenerateName()
```

True when the `metadata.generateName` prefix differs.

**Use for**: rarely needed post-creation. `generateName` is set once at creation time; it does not change on updates. This sentinel is most useful as a debugging aid or a defensive gate confirming the prefix is stable.

---

## `uidChanged`

```go
old.GetUID() != new.GetUID()
```

True when the UID differs — the object was deleted and a new object with the same name was created, and the informer delivered both events in sequence.

**Use for**: detecting a soft re-create — when you need to invalidate any state your reconciler tied to the previous UID (cached derived data, external registrations, lease ownership). Fires on the first update event after the new object's `Added` event in some informer cache configurations.

---

## `resourceVersionChanged`

```go
old.GetResourceVersion() != new.GetResourceVersion()
```

True when the resource version differs — i.e. any write to the object has occurred.

**Use for**: catching every update regardless of which field changed. `resourceVersionChanged` is the broadest possible sentinel — if you need to gate on "anything at all changed", this is it. Combine with narrower sentinels to handle different change classes differently.

**Note**: `resourceVersionChanged` is almost always `"true"` on update events by definition — the API server increments it on every write. Using it as an enqueue filter does not reduce queue pressure; it is more useful in `reconcileGate.when` expressions where you want to confirm the event reflects a real API server write.

---

## `creationTimestampChanged`

```go
old.GetCreationTimestamp() != new.GetCreationTimestamp()
```

True when the creation timestamp differs between old and new.

**Use for**: anomaly detection only — creation timestamp is immutable after the object is created. Like `nameChanged` and `uidChanged`, if this fires on a normal update event, something unexpected has happened. It can fire legitimately on the first `Modified` event for objects migrated across API server versions with timestamp normalization.

---

## `deletionGracePeriodSecondsChanged`

```go
!reflect.DeepEqual(old.GetDeletionGracePeriodSeconds(), new.GetDeletionGracePeriodSeconds())
```

True when the graceful deletion period (seconds) changed. Both old and new values are `*int64`; `reflect.DeepEqual` handles nil vs non-nil correctly.

**Use for**: detecting when a deletion grace period was set or modified — e.g. a user changed the grace period on an already-terminating object. Often paired with `deletionStarted` to differentiate the initial deletion event from a subsequent grace period change.

---

## `ownerReferenceChanged`

```go
!reflect.DeepEqual(old.GetOwnerReferences(), new.GetOwnerReferences())
```

True when the owner reference list differs — any owner added, removed, or modified.

**Use for**: detecting adoption or orphaning events. When a higher-level controller (e.g. a ReplicaSet controller) adds or removes an owner reference, this sentinel fires so your operator can react to the change in ownership without waiting for a spec change.

```yaml
preReconcile:
  sentinels: [ownerReferenceChanged]
  enqueueGate:
    sentinels: [ownerReferenceChanged]  # re-reconcile on adoption/orphan
```

---

## `managedFieldsChanged`

```go
!reflect.DeepEqual(old.GetManagedFields(), new.GetManagedFields())
```

True when the server-side apply managed fields differ — any field manager added, removed, or updated.

**Use for**: detecting field manager conflicts or tracking when another controller has taken ownership of a field your operator manages. Rarely needed in normal operator logic; more useful for diagnostic reconcilers or audit trails.

**Note**: `managedFieldsChanged` fires on almost every write if multiple field managers are active, since the managed fields entry for each manager is updated with each apply. Avoid using it as a standalone enqueue gate in high-churn environments — pair it with a more specific sentinel or a `when` expression.

---

## Combining sentinels in a gate

`enqueueGate.sentinels` is a list. The gate fires when **any** listed sentinel is `"true"`:

```yaml
enqueueGate:
  sentinels: [generationChanged, labelsChanged]
```

To require both, use a template expression in `enqueueGate.when` instead — sentinels are available as template variables in that context.

→ [02-compute.md](02-compute.md) — how sentinels flow from the informer event to the gate
