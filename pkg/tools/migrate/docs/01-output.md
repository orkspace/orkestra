# Output — the rewritten file

## toclient mode (default)

`ork migrate` (without `--mode`) produces the minimum change needed to run your reconciler inside Orkestra. Three things happen:

### SetupWithManager is removed

Before removal, `ork migrate` scans `SetupWithManager` and extracts:

- **`For(&pkg.Kind{})`** → `apiTypes.kind`, `object`, `objectList`, `version`, `location`, `alias` in `katalog.yaml`
- **`Owns(&pkg.Kind{})`** → `constructor.managedResources:` entries (kind + apiVersion for standard k8s types)
- **`Watches(&pkg.Kind{}, …)`** → `operatorBox.observe.watch:` entries

Only `group` and `plural` cannot be determined from Go source — they remain as TODOs.

The method itself is replaced with a comment:

```go
// SetupWithManager removed — Orkestra provides the informer, workqueue,
// worker pool, leader election, panic recovery, and metrics.
// Delete this file's main.go and scheme registration too.
```

### A constructor is injected

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

`orkadapter.ToClient` returns a `client.Client` — the same type your struct field already holds. `domain.ReconcilerFrom` adapts the `ctrl.Request` signature to Orkestra's interface. Your `Reconcile` method body is completely untouched.

### Orkestra imports are injected

```go
"github.com/orkspace/orkestra/domain"
"github.com/orkspace/orkestra/pkg/kubeclient"
```

The `ctrl` import is **kept** — the Reconcile signature and body are unchanged, so `ctrl.Request`, `ctrl.Result`, and `ctrl.LoggerFrom` still compile.

---

## native mode (`--mode native`)

Full mechanical rewrite to idiomatic Orkestra style.

### Signature change

```go
// Before
func (r *WebAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)

// After
func (r *WebAppReconciler) Reconcile(ctx context.Context, req domain.Request) (domain.Result, error)
```

`req.String()` returns `namespace/name` — same as before. `req.NamespacedName` is available directly on `domain.Request`. The `ctrl` import is removed.

### Return statements

Every `ctrl.Result` is rewritten:

```go
// Before
return ctrl.Result{}, err
return ctrl.Result{}, nil
return ctrl.Result{RequeueAfter: 30 * time.Second}, nil

// After
return domain.Result{}, err
return domain.Result{}, nil
return domain.Result{RequeueAfter: 30 * time.Second}, nil
```

`RequeueAfter` is preserved through `domain.Result` — no information is lost.

### req.NamespacedName

`req.NamespacedName` is available directly on `domain.Request` — no injection needed. Call sites that pass it to `r.Get` receive a TODO comment when the tool cannot decompose it automatically:

```go
r.kube.Get(ctx, namespace, name, obj /* TODO(ork migrate): extract namespace+name from: req.NamespacedName */)
```

### Struct simplified

The embedded `client.Client` (and any other ctrl-runtime fields) are replaced with a single `kube kubeclient.Interface` field. The constructor becomes:

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return &WebAppReconciler{kube: kube}
}
```

### r.Status().Update()

Flagged inline — Orkestra uses a different status API:

```go
nil /* TODO(ork migrate): replace with r.kube.PatchStatus(ctx, obj, map[string]interface{}{...}) */
```

### TODO markers

All items that need human review are marked `// TODO(ork migrate):`. After migration:

```bash
grep -rn "TODO(ork migrate)" ./my-operator/
```

---

Next: [Generated files](02-generated-files.md)
