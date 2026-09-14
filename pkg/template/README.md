# pkg/template

The resolver is Orkestra's expression evaluation engine. It wraps a CR's full object map and exposes a `Resolve(expr)` method that evaluates any Go `text/template` expression against the live CR context. Values without `{{` are returned unchanged — callers never need to distinguish static from dynamic values.

The resolver is the single evaluation surface used everywhere in Orkestra: admission webhooks, enqueueGate, reconcileGate, queue behaviour conditions, resource template fields, status field patches, autoscale conditions, and external HTTP expression interpolation.

---

## Core type

```go
type Resolver struct { ... }

func NewResolver(ctx context.Context, obj domain.Object) (*Resolver, error)
func NewResolverFromMap(data map[string]interface{}) *Resolver
func (r *Resolver) Resolve(expr string) (string, error)
func (r *Resolver) Data() map[string]interface{}
```

`NewResolver` builds the base context from a `domain.Object` — spec, status, and metadata are immediately available as `.spec.*`, `.status.*`, `.metadata.*`.

---

## Immutable extension chain

Every `With*` method returns a **new** resolver with the original unchanged. The pattern is safe for concurrent use — one reconcile's enriched context cannot leak into another's.

```
NewResolver(ctx, obj)         → .spec.*, .status.*, .metadata.*
  .WithChildren(map)          → + .children.<name>.*
  .WithItem(val, as)          → + .item / .<as>   (forEach loops)
  .WithExternal(map)          → + .external.<name>.*
  .WithCross(map)             → + .cross.<crd>.*
  .WithHealth(map)            → + .health.*
  .WithMetrics(map)           → + .metrics.*
  .WithRequest(map)           → + .request.*      (serve target mode)
  .WithSentinels(names, vals) → + sentinel functions in FuncMap
  .WithUserNotes(reg)         → + user-defined note functions in FuncMap
  .WithPrevious(map)          → + .previous.*     (rollback path)
```

---

## Template context

Every Go `text/template` expression is evaluated against the accumulated data map plus the full KEL function catalog (`pkg/note`). User-defined notes (the `notes:` block in a Katalog) are registered per-resolver via `WithUserNotes`.

```yaml
# These are all valid template expressions in any field
"{{ .metadata.name }}-svc"
"{{ .spec.replicas | default 2 }}"
"{{ if allReplicasReady .children.deployment }}healthy{{ else }}degraded{{ end }}"
"{{ .cross.database.status.endpoint }}"
"{{ .request.environment }}"
"{{ inBusinessHours }}"
```

---

## Sentinels

Sentinel functions are injected via `WithSentinels(declared, values)`. Each declared sentinel becomes a no-arg template function returning its computed string value. At validate time, pass `nil` for `values` — sentinels return `""` for parse checking only.

→ [pkg/runtime/sentinel](../runtime/sentinel/README.md) — how sentinel values are computed

---

## Progressive docs

| | |
|---|---|
| [01-extension-chain.md](docs/01-extension-chain.md) | With* methods — what each adds and when |
| [02-funcmap.md](docs/02-funcmap.md) | KEL functions, user notes, and sentinel injection |
| [03-evaluation.md](docs/03-evaluation.md) | Resolve(), EvaluateConditions(), and the condition model |
