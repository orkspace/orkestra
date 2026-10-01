# pkg/runtime/kordinator

`kordinator` is the orchestration heart of every Orkestra operator. It decides when each CRD's workers start, in what order, under what conditions, and how they recover when the cluster changes while the operator is running.

Everything upstream of kordinator — informers, queues, the reconciler factory — produces inputs. Kordinator is where those inputs become running workers.

## What kordinator does

- **Topological startup** — reads the dependency graph from the Katalog and starts CRD workers in the correct order, without blocking on conditions that may take an arbitrary amount of time
- **Self-healing** — monitors the cluster continuously and activates CRDs that appear after startup, deactivates and re-activates CRDs that are deleted and recreated at runtime
- **Health tracking** — maintains per-CRD health state with atomic counters (total reconciles, failures, consecutive failures, worker utilization) and a separate aggregate operator health signal
- **Dependency gating** — enforces `condition: started` and `condition: healthy` dependency requirements before activating a dependent CRD, without ever blocking the startup sequence
- **Reconcile preparation** — prepares the reconciliation context for every reconciler type via `prepare.Prepare()`: normalize, enrichment (cross, external, git), mutation, and validation all run here before `Reconcile()` is called; the reconciler receives a fully-prepared `domain.Request`
- **System maintenance** — applies and enforces system labels, annotations, and finalizers via `maintain.Apply()` on every reconcile pass, regardless of reconciler type
- **Post-reconcile jobs** — after `Reconcile()` returns, runs `post.Apply()`: status field patching, declarative event emission (`emit.events:`), and runtime annotation updates
- **Autoscaler wiring** — when `autoscale:` is declared, `startCRDWorkers` builds a `perCRDRuntime` (semaphore, `AutoMetrics`, `Autoscaler`) owned by the `Kontroller`, wires `kordinatorTarget` as the `AutoscaleTarget`, and launches the autoscaler and resync goroutines tied to the CRD's context; `AutoMetrics` is registered in `GlobalCrossMetricsRegistry` for cross-CRD condition evaluation
- **Runtime introspection** — serves the `/katalog`, `/katalog/{crd}`, and `/katalog/{crd}/health` endpoints that power the Control Center

## Where kordinator fits

Kordinator is the last component started in `konstructRuntime`. The startup sequence is:

```
resourceKatalog      — CRD entries, informers, reconciler factories registered
informer.Factory     — informers created and started, caches warm
DependencyKordinator — workers start in dependency order  ← here
```

From this point on, every reconcile event flows through:

```
informer event
  └── queue.Add(key)
        └── runWorkerForGVK
              └── processItemForGVK
                    ├── pre-reconcile gate   operatorBox.preReconcile.when/or
                    ├── prepare.Prepare()    normalize → enrichment → mutation → validation
                    ├── maintain.Apply()     system labels, annotations, finalizers
                    ├── safeReconcile        panic-isolated call to rec.Reconcile(ctx, req)
                    └── post.Apply()         status patch + emit + runtime annotations
```

For CRDs with `autoscale:` declared, two additional goroutines run alongside
the worker pool for the lifetime of the CRD's context:

```
autoscaler goroutine   — evaluates conditions, calls ResizeWorkers / SetQueueDepthLimit / SetResyncInterval
resync goroutine       — re-enqueues all cached objects at do.resync when override is active
```

## Developer documentation

Complete documentation is in [docs/](docs/README.md).

| I want to understand… | Go to |
|---|---|
| The CRD registry and what each entry holds | [01 — ResourceKatalog](docs/01-registry.md) |
| How health state is tracked per CRD | [02 — CRDHealth](docs/02-health.md) |
| How workers start in dependency order | [03 — Startup and dependency channels](docs/03-startup.md) |
| How the operator recovers from missing or deleted CRDs | [04 — Self-healing](docs/04-self-healing.md) |
| How the worker loop runs and how shutdown drains cleanly | [05 — Workers and drain](docs/05-workers.md) |
| How spec normalization works | [06 — normalize](docs/06-normalize.md) |
| Multi-replica health and `isKonductor` | [07 — health reporting](docs/07-health-reporting.md) |

## Key types at a glance

| Type | File | Role |
|---|---|---|
| `DependencyKordinator` | `dependency_kordinator.go` | Startup, dependency gating, shutdown |
| `Kontroller` | `kontroller.go` | Base worker manager embedded by `DependencyKordinator` |
| `perCRDRuntime` | `crd_runtime.go` | Per-CRD autoscale state: semaphore, `AutoMetrics`, `Autoscaler`, resync interval, `spawnWorker` |
| `kordinatorTarget` | `crd_runtime.go` | `AutoscaleTarget` implementation backed by `perCRDRuntime` |
| `ResourceKatalog` | `kordinator_registry.go` | Per-GVK registry: informer, reconciler factory, CRD config |
| `CRDHealth` | `vitals/crd_health.go` | Per-CRD health counters, worker states, dependency status |
| `RuntimeHealth` | `vitals/runtime_health.go` | Aggregate operator health (ready / degraded) |
