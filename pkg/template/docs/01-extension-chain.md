# Extension chain

The resolver is built incrementally. Each `With*` call returns a new resolver with one additional top-level key in the data map. The base resolver starts with the CR's own fields; each extension adds context from a different source.

## Order and availability

```
Step   Method                  Adds              Available in
─────  ──────────────────────  ────────────────  ─────────────────────────────────
  1    NewResolver(ctx, obj)   .spec.*           All evaluators
                               .status.*
                               .metadata.*
  2    .WithChildren(map)      .children.<n>.*   Resource conditions, status fields
  3    .WithItem(val, as)      .item / .<as>     forEach expanded expressions
  4    .WithExternal(map)      .external.<n>.*   Conditions after external: calls
  5    .WithCross(map)         .cross.<crd>.*    Reconcile-time templates
  6    .WithHealth(map)        .health.*         Admission, enqueueGate, reconcileGate
  7    .WithMetrics(map)       .metrics.*        Admission, enqueueGate, reconcileGate
  8    .WithRequest(map)       .request.*        Target-mode admission and preReconcile
  9    .WithSentinels(…)       sentinel funcs    enqueueGate, reconcileGate, behaviour
 10    .WithUserNotes(reg)     note funcs        Everywhere
 11    .WithPrevious(map)      .previous.*       Rollback path only
```

## Sources

| Key | Source |
|---|---|
| `.spec.*`, `.status.*`, `.metadata.*` | The CR itself, at event time |
| `.children.*` | Kubernetes objects owned by the CR, read after reconcile |
| `.external.*` | HTTP call results from `external:` declarations |
| `.cross.*` | Other operators' CR data — informer cache (same-binary) or ONCOP HTTP (cross-binary) |
| `.health.*` | Runtime health annotations written onto the CR after each reconcile |
| `.metrics.*` | Runtime metrics annotations written onto the CR after each reconcile |
| `.request.*` | Raw intent payload from the serve API in target mode |
| `.previous.*` | Previous status snapshot for rollback condition evaluation |

## Immutability

Each `With*` call shallow-copies the data map before adding the new key. The original resolver is unchanged. This means you can branch the chain safely:

```go
base := resolver.WithExternal(externalData)
branchA := base.WithCross(crossA)
branchB := base.WithCross(crossB)
// branchA and branchB do not share state
```

The reconciler builds the chain linearly, but this property makes the resolver safe for concurrent gate evaluation without locks.

→ Next: [The FuncMap](02-funcmap.md)
