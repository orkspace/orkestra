# 01 — Architecture

## Where `pkg/external` sits

External calls execute at two distinct points in Orkestra's request lifecycle:

```text
Kubernetes event (Add/Update/Delete)
         │
         ▼
runTemplateReconcile()                       reconciler
   │
   ├── Step 1  NewResolver(obj)
   ├── Step 2  readCross(r.rc.Cross)
   ├── Step 3  external.Run()               ◄── here (reconcile time)
   │             .external.<name>.*
   ├── Step 4  runResourceGroup(onCreate)
   └── Step 5  runResourceGroup(onReconcile)

Admission webhook (ValidatingWebhookConfiguration)
         │
         ▼
gateway/webhook Validate()
   │
   └── external.Run()                        ◄── here (admission time)
         .external.<name>.*
         (validation rules evaluate against resolver)
```

Same package. Same `Run()` function. Same result structure. The reconciler enriches the resolver for resource creation and status computation. The webhook enriches it for validation rules — a CR can be rejected before the reconciler ever runs.

---

## The `Run()` pipeline

`runner.go:Run()` processes the `external:` block sequentially. Calls run in declaration order so each call can reference earlier calls' results in its own `url:`, `query:`, and `body:` template expressions.

```text
for each call:
   │
   ├── EvaluateConditions()           — skip if conditions not met
   │     result: skippedResult() injected; continue
   │
   ├── resolver.Resolve(url)    — template expressions in url: are evaluated
   ├── resolver.Resolve(query)  — template expressions in query: are evaluated
   ├── resolveAuth()            — secretRef or env → credential string
   │
   ├── cacheGet(key)            — check in-memory TTL cache (if cacheFor: set)
   │     hit:  use cached result; skip Fetch
   │     miss: continue to Fetch
   │
   ├── newProtocolClient(protocol).Fetch()
   │     → map[string]interface{}   (the result map)
   │
   ├── cacheSet(key, result, ttl)   — store if cacheFor: set
   │
   ├── resolver = resolver.WithExternal(results)
   │     — inject this call's result before the next call runs
   │     — this is what makes forward-referencing work
   │
   └── continueOnError check
         false + error → return error (reconcile/webhook fails)
         true  + error → log warn, hold current replica count, continue
```

### Injection happens per-call, not at the end

`resolver.WithExternal(results)` is called after every call — not once at the end. Call N+1 can reference `.external.callN.result` in its own `url:` or `query:`. This is how chained lookups work: look up a queue URL from Redis, then use that URL in an HTTP call.

### Hard errors vs soft errors

`Fetch()` itself almost never returns a Go error. Network failures, bad responses, and protocol errors are captured in `result["error"]` and returned as a successful `map[string]interface{}`. The Go error path is reserved for context cancellations and internal panics.

`continueOnError` controls whether a non-empty `result["error"]` fails the reconcile. When `true`, the call's result map still contains the error string — templates can inspect `external.myCall.error` to branch on it.

---

## Protocol dispatch

`protocol.go:newProtocolClient()` maps a protocol string to a `ProtocolClient` implementation:

| `protocol:` | Client | File |
|-------------|--------|------|
| `""` or `http` | `httpProtocolClient` | `http_client.go` |
| `prometheus` | `prometheusClient` | `prometheus.go` |
| `redis` | `redisClient` | `redis.go` |
| `postgres` | `postgresClient` | `postgres.go` |
| `mongo` | `mongoClient` | `mongo.go` |
| `kafka` | `kafkaClient` | `kafka.go` |

`ork validate` rejects unknown protocol values before `Run()` is ever called.

---

## `ProtocolClient` interface

```go
type ProtocolClient interface {
    Fetch(
        ctx          context.Context,
        spec         orktypes.ExternalCallSpec,
        resolvedURL  string,
        resolvedQuery string,
        credential   string,
    ) (map[string]interface{}, error)
}
```

`resolvedURL` and `resolvedQuery` are already template-evaluated by the time `Fetch` is called — the client never calls the resolver. `credential` is the raw credential string from `auth:` resolution — how it is used depends on the protocol (HTTP header, Redis password, Kafka SASL token).

---

**Next →** [02 — Result Maps](02-result-maps.md)
