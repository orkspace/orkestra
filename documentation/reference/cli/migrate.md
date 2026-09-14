# ork migrate

Migrate a controller-runtime reconciler to Orkestra. By default, your `Reconcile` method is completely untouched — `ork migrate` removes `SetupWithManager` and injects a two-line constructor. The output also includes full operator scaffolding: `katalog.yaml`, `simulate.yaml`, `e2e.yaml`, `go.mod`, `Makefile`, and `Dockerfile`.

```bash
ork migrate <file> [flags]
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | *(none)* | Write all output to this directory (non-destructive; skips confirmation prompt) |
| `--mode` | | `toclient` | Migration mode: `toclient` (default) or `native` (full rewrite) |
| `--module` | | *(derived)* | Go module path for the migrated operator (e.g. `github.com/myorg/my-operator`) |
| `--name` | | *(derived)* | Operator name in kebab-case. Derived from receiver type if omitted. |

## Modes

### `--mode toclient` (default)

Zero changes to your reconciler. `Reconcile`, struct fields, and all call sites are untouched. Only `SetupWithManager` is removed and a constructor is injected:

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

`orkadapter.ToClient` returns a `client.Client` — the same type your struct already holds. `domain.ReconcilerFrom` adapts the `ctrl.Request` signature. Your reconciler compiles and runs inside Orkestra with no other edits.

### `--mode native`

Full rewrite to idiomatic Orkestra style:

| Before | After |
|--------|-------|
| `Reconcile(ctx, req ctrl.Request) (ctrl.Result, error)` | `Reconcile(ctx context.Context, req domain.Request) (domain.Result, error)` |
| `return ctrl.Result{}, err` | `return domain.Result{}, err` |
| `return ctrl.Result{}, nil` | `return domain.Result{}, nil` |
| `return ctrl.Result{RequeueAfter: X}, nil` | `return domain.Result{RequeueAfter: X}, nil` |
| `req.NamespacedName` | `req.NamespacedName` (available on `domain.Request` directly) |
| `req.String()` | `req.String()` (preserved — `domain.Request` implements `Stringer`) |
| `r.Status().Update(...)` | flagged `// TODO(ork migrate):` |
| `SetupWithManager` method | removed with explanation comment |
| `ctrl` import | removed |

## Examples

```bash
# Default (toclient) — zero Reconcile changes
ork migrate ./controller/webapp_controller.go -o ./my-operator

# Full rewrite to idiomatic Orkestra
ork migrate ./controller/webapp_controller.go --mode native -o ./my-operator

# Specify module path for go.mod and katalog.yaml location hints
ork migrate ./controller/webapp_controller.go \
  --module github.com/myorg/webapp-operator \
  -o ./webapp-operator

# Interactive: replace the file in place after confirmation
ork migrate ./controller/webapp_controller.go
```

## Output files

When `-o` is provided:

```text
my-operator/
  webapp_controller.go   rewritten reconciler
  katalog.yaml           constructor Katalog stub
  simulate.yaml          simulation stub
  e2e.yaml               end-to-end test stub
  go.mod                 module file with Orkestra pinned to this CLI version
  Makefile               registry, build, build-runtime, docker, release targets
  Dockerfile             distroless production image
```

## After migration

Search for `TODO(ork migrate)` in the output:

```bash
grep -rn "TODO(ork migrate)" ./my-operator/
```

**toclient mode:**
1. Set `group` and `plural` in `katalog.yaml` — `kind`, `version`, `location`, `alias`, `object`, `objectList`, `managedResources:`, and `watch:` are auto-detected from `SetupWithManager`
2. Delete `main.go`, scheme registration, and manager setup
3. Fill in resource assertions in `simulate.yaml` and `e2e.yaml`
4. Run `go mod tidy`

**native mode (additional):**
- Replace `r.Status().Update()` with `r.kube.PatchStatus()`

→ Full review checklist: [pkg/tools/migrate/README.md](https://github.com/orkspace/orkestra/blob/main/pkg/tools/migrate/README.md)

## Context

`ork migrate` is the last step in the `from-controller-runtime` migration pack. The pack shows the same operator expressed five ways — from the controller-runtime baseline through declarative, hybrid, hooks-only, and constructor options — and `ork migrate` automates the constructor path for an existing operator.

```bash
ork init --pack from-controller-runtime
```
