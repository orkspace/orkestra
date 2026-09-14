# Every CRD is a Live API

Kubernetes makes every CRD an API inside the cluster — a resource you can `kubectl apply` and `kubectl get`. Orkestra goes further: every CRD you declare in a Katalog also becomes a live HTTP API **outside** the cluster, served by the runtime on port 8080.

No code. No extra configuration. No service mesh. The moment the runtime starts, every operator has a full API surface.

---

## What the API gives you

For each CRD, the runtime exposes:

| What | URL | What it returns |
|---|---|---|
| Operator config | `/katalog/{crd}` | Workers, queue, resync, admission stats, metrics |
| Operator health | `/katalog/{crd}/health` | State, consecutive fails, last error, uptime |
| All CR instances | `/katalog/{crd}/cr` | Name, phase, ready, age — served from in-memory cache |
| One CR instance | `/katalog/{crd}/cr/{name}` | Full status, children keyed by kind |
| CR events | `/katalog/{crd}/cr/{name}/events` | Event stream, newest first |
| Raw CRD definition | `/katalog/{crd}/raw` | The CRD as you wrote it |
| Enriched definition | `/katalog/{crd}/enriched` | CRD with all Orkestra defaults applied |

The full Katalog also has top-level endpoints: `/katalog`, `/katalog/raw`, `/katalog/enriched`.

→ [Full endpoint reference](endpoints.md)

---

## Why this is different from the Kubernetes API

The Kubernetes API serves these same resources — but only from inside the cluster, via the API server, on every request. Orkestra's API is:

- **Outside the cluster** — reachable from any process with network access to the runtime
- **In-memory** — CR list and detail are served from the informer cache, not the API server
- **Zero latency for reads** — a reconciler writing to the cache and an HTTP client reading from it are looking at the same in-memory store

This is what makes features like the Control Center, ONCOP, and operator autoscaling work without a sidecar, without a service account for every consumer, and without hitting the API server on every reconcile.

Every response from the runtime includes an `isKonductor` field. In a multi-replica deployment, only the elected leader (the konductor) runs reconcilers — follower pods serve the API but their health state is always `pending`. Consumers that need authoritative data (like the Control Center) use `isKonductor: true` to confirm they are reading from the leader.

---

## The metrics field

Every CRD's `/katalog/{crd}` response includes a `metrics` map — even when no `autoscale:` block is declared. This is intentional.

The `metrics` map is the hook for cross-binary coordination: a sibling runtime in a different binary can read `cross.*.metrics.*` via HTTP without the observed CRD needing to declare anything. The publisher writes to its queue and worker state; the consumer reads it via ONCOP.

```json
{
  "name": "blockchainapp",
  "metrics": {
    "workers": 3,
    "queueDepth": 12,
    "errorRate": 0.0,
    "successRate": 1.0
  }
}
```

---

## The gateway API

The gateway process (when deployed) exposes a parallel API on port 8443:

- `GET /katalog` — admission, conversion, deletion-protection, and namespace-protection stats per CRD
- `GET /katalog/{crd}` — same stats for one CRD
- `POST /notify` — forward a notification event to SMTP or Slack

The gateway's `/katalog` response carries a `source: "gateway"` field and is keyed by GVR (`group/version/resource`). The Control Center reads both the runtime and gateway `/katalog` endpoints and merges them by GVR — neither process pushes to the other.

---

## Where the API matters

**Control Center** — reads `/katalog`, `/katalog/{crd}/health`, and `/katalog/{crd}/cr` on a live poll. No kubectl, no kubeconfig.

**ONCOP** — cross-binary operator reads use this API as the transport. `/katalog/{crd}/cr/{ns}/{name}` is the `protocol: cr` endpoint; `/katalog/{crd}/health` is `protocol: health`; `/katalog/{crd}` is `protocol: metrics` and `protocol: info`.

**Operator autoscaling** — the autoscaler reads `cross.*.metrics.*` from a sibling runtime via this API to scale workers based on another operator's queue depth.

**External tooling** — CI pipelines, dashboards, and integration tests can hit the runtime directly. `ork e2e` uses port-forward to assert health state without any Kubernetes API access.

→ [Full endpoint reference](endpoints.md)
→ [Endpoint access control](access-control.md) — disabling endpoints and cross reads per CRD
→ [ONCOP](../oncop/) — cross-binary observation protocol that rides on this API
→ [Operator Autoscaler](../operator-autoscaler/) — autoscaling that reads `metrics.*` from this API
