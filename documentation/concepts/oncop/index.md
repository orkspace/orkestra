# ONCOP — Cross-Operator Observation

No operator is an island. Deployments depend on queues, queues depend on databases, autoscalers depend on metrics from other operators. Real systems are networks of operators that need to observe each other.

Orkestra formalises this with a layered observation protocol:

```text
1. Informer registry  — same binary, zero API calls (in-memory)
2. ONCOP              — cross-binary, cross-cluster (HTTP)
3. Raw endpoint       — arbitrary JSON source (fallback)
```

**ONCOP** (Orkestra Native Cross-Operator Protocol) is the middle layer. It lets one operator read another operator's state — typed, cacheable, and without hard-coded URLs.

---

## The `cross:` declaration

All cross-operator observation is declared in the `cross:` block of an operatorBox:

```yaml
operatorBox:
  cross:
    # Same-binary: zero API calls, informer cache only
    - crd: database
      selector:
        name: "{{ .metadata.name }}-db"
      as: db

    # Cross-binary: ONCOP over HTTP
    - crd: loader
      selector:
        name: "{{ .metadata.name }}-loader"
        namespace: loader-system
      source:
        host: "http://loader-runtime.loader-system:8080"
        protocol: cr
        cacheFor: 10s
      as: loader
```

After `ReadCross` runs, both are available identically in templates:

```text
{{ .cross.db.status.phase }}
{{ .cross.loader.status.phase }}
{{ .cross.loader.metrics.queueDepth }}
```

The source of the data — informer cache or ONCOP HTTP — is transparent to the template.

---

## ONCOP protocols

Five observation surfaces, each mapping to a stable endpoint:

| Protocol | What you get | URL shape |
|---|---|---|
| `cr` | Full CR: status, spec, children, metrics | `/katalog/<crd>/cr/<ns>/<name>` |
| `health` | Operator health state and last error | `/katalog/<crd>/health` |
| `metrics` | Operator-level metrics | `/katalog/<crd>` |
| `info` | CRD info: list, metrics, children | `/katalog/<crd>` |
| `events` | CR-scoped event stream | `/katalog/<crd>/cr/<ns>/<name>/events` |

Default protocol is `cr`.

---

## URL inference — no hard-coded URLs

Given a `cross:` declaration with `source.host`, ONCOP constructs the URL from `protocol + crd + selector`. You never write the URL:

```yaml
source:
  host: "http://loader-runtime:8080"
  protocol: cr
crd: loader
selector:
  name: my-loader
  namespace: default
```

Becomes: `http://loader-runtime:8080/katalog/loader/cr/default/my-loader`

This is what makes ONCOP portable — the Katalog describes *what* to observe, not *where* it lives.

---

## Raw endpoint fallback

For non-Orkestra APIs that expose the same JSON shape, use `endpoint` directly:

```yaml
source:
  endpoint: "https://my-api.example.com/status/{{ .metadata.name }}"
  cacheFor: 30s
```

`endpoint` bypasses ONCOP entirely. Any service that returns the right JSON can be observed as a cross: source.

---

## Using cross data in templates

Cross data is available anywhere the resolver runs — `onReconcile` conditions, status fields, autoscale conditions:

**Resource conditions:**
```yaml
onReconcile:
  deployments:
    - name: "{{ .metadata.name }}"
      when:
        - field: cross.loader.status.phase
          equals: "Ready"
```

**Status fields:**
```yaml
status:
  fields:
    - path: loaderPhase
      value: "{{ .cross.loader.status.phase }}"
    - path: loaderQueueDepth
      value: "{{ .cross.loader.metrics.queueDepth }}"
```

**Autoscale conditions:**
```yaml
autoscale:
  conditions:
    - when:
        - field: cross.loader.metrics.queueDepth
          greaterThan: "60"
          source:
            host: "http://loader-runtime:8080"
            cacheFor: 10s
      override:
        workers: 10
```

---

## Caching

Every ONCOP source caches its result. Default: `30s`. Set `cacheFor:` to control how often the remote operator is polled:

```yaml
source:
  host: "http://loader-runtime:8080"
  protocol: health
  cacheFor: 5s   # fast health checks
```

Caching prevents hammering the remote operator on every reconcile cycle. Same-binary informer cache reads are not cached — they are always current.

---

## What ONCOP unlocks

Same-binary `cross:` already enables coordination between operators in one Katalog. ONCOP extends this across process boundaries:

- **Cross-service dependencies** — application operator waits for database operator in a different binary to be healthy
- **Platform-wide autoscaling** — processor scales based on loader queue depth running in a separate runtime
- **Unified status** — parent CR surfaces health and metrics from child operators across clusters
- **Non-Orkestra integration** — any service exposing the ONCOP JSON shape becomes observable

The composition story: Motif composes resources. ONCOP composes operators.
