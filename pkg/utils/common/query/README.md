# query — inter-component HTTP query client

HTTP client through which Orkestra components reach into each other to read live state. Today the only target is the runtime (`pkg/runtime`), implementing `domain.RuntimeQuery`. Future targets (e.g. the gateway) would add their own implementation of their own query interface.

> [!NOTE]
> `IsUnique` is **best-effort**: it reads from the runtime's informer cache, not a live List. Two concurrent requests can both pass — the reconciler's `liveUniquenessChecker` in `pkg/runtime/reconciler` is the authoritative check. `ForHealth` and `ForMetrics` return live operational stats stamped by the runtime, not cache reads.

---

## Usage

```go
q := query.NewRuntimeQuery(ctx, runtimeEndpoint, crdName)

// operator: unique validation — admission time
resolver = resolver.WithUniquenessChecker(q)

// gate on live health and metrics in preReconcile conditions
resolver = resolver.WithHealth(q.ForHealth())
resolver = resolver.WithMetrics(q.ForMetrics())
```

---

## Endpoints

| Method | Runtime endpoint | Returns |
|--------|-----------------|---------|
| `IsUnique(field, value, ns, name)` | `GET /katalog/{crd}/cr?field=<field>` | Whether no other CR has this value for this field |
| `ForHealth()` | `GET /katalog/{crd}/health` | CRD health summary as `map[string]interface{}` |
| `ForMetrics()` | `GET /katalog/{crd}` → `metrics` key | CRD metrics as `map[string]interface{}` |

All three become available in the resolver context:

```yaml
validation:
  rules:
    - field: spec.domain
      operator: unique          # uses IsUnique

    - field: "{{ .health.status }}"
      equals: healthy           # uses ForHealth

    - field: "{{ .metrics.queueDepth }}"
      operator: lte
      value: "80"               # uses ForMetrics
```

---

## Two-tier enforcement

`IsUnique` at admission is a fast early rejection, not a guarantee. Two concurrent requests could both pass — the reconciler's `liveUniquenessChecker` in `pkg/runtime/reconciler` does a live List() and is the authoritative check. This client exists to catch the common case cheaply.

`ForHealth` and `ForMetrics` return live operational stats: the runtime stamps these values onto the CR as annotations (via `common.InjectAnnotationToObject`) and the gate evaluators read them back into the resolver. The gateway admission path queries the runtime directly via this client.
