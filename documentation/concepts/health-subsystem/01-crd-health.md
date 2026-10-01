# CRD Health

Each operatorBox tracks its own health state independently using a `CRDHealth` instance. Health is updated on every reconcile cycle.

---

## What CRD health tracks

| Field | Description |
|---|---|
| `started` | Whether the reconciler has begun processing events |
| `healthy` | Whether the reconciler is currently considered healthy |
| `totalReconciles` | Total reconcile attempts |
| `failedReconciles` | Number of failed reconciles |
| `consecutiveFails` | Consecutive failure counter — drives degradation |
| `lastError` | Last error message |
| `lastReconcile` | Timestamp of last reconcile |
| `startTime` | When the reconciler first started |

All fields are atomic and safe for concurrent updates from multiple workers.

---

## On success

```go
RecordSuccess()
```

- increments total reconciles
- resets consecutive failures to zero
- marks healthy
- updates `lastReconcile` timestamp

## On failure

```go
RecordFailure(err, failureThreshold)
```

- increments total and failed reconcile counts
- increments consecutive failures
- stores `lastError`
- marks unhealthy if `consecutiveFails >= failureThreshold`

---

## Degradation

A CRD becomes **unhealthy** when:

```text
consecutiveFails >= failureThreshold
```

The threshold is configurable per CRD in the Katalog - `queue.failureThreshold`. Unhealthy CRDs are visible in the Control Center.

---

## Health endpoints

Each CRD exposes its health through the operator's health server:

```text
GET /katalog/{crd}/health   — live health status (200 healthy, 503 unhealthy)
GET /katalog/{crd}          — configuration + health summary stats
GET /katalog                — all CRDs with health
```

These endpoints power the Control Center dashboard, readiness checks, and any automation that needs to detect a failing CRD without watching the CR directly.
