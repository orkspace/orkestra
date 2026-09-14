# CRD Health Reporting — Runtime

## `RuntimeHealth` and `isKonductor`

`RuntimeHealth` is the operator-level health aggregate. It is a single shared instance created in `konstructRuntime` and passed to every handler that serves health-sensitive data. It is separate from the per-CRD `CRDHealth` objects.

```go
type RuntimeHealth struct {
    name        string
    orkReady    atomic.Bool  // runtime process is up and serving
    katReady    atomic.Bool  // all CRDs in the Katalog are online
    allOnline   atomic.Bool  // set when topo-order startup completes
    isKonductor atomic.Bool  // this pod holds the leader election lease
    mu          sync.RWMutex
}
```

`isKonductor` is set at the start of `Kordinate()` — the function that is only called on the pod that wins the leader election:

```go
func (k *DependencyKordinator) Kordinate(ctx context.Context) {
    k.orkHealth.SetOrkReady()
    k.orkHealth.SetIsKonductor(true)   // ← only the leader reaches this

    // ... starts all CRD workers in dependency order ...

    <-ctx.Done()   // blocks until leadership is lost

    k.orkHealth.SetIsKonductor(false)  // ← clears on leadership loss
    k.orkHealth.SetOrkDegraded()
}
```

Follower pods start the runtime, bind the HTTP server, and serve requests — but never call `Kordinate()`. Their `isKonductor` stays `false` for their entire lifetime.

---

## `GET /katalog`

The top-level katalog endpoint. Used by the CC background fetch loop.

**Leader pod:**
```json
{
  "name": "data-platform",
  "total": 10,
  "totalEnabled": 10,
  "OrkReady": true,
  "isKonductor": true,
  "healthy": true,
  "status": 200,
  "statusCounts": {
    "healthy": 10,
    "degraded": 0,
    "started": 0,
    "pending": 0
  }
}
```

**Follower pod:**
```json
{
  "name": "data-platform",
  "total": 10,
  "totalEnabled": 10,
  "OrkReady": true,
  "isKonductor": false,
  "healthy": false,
  "status": 200,
  "statusCounts": {
    "healthy": 0,
    "degraded": 0,
    "started": 0,
    "pending": 10
  }
}
```

`OrkReady: true` on both pods — the runtime process is running on both. `isKonductor` is the distinguishing signal.

---

## `GET /katalog/{crd}/health`

Per-CRD health endpoint. Used by the CC CRD detail page for state, error counters, and reconcile timestamps.

`isKonductor` here comes from the same `RuntimeHealth` instance as `/katalog`. It is NOT the per-CRD `h.orkHealth` field — that is a separate uninitialised instance attached to `CRDHealth` for other purposes. Both handlers take the global `o *RuntimeHealth` explicitly:

```go
func BuildCRDHealthHandler(
    crd orktypes.CRDEntry,
    kfg *konfig.Konfig,
    inf cache.SharedIndexInformer,
    h   *CRDHealth,
    o   *RuntimeHealth,   // ← global instance from konstructRuntime
) http.HandlerFunc
```

**Leader pod (`isKonductor: true`):**
```json
{
  "name": "website",
  "state": "healthy",
  "status": 200,
  "isKonductor": true,
  "healthy": true,
  "started": true,
  "pending": false,
  "startedAt": "2026-05-29T14:53:01Z",
  "uptime": "12m14s",
  "queueDepth": 0,
  "errorRate": 0,
  "consecutiveFails": 0,
  "totalReconciles": 101,
  "resourceCount": 1,
  "lastError": "",
  "lastReconcile": "2026-05-29T15:05:14Z",
  "hasUnhealthyDependencies": false
}
```

**Follower pod (`isKonductor: false`):**
```json
{
  "name": "website",
  "state": "pending",
  "status": 200,
  "isKonductor": false,
  "healthy": false,
  "started": false,
  "pending": true,
  "startedAt": "not started",
  "uptime": "not started",
  "queueDepth": 0,
  "errorRate": 0,
  "consecutiveFails": 0,
  "totalReconciles": 0,
  "resourceCount": 1,
  "lastError": "",
  "lastReconcile": "not started",
  "hasUnhealthyDependencies": false
}
```

---

## `GET /katalog/{crd}`

Per-CRD info endpoint. Used by the CC CRD detail page for worker pool state, queue depth, resync, RBAC, and providers.

Worker fields are only meaningful on the leader — the follower never starts reconcilers so its worker counts are all zero and `workerDetails` is empty.

```go
func BuildCRDInfoHandler(
    crd orktypes.CRDEntry,
    kfg *konfig.Konfig,
    inf cache.SharedIndexInformer,
    h   *CRDHealth,
    o   *RuntimeHealth,   // ← global instance from konstructRuntime
    ...
) http.HandlerFunc
```

**Leader pod (`isKonductor: true`):**
```json
{
  "name": "website",
  "isKonductor": true,
  "workers": 3,
  "workersActive": 3,
  "workersIdle": 3,
  "workersProcessing": 0,
  "workerDetails": {
    "demo.orkestra.io/v1alpha1-Kind=Website-worker-0": "idle",
    "demo.orkestra.io/v1alpha1-Kind=Website-worker-1": "idle",
    "demo.orkestra.io/v1alpha1-Kind=Website-worker-2": "idle"
  },
  "resync": "15s",
  "queueDepth": 0,
  "maxDepth": 100
}
```

**Follower pod (`isKonductor: false`):**
```json
{
  "name": "website",
  "isKonductor": false,
  "workers": 3,
  "workersActive": 0,
  "workersIdle": 0,
  "workersProcessing": 0,
  "resync": "15s",
  "queueDepth": 0,
  "maxDepth": 100
}
```

`workers: 3` is the configured value (from the Katalog) — present on both pods. `workersActive`, `workersIdle`, `workerDetails` are live runtime state — only meaningful on the leader.

`resourceCount` is identical on both pods — it comes from the informer cache, which is synced from the Kubernetes API independently of leadership.

---

→ Next: [03-control-center.md](03-control-center.md)
