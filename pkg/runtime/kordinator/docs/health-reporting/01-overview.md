# CRD Health Reporting — Overview

Orkestra exposes live health state for every managed CRD through HTTP endpoints on the runtime process. The Control Center fetches this state on a periodic background loop and renders it in the UI.

This works correctly with `replicaCount: 1`. With `replicaCount > 1` and leader election enabled, only one pod (the konductor) runs the reconcilers. Follower pods serve requests but their in-memory `CRDHealth` never advances — they always return `state: pending, healthy: false`.

This document series covers how the health signal is tracked, surfaced, and correctly consumed under multi-replica deployments.

---

## The problem

```
Runtime Service (2 pods)
  ├── Pod A  — konductor (leader)    → state: healthy, totalReconciles: 101
  └── Pod B  — follower              → state: pending, totalReconciles: 0

Control Center
  └── background fetch → Service → round-robins to A or B
        → display flips between healthy and pending on every other tick
```

With HTTP keep-alive (Go's default), the CC's HTTP client reuses the first TCP connection to a given Service. Whichever pod it connected to first is where ALL subsequent requests go — permanently. Two runtimes whose first connection landed on a follower pod are stuck showing "pending" for the lifetime of the CC pod.

---

## The solution: `isKonductor`

The konductor pod sets an atomic flag on `RuntimeHealth` at the start of `Kordinate()`. Every runtime HTTP response includes this flag. The CC uses it to decide whether the response came from an authoritative source.

```
Kordinate() called on winning pod
  → orkHealth.SetIsKonductor(true)
  → reconcilers start
  → CRDHealth advances: pending → started → healthy

Pod loses leadership
  → orkHealth.SetIsKonductor(false)
  → Kordinate() returns
```

The flag is present on every endpoint that returns health-sensitive data:

| Endpoint | Field | Used for |
|----------|-------|----------|
| `GET /katalog` | `"isKonductor"` | Background fetch cache guard |
| `GET /katalog/{crd}/health` | `"isKonductor"` | Per-CRD detail page retry |

---

## Pages in this series

| Page | Covers |
|------|--------|
| [02-runtime.md](02-runtime.md) | How `isKonductor` is set and surfaced on the runtime |
| [03-control-center.md](03-control-center.md) | How the CC consumes it — cache guard and retry logic |
| [04-diagnosis.md](04-diagnosis.md) | Diagnosing multi-replica health issues |
