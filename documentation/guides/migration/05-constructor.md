# Constructor — Zero Changes

You have a working controller-runtime operator. The reconcile logic is solid. What you want to remove is the machinery around it — the manager, the workqueue, the scheme registration, the leader election setup, the metrics. Not rewrite the operator from scratch.

Option 04 is the zero-change path. Your `Reconcile` method is completely untouched. Two lines wire it into Orkestra.

```bash
ork init --pack from-controller-runtime
cd from-controller-runtime/04-constructor-migration
```

---

## What you will learn

- How `orkadapter.ToClient` and `domain.ReconcilerFrom` bridge a controller-runtime reconciler into Orkestra
- What `default: false` means in the Katalog — the GenericReconciler is disabled, the constructor owns the loop
- What the runtime provides that `ctrl.NewManager` previously handled
- How `ork migrate` generates the constructor automatically

---

## The only new code

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

- `orkadapter.ToClient(kube)` — wraps Orkestra's `kubeclient.Interface` as a `client.Client`. The same type your struct already holds.
- `domain.ReconcilerFrom(r)` — adapts the `ctrl.Request` signature so Orkestra's worker pool can call it.

That is all. Your `Reconcile` method, your struct, your `r.Get` / `r.Create` / `r.Status().Update()` calls — unchanged.

---

## What you removed

`SetupWithManager`, `Scheme`, scheme registration, `main.go`, leader election setup. Orkestra provides all of it — informer, workqueue, worker pool, panic recovery, Prometheus metrics, health endpoints, leader election.

---

## `ork migrate` automates this

```bash
ork migrate ./controller/webapp_controller.go -o ./output
```

Default mode (`--mode toclient`) does exactly what this option shows: removes `SetupWithManager`, injects the two-line constructor, leaves everything else untouched.

See [06 — ork migrate](./07-ork-migrate.md) or run option 06 in the pack.

---

## Try it

```bash
ork init --pack from-controller-runtime
cd from-controller-runtime/04-constructor-migration
# Follow steps in README
```

→ [06 — Constructor: Orkestra resources](./06-constructor-resources.md)
