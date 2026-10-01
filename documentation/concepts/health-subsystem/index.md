# Health Subsystem

Orkestra's health subsystem is a lightweight HTTP server that exposes Kubernetes-native probe endpoints and per-CRD health state. It starts before anything else and stays alive until shutdown.

---

## Operator-level probes

Four endpoints serve the operator's lifecycle state:

| Endpoint | Probe type | Returns |
|---|---|---|
| `GET /startup` | startupProbe | 200 once startup is complete; 503 while booting |
| `GET /health` | livenessProbe | 200 when healthy; 500 on fatal condition |
| `GET /ready` | readinessProbe | 200 when ready; 503 during startup and shutdown |
| `GET /metrics` | — | Prometheus metrics |

The `/startup` probe prevents liveness and readiness probes from running too early — Kubernetes will not send traffic or restart the pod until `/startup` returns 200.

---

## Operator health states

The server tracks four independent flags:

| State | Set when | Clears when |
|---|---|---|
| `started` | HTTP server binds and begins serving | Never once set |
| `startup` | `SetStartupComplete()` is called | Never once set |
| `healthy` | HTTP server starts | `Unhealthy()` is called (fatal condition) |
| `ready` | `SetReady()` is called | `Degraded()` or `Shutdown()` is called |

**`Degraded()`** — transitions `ready` to false without touching `healthy`. The operator is alive but not ready to accept traffic. Used for transient conditions.

**`Unhealthy()`** — sets `healthy` to false. Signals a fatal condition. The liveness probe fails and Kubernetes restarts the pod.

---

## CRD-level health

Each operatorBox tracks its own `CRDHealth` instance — see [CRD Health](01-crd-health.md).

---

## Katalog API routes

The health server also hosts all CRD-specific endpoints. These are registered by the runtime before `Start()` is called — they cannot be added after the server starts:

```text
GET /katalog              — all CRDs with health summary
GET /katalog/{crd}        — configuration + health for one CRD
GET /katalog/{crd}/health — live health status for one CRD
```

---

## Where to go next

- [CRD Health](01-crd-health.md) — per-CRD health tracking, degradation logic, and health endpoints
