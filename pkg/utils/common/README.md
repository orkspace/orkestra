# utils/common — shared runtime utilities

Utility functions shared by `pkg/runtime`, `pkg/gateway`, and `pkg/katalog` that don't belong to any one of those packages. This is a **utility leaf**: it must not import back into `pkg/runtime`, `pkg/gateway`, or `pkg/katalog`.

> [!IMPORTANT]
> This package has one constraint: it is imported by packages that cannot import each other. Keep it a leaf — if you find yourself needing to import a higher-level package, the function belongs in the caller, not here.

---

## Packages

### `query/`

HTTP client for querying the Orkestra runtime's `/katalog` endpoints at admission time. See [`query/README.md`](query/README.md).

---

## Functions

### Annotation helpers

```go
// Stamps a map[string]interface{} onto an unstructured object as a JSON annotation.
common.InjectAnnotationToObject(obj *unstructured.Unstructured, raw map[string]interface{}, annotation string)

// Reads a JSON annotation back out of a raw map[string]interface{} object.
common.ResolveAnnotationFromObject(obj map[string]interface{}, annotation string) map[string]interface{}
```

Used by the runtime to stamp `.metrics` and `.health` onto CRs as annotations, and by the gate evaluators to read those values into the resolver context so `preReconcile.enqueueGate` and `preReconcile.reconcileGate` conditions can reference `.metrics.*` and `.health.*`.

### Cross utilities

```go
// Resolves a cross: declaration's auth token from either a plain token or a tokenRef secret.
common.ResolveCrossToken(ctx, cs kubernetes.Interface, s *orktypes.CrossSource) (string, error)

// Fetches a CR's detail from a remote Orkestra /katalog/{crd}/cr endpoint.
common.FetchCrossViaHTTP(ctx, cs kubernetes.Interface, source *orktypes.CrossSource) ([]byte, map[string]interface{})
```

Both the autoscaler and the reconciler need to resolve cross tokens and fetch remote CR data. Centralised here to avoid duplication.
