# Migrating from controller-runtime

You have a working Kubernetes operator. The question is not whether controller-runtime works — it does. The question is what it costs: informers, workqueues, worker pools, leader election, status, finalizers, events, metrics — written from scratch for every CRD.

Orkestra removes the machinery. You keep the business logic.

---

## The migration pack

The `from-controller-runtime` pack shows the same WebApp operator expressed five ways so you can see what you are choosing between:

```bash
ork init --pack from-controller-runtime
```

| Option | Go required | What you own |
|--------|-------------|--------------|
| **00 — controller-runtime baseline** | Yes — full | Everything: informers, manager, scheme, main.go |
| **01 — declarative** | No | Nothing — pure YAML |
| **02 — hybrid** | Yes — hook only | The 10% templates can't express |
| **03 — hooks only** | Yes — all resources | All child resource specs in Go |
| **04 — constructor: minimal migration** | Yes — full reconciler | Reconcile logic; two lines added, nothing changed |
| **05 — constructor: Orkestra resources** | Yes — full reconciler | Reconcile logic; resource ops simplified |

Start at `00` and follow the READMEs — each step removes one layer of machinery.

---

## The mechanical path: `ork migrate`

`ork migrate` automates option 04. It takes your reconciler file, makes the minimum change needed to run inside Orkestra, and generates the scaffolding:

```bash
ork migrate ./controller/webapp_controller.go -o ./my-operator
```

Output:

```text
my-operator/
  webapp_controller.go   SetupWithManager removed, constructor injected — Reconcile untouched
  katalog.yaml           constructor Katalog stub — fill in group, kind, plural
  simulate.yaml          simulation stub — fill in expected resources
  e2e.yaml               end-to-end stub — fill in CR assertions
  go.mod                 module file with Orkestra pinned to this CLI version
```

### What the rewrite does

**SetupWithManager is removed** and replaced with a comment. Before removal, `ork migrate` scans it to extract `For()`, `Owns()`, and `Watches()` — those become `apiTypes`, `managedResources:`, and `watch:` entries in the generated `katalog.yaml`.

**A constructor is injected** — two lines:

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

`orkadapter.ToClient` returns a `client.Client` — the same type your struct field already holds. `domain.ReconcilerFrom` adapts the `ctrl.Request` signature to Orkestra's interface. Your `Reconcile` method body is completely untouched.

**`ctrl.Result{RequeueAfter: X}` is preserved** — the bridge forwards it to Orkestra's workqueue. No changes needed.

**Imports are injected** — `domain` and `kubeclient`.

### What still needs manual review

```bash
grep -rn "TODO(ork migrate)" ./my-operator/
```

- Set `group`, `kind`, `plural`, `location` in `katalog.yaml`
- Review `managedResources:` entries — add any resource kinds the operator manages that were not detected from `Owns()`
- Fill in resource assertions in `simulate.yaml` and `e2e.yaml`
- Delete `main.go`, scheme registration, and manager setup

---

## Option 05: Orkestra resources

After completing option 04, you can go one step further and replace the manual Get / IsNotFound / Create / Patch pattern with `pkg/resources`:

```go
// Before — manual (option 04)
existing := &appsv1.Deployment{}
err := r.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, existing)
if errors.IsNotFound(err) { return r.client.Create(ctx, desired) }
patch := client.MergeFrom(existing.DeepCopy())
existing.Spec = desired.Spec
return r.client.Patch(ctx, existing, patch)

// After — Orkestra resources (option 05)
return orkdeploy.Update(ctx, r.kube, webapp, spec)
```

`Update` handles create-if-absent, drift correction, owner references, and system labels. `DeleteIfOwned` removes a resource only if this CR owns it.

---

## Where to go next

- `from-controller-runtime` pack — `ork init --pack from-controller-runtime`
- [ork migrate reference](../../reference/cli/migrate.md)
- [Constructor concept](./02-constructor.md) — deep dive on the constructor pattern
