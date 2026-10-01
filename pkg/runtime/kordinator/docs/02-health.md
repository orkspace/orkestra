# 02 — CRDHealth

`CRDHealth` tracks the runtime health of a single CRD. Every hot-path read and write uses `atomic` operations — no locks on the reconcile path.

## Health states

| State | Meaning |
|---|---|
| `pending` | CRD is registered but workers have not started |
| `started` | Worker goroutines are running |
| `healthy` | At least one reconcile completed without error |
| `degraded` | Consecutive failures exceeded `DegradeThreshold`, or CRD missing from cluster |

Recovery from `degraded` to `healthy` happens on the next successful reconcile — no hysteresis.

## What it tracks

- **Reconcile counts** — total, failed, consecutive failures, last error, last reconcile time
- **Worker states** — per-worker idle/processing/stopped, plus aggregate counters; updated atomically on each reconcile item, reflected in Prometheus gauges immediately
- **Dependency status** — kept fresh by `dependencyHealthChecker`; flows into `/katalog/{crd}` and the Control Center
- **Autoscaler snapshot** — `workerInfoFn` and `autoMetricsFn` closures set by `wireCRDHealthCallbacks` during startup; called on every `/katalog/{crd}` request for a live snapshot; omitted when no autoscaler is configured
```go
health.MarkWorkerProcessing(workerID)  // item dequeued, reconcile starting
// ... reconcile runs ...
health.MarkWorkerIdle(workerID)        // reconcile finished
```

Both methods update the `processing` and `idle` atomic counters and write the worker's state into `workerStates`. They also push Prometheus gauge updates immediately, so `controller_workers_processing` and `controller_workers_idle` metrics reflect the live state without any scrape delay.

`ResetWorkerCounts` is called during `deactivateCRD` — it zeroes the counters and marks every worker as stopped.

## Reconcile tracking

```go
health.RecordSuccess()
health.RecordFailure(errMsg string)
```

`RecordSuccess` increments `totalReconciles` and resets `consecutiveFails` to zero. If the CRD was degraded, it recovers.

`RecordFailure` increments `totalReconciles`, `failedReconciles`, and `consecutiveFails`. When `consecutiveFails` reaches `DegradeThreshold`, `degraded` is set to true.

## Dependency status

Each CRD's `CRDHealth` carries a `dependencies` map updated by the `dependencyHealthChecker` goroutine (see [04 — Self-healing](04-self-healing.md)):

```go
type DependencyStatus struct {
    Name                string
    State               string  // "pending" | "started" | "healthy" | "degraded" | "missing" | "unknown"
    Condition           string  // current state of the dependency
    AcceptableCondition string  // what the declaring CRD requires
    Satisfied           bool
}
```

`hasUnhealthyDeps` is set to true when any dependency is not satisfied. This flows into the `/katalog/{crd}` response and the Control Center.

## Autoscaler worker info

`workerInfoFn` is a zero-argument closure injected by `startCRDWorkers` after the reconciler is constructed. It calls `reconciler.WorkerInfo(configuredWorkers, configuredQueueDepth)` and returns a live snapshot for the `/katalog/{crd}` endpoint.

```go
h.SetWorkerInfoFn(func() *ork_autoscaler.WorkerInfo {
    info := rec.WorkerInfo(workers, queueDepth)
    return &info
})
```

`GetWorkerInfo()` calls the function on every request — it is never cached. Returns `nil` when no autoscaler is configured; the handler omits the field from the JSON response in that case.

`autoMetricsFn` is a companion closure that returns `AutoMetrics.AsMap()` — the same five metric fields exposed for autoscale condition evaluation. It is included in the `/katalog/{crd}` response as `"metrics"` and serves as the HTTP endpoint that cross-binary autoscale conditions call via `source.endpoint`, following the same fallback pattern as `readCross`.

```go
h.SetAutoMetricsFn(m.AsMap)  // m is *autoscaler.AutoMetrics
```

## RuntimeHealth

`RuntimeHealth` is the operator-level aggregate signal. It is separate from per-CRD health.

```
SetOrkReady()        — called at the start of Kordinate(); /ready returns 200
SetKatalogReady()    — called when all CRDs in the graph have started
SetKatalogDegraded() — called when any CRD is missing or degraded
SetOrkDegraded()     — called on leadership loss before shutdown
```

`/health` reflects `RuntimeHealth`. `/ready` reflects both `RuntimeHealth` and whether `Kordinate()` has started.

---

**Next →** [03 — Startup](03-startup.md)
