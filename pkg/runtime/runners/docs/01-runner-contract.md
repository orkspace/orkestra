# 01 — The Runner Contract

Every file in `pkg/runtime/runners/` follows the same structure. This document describes each section in the order it appears.

## Canonical shape

```go
// pkg/runtime/runners/widgets.go
package runners

import (
    "context"
    "fmt"

    "github.com/orkspace/orkestra/domain"
    "github.com/orkspace/orkestra/pkg/kubeclient"
    "github.com/orkspace/orkestra/pkg/logger"
    orkwidget "github.com/orkspace/orkestra/pkg/resources/widgets"
    orktmpl "github.com/orkspace/orkestra/pkg/template"
    orktypes "github.com/orkspace/orkestra/pkg/types"
)

func RunWidgets(
    ctx      context.Context,
    kube     kubeclient.KubeClient,
    resolver *orktmpl.Resolver,
    owner    domain.Object,
    srcs     []orktypes.WidgetTemplateSource,
    update   bool,
    guard    func(ctx context.Context, obj domain.Object, ns string) bool,
) error {

    // ── Section A: activeNames pre-pass ──────────────────────────────────────
    activeNames := make(map[string]bool, len(srcs))
    for _, s := range srcs {
        if !orktypes.EvaluateConditions(resolver.Data(), s.Conditions, s.Or, resolver.TemplateEvaluator()) {
            continue
        }
        n, _   := resolver.Resolve(s.Name)
        nsp, _ := resolver.Resolve(s.Namespace)
        if nsp == "" {
            nsp = owner.GetNamespace()
        }
        activeNames[nsp+"/"+n] = true
    }

    // ── Section B: main loop ──────────────────────────────────────────────────
    for i, src := range srcs {

        // B1. Evaluate conditions
        conditionPassed := orktypes.EvaluateConditions(resolver.Data(), src.Conditions, src.Or, resolver.TemplateEvaluator())

        // B2. Early name/namespace resolution
        name, _ := resolver.Resolve(src.Name)
        ns, _   := resolver.Resolve(src.Namespace)
        if ns == "" {
            ns = owner.GetNamespace()
        }

        // B3. Namespace guard
        if guard != nil && !guard(ctx, owner, ns) {
            continue
        }

        // B4. Condition failure path
        if !conditionPassed {
            if update || src.Reconcile {
                if !activeNames[ns+"/"+name] {
                    if err := orkwidget.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
                        return fmt.Errorf("widgets[%d]: conditional cleanup: %w", i, err)
                    }
                }
            }
            logger.FromContext(ctx).Debug().
                Str("resource", "Widget").
                Int("index", i).
                Msg("conditions not met — skipping resource")
            continue
        }

        // B5. Resolve template expressions
        resolved, err := resolver.ResolveWidgetTemplate(src)
        if err != nil {
            return fmt.Errorf("widgets[%d]: %w", i, err)
        }

        // B6. Build registry spec and apply
        spec := orkwidget.Resolve(resolved, resolver.OwnerName())

        if update {
            if err := orkwidget.Update(ctx, kube, owner, spec); err != nil {
                return fmt.Errorf("widgets[%d].update: %w", i, err)
            }
        } else {
            if err := orkwidget.Create(ctx, kube, owner, spec); err != nil {
                return fmt.Errorf("widgets[%d].create: %w", i, err)
            }
            if src.Reconcile {
                if err := orkwidget.Update(ctx, kube, owner, spec); err != nil {
                    return fmt.Errorf("widgets[%d].reconcile: %w", i, err)
                }
            }
        }
    }
    return nil
}
```

---

## Section A — activeNames pre-pass

**Why it exists.** When two declarations target the same resource name but have mutually exclusive conditions (`when: typeOf == A` vs `when: typeOf == B`), the failing path's cleanup (`DeleteIfOwned`) would delete what the passing path just created. The pre-pass builds the set of `ns/name` pairs that have at least one passing condition. The main loop only calls `DeleteIfOwned` when the target is NOT in that set.

**When to omit it.** If the resource never calls `DeleteIfOwned` on the condition-failure path (e.g. `RunJobs` — jobs are terminal), omit Section A and the `activeNames` guard in B4.

---

## Section B1 — Condition evaluation

Always call `orktypes.EvaluateConditions(resolver.Data(), src.Conditions, src.Or, resolver.TemplateEvaluator())`.

- `resolver.Data()` — the full CR data map including `.children.*`, `.external.*`, `.cross.*`. Do NOT pass the `owner` object directly — it does not have these injected fields.
- `src.Conditions` — the `when:` block (AND semantics).
- `src.Or` — the `or:` block (OR semantics).

---

## Section B2 — Early name/namespace resolution

Resolve before the guard and before the condition-failure cleanup. `Resolve` is cheap; the full `ResolveXxxTemplate` in B5 re-resolves internally.

```go
name, _ := resolver.Resolve(src.Name)
ns, _   := resolver.Resolve(src.Namespace)
if ns == "" {
    ns = owner.GetNamespace()
}
```

The blank identifier on the error is intentional — resolution errors on name/namespace are surfaced in B5 during full resolution.

---

## Section B3 — Namespace guard

```go
if guard != nil && !guard(ctx, owner, ns) {
    continue
}
```

Always nil-check. The guard is nil when the CRD has no namespace restrictions. Call it after namespace resolution so the actual target namespace is known.

---

## Section B4 — Condition failure path

Only attempt cleanup when `update || src.Reconcile` — on first create (update=false, reconcile=false) there is nothing to clean up yet.

Wrap `DeleteIfOwned` with `if !activeNames[ns+"/"+name]` to prevent the failing path from deleting a resource the passing path owns.

---

## Section B5 — Template resolution

Call `resolver.ResolveXxxTemplate(src)`. This evaluates all `{{ }}` expressions in the source struct. Errors here are real — return them.

---

## Section B6 — Apply

```go
if update {
    orkwidget.Update(...)   // onReconcile — drift correction
} else {
    orkwidget.Create(...)   // onCreate — idempotent create
    if src.Reconcile {
        orkwidget.Update(...) // reconcile: true shorthand
    }
}
```

Resources that are create-only (ServiceAccount, Job) skip the `update` branch entirely.

---

## Signature variations

| Runner | `update bool` | `guard` | Notes |
|--------|--------------|---------|-------|
| `RunDeployments` | yes | yes | Full create/update |
| `RunServices` | yes | yes | Full create/update |
| `RunSecrets` | yes | yes | Extra: `once:`, `rotateAfter:`, `tls:`, `toNamespaces:` |
| `RunConfigMaps` | yes | yes | Extra: `toNamespaces:`, `fromConfigMap:` |
| `RunServiceAccounts` | yes | yes | Create-only (no update branch) |
| `RunCronJobs` | yes | yes | Full create/update |
| `RunJobs` | no | yes | Create-only, no activeNames pre-pass |
| `RunNamespaces` | yes | no | Create-only, no guard (cluster-scoped) |
| `RunPVs` | yes | no | No guard (cluster-scoped) |

---

## Error format convention

```go
return fmt.Errorf("widgets[%d]: %w", i, err)                     // resolution errors
return fmt.Errorf("widgets[%d].create: %w", i, err)              // apply errors
return fmt.Errorf("widgets[%d].update: %w", i, err)
return fmt.Errorf("widgets[%d]: conditional cleanup: %w", i, err)
```

The `[%d]` index tells operators exactly which declaration in the YAML failed.

---

## Common mistakes

**Forgetting the activeNames pre-pass.** Causes create/delete loops when two declarations target the same resource with mutually exclusive conditions.

**Passing `owner` to EvaluateConditions instead of `resolver.Data()`.** The `owner` object does not have `.children.*`, `.external.*`, or `.cross.*` — conditions referencing those fields silently fail.

**Not nil-checking the guard.** `guard` is nil when the CRD has no namespace restrictions. A nil dereference panics.

**Forgetting `reconcile: true` support.** The `if src.Reconcile { Update(...) }` block in the non-update branch is how `reconcile: true` works on onCreate. Without it, the shorthand is silently ignored.

**Not exporting the function name.** Functions in `pkg/runtime/runners/` are exported (`RunWidgets`, not `runWidgets`) — they are called from `pkg/runtime/reconciler/` and can be called from anywhere else that needs the same resource logic.

---

→ Next: [02 — Garbage Collection](02-garbage-collection.md)
