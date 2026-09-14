# Evaluation — Resolve and conditions

## Resolve

```go
func (r *Resolver) Resolve(expr string) (string, error)
```

The primary evaluation method. Returns the expression result as a string. Values without `{{` are returned unchanged. Callers never need to distinguish static from dynamic.

```go
val, err := resolver.Resolve("{{ .metadata.name }}-svc")  // "my-app-svc"
val, err := resolver.Resolve("nginx:latest")               // "nginx:latest"
val, err := resolver.Resolve("{{ .spec.replicas }}")       // "3"
```

Template errors return the original expression and a non-nil error. Callers decide whether to propagate or fall back.

## EvaluateConditions

```go
func EvaluateConditions(resolver *Resolver, when []Condition, or []Condition) (bool, error)
```

Evaluates a `when:` + `or:` condition block against the resolver. Used by every gate in Orkestra — enqueueGate, reconcileGate, resource `when:`, queue `behaviour.onLimit.when:`, autoscale conditions.

Logic:
- `when:` — AND block: all conditions must pass
- `or:` — OR block: at least one must pass
- When both are present, both blocks must pass
- When neither is present, the block passes (unconditional)

Each condition resolves the `field:` expression against the resolver, then applies the declared operator (`equals`, `exists`, `greaterThan`, `lte`, `in`, `unique`, …).

## ResolveAll

```go
func (r *Resolver) ResolveAll(fields map[string]string) (map[string]string, error)
```

Resolves a map of field expressions in one call. Used for status field patches, label maps, and annotation maps where every value may be a template expression.

## Parse-only mode

At validate time, `ork validate` builds resolvers with nil data and stub sentinels. `Resolve` parses the expression without executing it — catching syntax errors and undeclared sentinel references before the operator runs.

---
**Next →** [Back to README](../README.md)
