# Reconciler Model

When a CR is applied to your cluster, Orkestra reconciles it — reads your Katalog, acts on it, and keeps the declared state correct over time. How it does that depends on which reconciler model you are using.

---

## Two reconciler models

### Generic Reconciler

The default. You write YAML — `onCreate`, `onReconcile`, `onDelete`, `hooks`, conditions, status fields — and Orkestra's built-in reconciler carries out those instructions. You own the declaration; Orkestra owns the loop.

This is the right model for most operators. It handles drift correction, templating, status patching, finalizers, ordered deletion, external calls, and more — without any Go code.

```yaml
operatorBox:
  onReconcile:
    deployments:
      - name: "{{ .Name }}-server"
        image: "{{ .Spec.Image }}"
```

Or with minimal Go hooks for logic that belongs in code:

```yaml
operatorBox:
  reconciler:
    hooks:
      location: github.com/myorg/operator/hooks
      function: AppHooks
```

### Your Reconcile()

When you need full control of the reconcile loop — or you are migrating an existing operator — you provide your own implementation by returning a `domain.Reconciler` from a constructor function declared in the Katalog.

```go
func NewAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return &AppReconciler{Client: orkadapter.ToClient(kube)}
}
```

```yaml
operatorBox:
  reconciler:
    default: false
    constructor:
      location: github.com/myorg/operator/controller
      function: NewAppReconciler
```

Orkestra provides the informer, workqueue, worker pool, leader election, and metrics. Your `Reconcile` method handles the business logic. If you are migrating from controller-runtime, `ork migrate` automates the initial scaffolding — see [from-controller-runtime](../typed-operators/05-migration.md).

---

## What the runtime provides to both models

Regardless of which model you use, Orkestra manages:

- One informer per CRD — a watch stream from the API server, kept in a local store
- One workqueue per CRD — items are deduplicated, rate-limited on error, and re-enqueued on a timer when `requeue:` is declared
- A configurable worker pool — concurrency is set via `reconciler.workers:` and can be adjusted at runtime with `autoscale:`
- Health tracking — each CRD moves through `pending → started → healthy → degraded` as it processes items
- Operational state on the CR — after every reconcile the runtime stamps `.health` and `.metrics` onto each CR, making live operator state readable from the CR itself without an HTTP call
- Startup sequencing — CRDs with `dependsOn:` declarations start in dependency order, not all at once

---

## The kordinator

The kordinator is the part of the runtime that owns startup, worker management, and health. It starts each CRD's workers when its declared dependencies are met, monitors CRDs for disappearance, restarts workers when a CRD reappears, and aggregates health across all CRDs. See [Kordinator](05-kordinator.md).

---

## Three reconcile flows

**Create / Update** — a CR appears or changes. Orkestra reads the object from the informer store, evaluates any gate conditions, runs the reconcile pipeline, patches status, and records health.

**Delete** — a CR receives a deletion timestamp. The finalizer holds deletion until `onDelete:` templates and hooks have run. Then the finalizer is removed and Kubernetes garbage-collects child resources via owner references.

**Drift correction** — a child resource is changed manually. Because the child emits a watch event, the parent CR is re-enqueued and the `onReconcile:` cycle runs again, restoring declared state.

---

## Per-target reconciliation

When your Katalog declares `serve.target:` entries, CRs routed through the gateway carry a target annotation. Each reconcile cycle reads that annotation and uses the operatorBox declared for that target — different hooks, different args, different conditions, potentially a different reconciler binary — all from the same CRD type. See [Serve targets](../self-service/02-target-mode.md).

---

## Where to go next

- [Create and Update](01-create-update.md) — the reconcile pipeline step by step
- [Delete](02-delete.md) — finalizer lifecycle and cleanup sequencing
- [Requeue](03-requeue.md) — per-object scheduled requeue after a successful reconcile
- [resync vs requeue](04-resync-vs-requeue.md) — when to use each and how they compose
- [Kordinator](05-kordinator.md) — startup sequencing, worker management, health
- [Queue behaviour](06-queue-behaviour.md) — controlled back-pressure at the queue boundary: `onLimit`, `onThreshold`, and two-tier conditional evaluation
- [Operational state on the CR](07-operational-state.md) — the runtime stamps live health and metrics onto each CR; readable in conditions, validation rules, and cross-CRD references
- [Event-Aware Reconciliation](08-event-aware-reonciliation.md) —  the reconciliation that follows the event
