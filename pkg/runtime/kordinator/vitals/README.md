# pkg/runtime/kordinator/vitals

Health-state tracking for the Orkestra runtime.

This package holds the live health state for two things:

- **`CRDHealth`** — the lifecycle state of each CRD as the kordinator processes it: `pending`, `started`, `healthy`, `degraded`, along with the counters, worker states, and dependency status that feed the Control Center and the `/katalog` health endpoints.
- **`RuntimeHealth`** — holds the runtime's own readiness: whether the engine is ready, whether the Katalog is loaded, whether all CRDs are  online, and whether this pod is the current konductor (leader).

Both are **state only**. This package does not serve HTTP, register routes, or decide when to reconcile. The handlers that expose this
state, and the kordinator logic that drives the transitions, live one level up in [`kordinator`](../README.md).

## See also

- [Kordinator concept](https://orkestra.sh/docs/concepts/reconciler-model/kordinator/)
- [HTTP handlers](../docs/06-handlers.md) — how this state is served
- [Self-healing](../docs/04-self-healing.md) — how CRD state transitions propagate to dependents
- [Runtime Reporting](../docs/health-reporting/02-runtime.md)