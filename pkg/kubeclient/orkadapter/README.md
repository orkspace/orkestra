# Orkestra Controller-Runtime Adapter

`orkadapter` is the compatibility boundary between controller-runtime and
Orkestra's native Kubernetes runtime.

Orkestra owns the reconciliation runtime:

- informers
- workqueues
- event-aware reconciliation
- enqueue and reconcile gates
- workers
- startup and health
- metrics
- scheduling

`kubeclient` owns Kubernetes operations:

- Get
- Create
- Update
- Patch
- Delete
- DeleteAllOf
- Apply
- Kubernetes API options
- dynamic client and REST mapping
- informer/cache primitives

`orkadapter` owns controller-runtime compatibility:

- `client.Client`
- controller-runtime request signatures
- controller-runtime options
- controller-runtime object/list types
- translation between controller-runtime options and Kubernetes options
- controller-runtime-specific cache/query semantics
- subresource client semantics

The important distinction is that `orkadapter` does not execute Kubernetes
operations itself. It adapts controller-runtime calls into Orkestra's native
kubeclient operations.

## Architecture

    controller-runtime reconciler
              │
              │ client.Client
              ▼
        ┌──────────────┐
        │ orkadapter   │
        │              │
        │ signatures   │
        │ options      │
        │ semantics    │
        └──────┬───────┘
               │
               │ native kubeclient operations
               ▼
        ┌──────────────┐
        │ kubeclient   │
        │              │
        │ Kubernetes   │
        │ cache/API    │
        └──────┬───────┘
               │
        ┌──────┴───────┐
        ▼              ▼
     Kubernetes      Simulate

## Why this boundary exists

A controller-runtime reconciler should be able to run inside Orkestra without
knowing whether its Kubernetes client is backed by a real cluster or the
simulation runtime.

The adapter provides the controller-runtime API surface.

The kubeclient provides the actual operation.

This means:

    client.Get(...)
          ↓
    orkadapter
          ↓
    kubeclient.Get(... metav1.GetOptions)
          ↓
    Kubernetes or simulation

The adapter translates the API. It does not become another Kubernetes client.

## Native kubeclient operations

The native interface uses Kubernetes-native object and option types rather than
controller-runtime types.

For example:

    Get(ctx, namespace, name, obj, metav1.GetOptions)
    Create(ctx, obj, metav1.CreateOptions)
    Update(ctx, obj, metav1.UpdateOptions)
    Patch(ctx, obj, patch, metav1.PatchOptions)
    Delete(ctx, obj, metav1.DeleteOptions)

This keeps the native client independent of controller-runtime while allowing
the adapter to provide the full controller-runtime client surface.

## List

`List` is currently the exception.

controller-runtime's List API includes cache/index semantics such as
`MatchingFields`, which map onto Orkestra's informer stores and indexers rather
than directly onto a single native Kubernetes operation.

For that reason List remains adapter-owned until its native contract is
defined.

## Simulation

The simulation kubeclient implements the same native kubeclient operations as
the real kubeclient.

The adapter is therefore identical in both environments:

    controller-runtime
          ↓
      orkadapter
          ↓
      kubeclient.Interface
          ├── Kubeclient
          └── FakeKubeclient

A reconciler does not need to know which implementation is underneath.

## Design principle

`orkadapter` adapts controller-runtime.

`kubeclient` performs Kubernetes operations.

Orkestra runtime owns reconciliation execution.

Those are three separate responsibilities.