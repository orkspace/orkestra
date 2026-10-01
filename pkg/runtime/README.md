# pkg/runtime

The runtime process is the reconciliation engine. It watches custom resources and drives them to the desired state declared in the Katalog.

| Sub-package    | Responsibility |
|----------------|----------------|
| [reconciler/](reconciler/README.md) | Core reconcile loop — normalize, resolve, apply |
| [kordinator/](kordinator/README.md) | CRD workqueue, dependency graph, CR fan-out, health tracking |
| [konductor/](konductor/README.md)  | Leader election and conductor lifecycle |
| [informer/](informer/README.md)    | CR watcher, namespace enforcement, shared index informer factory |
| [runners/](runners/README.md)      | Per-resource Kubernetes apply — Deployment, Service, Secret, Role, … |
| [autoscaler/](autoscaler/README.md)| Workload and operator autoscaler |
| [queue/](queue/README.md)          | Rate-limited, deduplicating workqueue per CRD |

## Cross-domain imports

`runners/` imports `pkg/gateway/certmanager` for TLS certificate generation.
`reconciler/` imports `pkg/gateway/notification` for outbound webhook dispatch.

These are intentional — the runtime delegates TLS and notification to gateway infrastructure.

## Shared packages

Packages used by both the runtime and other components stay at `pkg/` root:
`katalog`, `kubeclient`, `resources`, `merger`, `children`, `profiles`, `typeregistry`,
`event`, `note`, `secrets`, `health`, `metrics`, `konfig`, `labels`, `logger`, `types`, `version`.
