# 03 — Startup and dependency channels

`DependencyKordinator.Kordinate()` blocks for the operator's lifetime. It owns the full startup → run → shutdown sequence.

## The dependency graph

`konstructRuntime` builds a `DependencyGraph` from `dependsOn` fields in the Katalog. Kahn's algorithm produces a topological order — alphabetical tie-breaking within the same depth tier makes it deterministic across restarts. A cycle is a fatal error at startup.

## Dependency channels

Every CRD gets two channels at the start of `Kordinate()`:

- `startedCh[gvk]` — closed when `startCRDWorkers` returns for this GVK
- `healthyCh[gvk]` — closed by `dependencyHealthChecker` on first healthy reconcile

`dependenciesReady()` checks both channels with `select/default` — non-blocking. It returns `false` immediately if any channel is still open.

## The startup loop

The loop walks the topological order once. For each CRD it calls `dependenciesReady()` non-blocking: if not ready, the CRD is skipped. If the CRD's informer is missing from the cluster, it is also skipped. Otherwise `startCRDWorkers` is called and `startedCh[gvk]` is closed.

**The loop must never block.** If it blocked on a `condition: healthy` dependency, every alphabetically later CRD in the same tier would stall — even those with no dependency on the slow one. Skipped CRDs are activated by the retry loop (see [04 — Self-healing](04-self-healing.md)).

## What startCRDWorkers does

1. **`buildCRDRuntime`** — creates the `perCRDRuntime` for this GVK: a `ResizableSemaphore`, `AutoMetrics`, the `spawnWorker` closure, and the `Autoscaler` when `autoscale:` is declared. Registers `AutoMetrics` in `GlobalCrossMetricsRegistry` for cross-CRD condition evaluation. All state is owned by the kordinator.

2. **`wireCRDHealthCallbacks`** — sets `workerInfoFn` and `autoMetricsFn` on `CRDHealth` so the `/katalog/{crd}` handler can read live autoscaler state on every request.

3. **`ReconcilerFactory()`** — builds the reconciler.

4. **Autoscaler + resync goroutines** — started directly (`rt.autoscaler.Run`, `k.startResyncLoop`) when `autoscale:` is declared. Tied to the CRD's context.

5. **`launchWorkers`** — starts the baseline worker goroutines.

## Shutdown

Shutdown reverses the topological order so dependents drain before the CRDs they depend on. `stopCRDWorkers` cancels the CRD context, shuts down its queue, and waits for workers to exit.

---

**Next →** [04 — Self-healing](04-self-healing.md)
