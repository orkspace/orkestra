# Typed Operators

Every controller-runtime operator has two layers.

**Infrastructure** — Manager setup, informer cache, workqueue, predicates, event handlers, retry logic, leader election, metrics, panic recovery. This is the same in every operator. You write it, maintain it, debug it, and upgrade it — for every operator you build.

**Business logic** — your `Reconcile` function. The part that is actually yours.

Orkestra separates them. You write `Reconcile(ctx context.Context, req domain.Request) (domain.Result, error)` — the same logic you have today. Orkestra provides the rest: informers, workqueue, worker pool, retry backoff, watch on secondary resources, enqueue and reconcile gates, resync, leader election, metrics, panic recovery. You declare the topology in the Katalog. You never touch the infrastructure again.

```go
// Your reconciler — untouched. Same struct, same Reconcile signature, same logic.
type PipelineReconciler struct {
    client client.Client
}

func (r *PipelineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
    // your logic, unchanged
}

// The constructor — the only new code. Replaces main.go and SetupWithManager.
// ToClient adapts Orkestra's kube hub to the client.Client your reconciler already uses.
// ReconcilerFrom adapts the controller-runtime signature to Orkestra's interface.
func NewPipelineReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&PipelineReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

```yaml
# The infrastructure — declared, not written.
operatorBox:
  reconciler:
    workers: 4
    resync: 30s
    queue:
      retryBackoff: 500ms
  watch:
    - apiVersion: v1
      kind: ConfigMap
      name: shared-config
      on: [update]
      enqueueGate:
        when:
          - field: "{{ generationChanged }}"
            equals: "true"
  preReconcile:
    sentinels: [generationChanged]
```

Watches on secondary resources, retry backoff, enqueue filtering — declared. Your reconciler sees none of it. It just runs.

---

## Patterns

→ **[Hooks — hybrid](./01-hooks.md#hybrid)** *(recommended)* — declare everything Orkestra handles well in the Katalog; write Go only for what templates cannot express. Orkestra runs declared templates first, then your hook. The smallest surface area of Go code.

→ **[Hooks — hooks only](./01-hooks.md#hooks-only)** — the hook manages all child resources in Go. Use when type-safe control over every resource matters more than keeping declarations in YAML.

→ **[Constructor](./02-constructor.md)** — replace the reconciler entirely. Your Go code owns the full reconcile loop. The right entry point when migrating an existing controller-runtime operator — change the signature, remove the Manager, register the constructor. The rest of your code is unchanged.

→ **[Mixing all three](./03-mixed.md)** — a declarative operator, a hooks operator, and a constructor operator composed into one runtime from a single Komposer.

→ **[Migrating from controller-runtime](./05-migration.md)** — have a working controller-runtime operator? The `from-controller-runtime` pack shows the same operator expressed five ways, and `ork migrate` automates the constructor path.

→ **[Reusability](./06-reusability.md)** — one binary, many deployments. The same hook or constructor serves different environments, tiers, and tenants — API type routing via `apiTypes`, behavior routing via `args:`.

---

!!! note "Templates already see the full spec"
    Template expressions have access to the complete CR — `{{ .spec.* }}`, `{{ .status.* }}`, `{{ .metadata.* }}` — regardless of whether `apiTypes.location` is set. The template resolver converts any object to `map[string]interface{}` before executing expressions. Setting `location` is for Go code only.

## When to write Go

Stay declarative when your operator creates Kubernetes resources and applies rules. Reach for Go when you need:

- **Non-HTTP protocols** — SDK calls, gRPC, database connections, multi-step interactions that can't be expressed as "call a URL and gate on the response"
- **Complex computed fields** — logic that template expressions cannot express
- **An existing reconciler** — integrating a controller-runtime operator without rewriting it

---

## The infrastructure you are declaring

The Katalog fields that replace what you used to write:

- [watch](../operatorbox/watch.md) — secondary resource informers, enqueue filtering, key resolution
- [retryBackoff](../operatorbox/09-retry-backoff.md) — per-call and per-reconciler retry with exponential backoff
- [Conditional reconciliation](../conditional/04-conditional-reconciliation.md) — enqueue gates, reconcile gates, sentinels
- [Profiles](../operatorbox/06-profiles/) — worker count, resync, queue depth — named and reusable across CRDs
- [Reconciler model](../reconciler-model/) — how items move from the informer cache through the workqueue to your Reconcile call

