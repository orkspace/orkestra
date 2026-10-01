# pkg/runtime/runners

Resource runners — one file per Kubernetes resource type.

Each runner takes a resolved list of template sources and applies them to the cluster: creating, updating, or deleting the corresponding Kubernetes objects according to the CR's declared state.

## What lives here

| File | Resource |
|------|----------|
| `secrets.go` | Secret (includes `once:`, `rotateAfter:`, `tls:`, `toNamespaces:`) |
| `configmaps.go` | ConfigMap (includes `toNamespaces:`, `fromConfigMap:`) |
| `serviceaccounts.go` | ServiceAccount |
| `roles.go` | Role |
| `rolebindings.go` | RoleBinding |
| `deployments.go` | Deployment |
| `statefulsets.go` | StatefulSet |
| `replicasets.go` | ReplicaSet |
| `services.go` | Service |
| `ingresses.go` | Ingress |
| `jobs.go` | Job |
| `cronjobs.go` | CronJob |
| `pods.go` | Pod |
| `pvcs.go` | PersistentVolumeClaim |
| `pvs.go` | PersistentVolume |
| `hpas.go` | HorizontalPodAutoscaler |
| `pdbs.go` | PodDisruptionBudget |
| `namespaces.go` | Namespace (create-only; no drift correction) |
| `secrets_once.go` | Helper: `once:` guard, `IsNotFoundErr` |
| `secret_tls.go` | Helper: TLS secret rotation |

## What does NOT live here

Runners that are specific to the reconciler's dispatch logic stay in `pkg/runtime/reconciler/`:

- `run_template_reconcile.go` — the dispatcher that calls each runner in sequence
- `run_delete_ordered.go` — sequential staged deletion
- `run_customresource.go` — cross-CRD runners
- `run_surface_cleanup.go` — removes resources from a previous surface that are no longer declared

Admission, validation, mutation, status writing, and namespace enforcement all moved to `pkg/runtime/kordinator/prepare/` as part of the kordinator refactor.

## Adding a new resource type

See [docs/](docs/README.md) for the full reference: runner contract, condition evaluation, forEach expansion, and the end-to-end walkthrough.
