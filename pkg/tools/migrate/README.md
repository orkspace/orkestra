# pkg/tools/migrate

`migrate` rewrites a controller-runtime reconciler file for Orkestra. It is invoked by `ork migrate` and produces a rewritten Go file plus full scaffolding — `katalog.yaml`, `simulate.yaml`, `e2e.yaml`, `go.mod`, `Makefile`, and `Dockerfile` — as a ready-to-run starting point.

---

## Try it first

```bash
ork init --pack from-controller-runtime
```

Eight progressive examples — from the raw controller-runtime baseline through five migration options to the automated `ork migrate` output. The step-by-step narrative is in `documentation/guides/migration/`.

---

## Two modes

### `--mode toclient` (default)

Zero changes to your reconciler. `Reconcile`, struct fields, and all call sites are untouched. Only `SetupWithManager` is removed and a constructor is injected:

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

`ToClient` returns the same `client.Client` your reconciler already holds. `ReconcilerFrom` adapts the `ctrl.Request` signature to Orkestra's interface. Your reconciler compiles and runs inside Orkestra with no other edits.

### `--mode native`

Full rewrite to idiomatic Orkestra style:

| Before | After |
|--------|-------|
| `Reconcile(ctx, req ctrl.Request) (ctrl.Result, error)` | `Reconcile(ctx context.Context, req domain.Request) (domain.Result, error)` |
| `return ctrl.Result{}, err` | `return domain.Result{}, err` |
| `return ctrl.Result{}, nil` | `return domain.Result{}, nil` |
| `return ctrl.Result{RequeueAfter: X}, nil` | `return domain.Result{RequeueAfter: X}, nil` |
| `req.String()` | `req.String()` (preserved — `domain.Request` implements `Stringer`) |
| `req.NamespacedName` | `req.NamespacedName` (available directly on `domain.Request`) |
| `r.client.Get(ctx, key, obj)` | `r.kube.Get(ctx, namespace, name, obj)` |
| `r.Status().Update(...)` | flagged with `// TODO(ork migrate):` |
| `SetupWithManager` | removed with explanation comment |

More invasive; produces fully idiomatic Orkestra code.

---

## Usage

```bash
# Default (toclient) — zero Reconcile changes
ork migrate ./controller/webapp_controller.go -o ./my-operator

# Full rewrite
ork migrate ./controller/webapp_controller.go --mode native -o ./out

# In-place — prompts before replacing
ork migrate ./controller/webapp_controller.go
```

---

## Review checklist

After running `ork migrate`, search for `TODO(ork migrate)`:

```bash
grep -rn "TODO(ork migrate)" .
```

**toclient mode:**
- [ ] Add `domain` and `kubeclient` imports where flagged
- [ ] Set `group`, `kind`, `plural`, `location` in `katalog.yaml`
- [ ] Delete `main.go` and scheme registration — Orkestra provides the runtime
- [ ] Fill in resource assertions in `simulate.yaml` and `e2e.yaml`

**native mode (additional):**
- [ ] Replace `r.Status().Update()` with `r.kube.PatchStatus(ctx, obj, map[string]interface{}{...})`
- [ ] Resolve any `RequeueAfter` TODOs — return `err` requeues with backoff

---

## What Orkestra provides for free

| Concern | Orkestra |
|---------|----------|
| Informer watching your CRD | ✓ |
| Workqueue with dedup and backoff | ✓ |
| Worker pool | ✓ |
| Panic recovery | ✓ |
| Arbitrary watch | ✓ |
| Conditional reconciliation | ✓ |
| Leader election | ✓ |
| Prometheus metrics | ✓ |
| Health tracking | ✓ |
| `ork control` UI | ✓ |

And more.

---

## Developer documentation

| I want to… | Go to |
|-----------|-------|
| Understand what the output looks like | [docs/01-output.md](docs/01-output.md) |
| See a before/after of the generated files | [docs/02-generated-files.md](docs/02-generated-files.md) |
| Understand what the tool cannot auto-fix | [docs/03-limitations.md](docs/03-limitations.md) |
