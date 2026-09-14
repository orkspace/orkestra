# pkg/katalog/validate

The direct implementation of `ork validate`. Runs the full validation pipeline on a loaded `*katalog.Katalog`. All validators are methods on the unexported `executor` struct. `Execute` is the single public entry point.

## Entry points

| Function | Use |
|----------|-----|
| `Execute(k, kfg)` | Full validation pipeline — called by `pipeline.NewKatalog` and `pipeline.BuildExpanded` |
| `DetectCycles(k)` | Dependency cycle check only — used by integration tests |
| `ValidateGatewayClusters(k)` | Gateway cluster validation — called by `ork clusters` |
| `ValidateServe(k)` | Serve validation — called by `ork serve validate` |

## Pipeline order

`Execute` runs validators in numbered steps. Broadly:

1. **Struct validation** — field-level constraints via `konfig.Validate`
2. **Structural consistency** — uniqueness, dependsOn cycles, GVK, defaults
3. **Reconciler mode** — typed/dynamic, hooks/constructor requirements
4. **Resource wiring** — reconcilers, runtime objects, hooks, status
5. **Profile expansion** — autoscale, resources, probes, HPA, PDB, rolling update, network policy, resource quota, limit range
6. **Workload validation** — autoscale/HPA conflict, port protocols, envFrom suffix
7. **Security** — profiles, capabilities, gateway requirement, deletion/namespace protection, admission/mutation operators
8. **Serve** — fields, paths, order, namespace, tokens, target, aliases
9. **Gateway tokens** — duplicates, source exclusivity, OIDC rules
10. **External calls, notes, user profiles** — uniqueness and template validity

## Conventions

**Error display** — every error return must start with `failureMark()`:

```go
return fmt.Errorf("%s crd %q: ...", failureMark(), crdName)
```

**Multi-field errors** — use the boxed block format (see `gateway_tokens.go`): a `──────` border, mark + headline, indented key-value lines, and a `Fix:` block. Single-condition errors are plain one-liners.

**Mutations** — validators must not mutate the Katalog except for profile expansion (deliberate in-place replacement) and adding warnings via `AddWarning`. All other mutations belong in `setDefaults` or the enrichment layer.

**Template expressions** — fields containing `{{` are skipped (`orktypes.IsTemplate`). They are evaluated at reconcile time by the resolver.

## Adding a validator

1. Create `<topic>.go` in this package.
2. Write the method on `*executor` with the `failureMark()` convention on every error return.
3. Add a call in `execute.go` at the appropriate step.
4. Write `<topic>_test.go` using `newKatalogExec(crds)` or `newExec(&katalog.Katalog{...})`.
5. Add a minimal katalog YAML to [`testdata/validate/valid/`](../testdata/validate/valid/) and [`testdata/validate/invalid/`](../testdata/validate/invalid/) for end-to-end coverage with `ork validate`.
