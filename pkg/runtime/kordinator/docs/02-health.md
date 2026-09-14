# 02 — CRDHealth

`CRDHealth` tracks the runtime health of a single CRD's reconciler. Every field that is read or written from multiple goroutines uses `atomic` operations or `sync.Map` — there are no locks in the hot path.

## Fields

```go
type CRDHealth struct {
    name             string
    started          atomic.Bool
    pending          atomic.Bool
    healthy          atomic.Bool
    degraded         atomic.Bool
    totalReconciles  atomic.Int64
    failedReconciles atomic.Int64
    consecutiveFails atomic.Int64
    lastError        atomic.Value  // string
    lastReconcile    atomic.Value  // time.Time
    startTime        atomic.Value  // time.Time
    queueReg         *queue.QueueRegistry

    // CRD presence at runtime
    crdExists  atomic.Bool
    crdCheckMu sync.RWMutex

    // Worker state
    totalWorkers      atomic.Int32
    idleWorkers       atomic.Int32
    processingWorkers atomic.Int32
    workerStates      sync.Map  // workerID → "idle" | "processing" | "stopped"
    gvk               string

    // Dependency health
    dependencies     map[string]DependencyStatus
    dependenciesMu   sync.RWMutex
    hasUnhealthyDeps atomic.Bool
    healthySignaled  atomic.Bool

    // Autoscaler worker snapshot — populated by kordinator after reconciler construction
    workerInfoFn func() *ork_autoscaler.WorkerInfo

    // Rollback tracking — updated by callbacks injected from the reconciler
    rollbackTotal   atomic.Int64
    rollbackActive  atomic.Bool
    rollbackLastAt  atomic.Value  // time.Time
    rollbackMu      sync.RWMutex
    rollbackLastReason string
}
```

## Health states

A CRD moves through the following states during its lifetime:

| State | How it is set | Meaning |
|---|---|---|
| `pending` | Informer created, `SetPending()` called | CRD exists in the Katalog but workers have not started yet |
| `started` | `startCRDWorkers` calls `SetStarted()` | Worker goroutines are running |
| `healthy` | First `RecordSuccess()` call | At least one reconcile has completed without error |
| `degraded` | `consecutiveFails >= DegradeThreshold` | A sustained streak of failures; or CRD missing from cluster |

Recovery from `degraded` to `healthy` happens on the next `RecordSuccess()` — there is no hysteresis. A transient error spike that clears should not leave the CRD permanently degraded.

## Worker tracking

Workers call two methods around every reconcile item:

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

## Rollback tracking

When `operatorBox.rollback:` is declared, `startCRDWorkers` injects two callbacks into the reconciler via `SetRollbackNotifiers(onTrigger, onClear)`:

- `onTrigger` — called by `markRollbackActive` when the failure trigger fires. Increments `rollbackTotal`, sets `rollbackActive = true`, stores `rollbackLastAt = now`.
- `onClear` — called by `clearRollback` when the user submits a new spec generation. Sets `rollbackActive = false`.

`RollbackStats()` returns a snapshot struct for the handler:

```go
type RollbackStats struct {
    TotalRollbacks int64
    Active         bool
    LastRollbackAt string  // RFC3339, empty if never triggered
}
```

The handler includes this under `"rollback"` in the `/katalog/{crd}` response only when `crd.HasRollbackRules()` is true.

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

**Next →** [03 — Startup and dependency channels](03-startup.md)
