# pkg/intent/target

`target` is the runtime face of Orkestra's intent model. It sits between the gateway (where intent arrives) and the reconciler (where the CR is processed), owning the three responsibilities that make per-target dispatch work.

## What lives here

**Target resolution** — reads the serve-target and serve-alias annotations from a CR and returns the effective target name. Resolution order: alias (most specific) → target → empty string. Every reconcile cycle starts here so the right operatorBox and the right hooks are selected.

**Intent-to-CR translation** — takes a flat intent payload (`{"target": "app", "repository": "...", "replicas": 2}`) and builds a full `Unstructured` CR from it. Routes each field to its declared destination: `serve.fields` → `spec.*`, `serve.labels` → `metadata.labels`, `serve.annotations` → `metadata.annotations`. Resolves `serve.name` and `serve.namespace` from the same payload via template expressions. Unknown fields are silently ignored — the caller's vocabulary and the CRD's structure don't have to match.

**MuxReconciler** — dispatches `Reconcile(ctx, key)` to the right `domain.Reconciler` based on the CR's target annotation. Targets with a registered constructor (via `TargetReconcilerRegistry`) get their own reconciler instance. CRs with no annotation, or an unknown target, fall through to the CRD-level reconciler. Deletion cycles route to the reconciler that handled the last create (tracked in a `sync.Map`). All CRD-level infrastructure (queue injection, autoscale, resync, metrics) is forwarded to the fallback.

## How the pieces connect

```
Gateway API (intent payload)
    │
    ▼
BuildCRFromTarget          — flat fields → Unstructured CR
    │
    ▼  (kubectl apply / SSA)
    CR lands in cluster with orkestra.io/serve-target annotation
    │
    ▼
MuxReconciler.Reconcile
    │
    ├── ResolveTargetFromAnnotations → "v2-ctor"
    │
    ├── targets["v2-ctor"]           → per-target domain.Reconciler
    │
    └── fallback                     → CRD-level Generic Reconciler
```

`MuxReconciler` is only wired when `CRDEntry.HasTargetConstructorFactories()` returns true — CRDs with only per-target hooks stay on `Generic Reconciler`, which handles hook dispatch in `hooksFor()`.
