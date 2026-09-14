# Running Orkestra

## Can Orkestra manage multiple CRDs?

Yes — any number. This is the point.

Each CRD in a Katalog gets its own complete, isolated operator stack:

- Dedicated informer watching its exact GVK and API version
- Dedicated workqueue with independent depth and backoff
- Dedicated worker pool — other CRDs cannot consume its workers
- Dedicated health endpoint at `/katalog/{crd}/health`
- Dedicated Prometheus metrics labeled by GVK

All of these operator stacks run inside one Orkestra process. The isolation is at
the logic level. The shared infrastructure — API server connection, informer factory,
health server, leader election — is paid once.

!!! tip "The economics"
    10 separate operator processes: ~750 MB–3 GB memory, 10 health endpoints,
    10 metric schemas, 10 upgrade procedures.
    Orkestra managing 10 CRDs: ~79 MB memory, 1 health server, 1 metric schema,
    1 upgrade procedure.

---

## How do I start Orkestra?

Locally, for development:

```bash
ork run
# Orkestra reads katalog.yaml from the current directory and starts the runtime.
```

In a cluster, via Helm:

```bash
helm repo add orkestra https://orkspace.github.io/orkestra
helm upgrade --install orkestra orkestra/orkestra \
  --namespace orkestra-system \
  --create-namespace \
  --set runtime.katalog.existingConfigMap=my-katalog-configmap
```

See [Deploying](../deploying.md) for full cluster setup including TLS, RBAC, and production tuning.

---

## What does `ork validate` do?

`ork validate` runs the complete Katalog loading sequence without starting the runtime.
It surfaces every configuration error — bad YAML, unknown kinds, circular dependencies,
missing registry files, empty pattern files — before any cluster changes are made.

```bash
ork validate

✓ website
    kind: Website
    group: demo.orkestra.io / version: v1alpha1 / plural: websites
    mode: dynamic / workers: 3 / resync: 15s
    validation: 2 rules / mutation: 1 rule

✗ application
    error: circular dependency: application → namespace → application
```

!!! tip "Run in CI"
    `ork validate` exits with a non-zero code on any error. Add it to your CI
    pipeline to catch Katalog errors before they reach the cluster:
    ```yaml
    - name: Validate Katalog
      run: ork validate --full
    ```
    It requires no cluster connection — safe to run in any CI environment.

---

## Does Orkestra require cert-manager?

No. Orkestra needs TLS certificates for its HTTPS server (used by conversion
and admission webhooks) when `ENABLE_CONVERSION=true` or `ENABLE_ADMISSION_WEBHOOK=true`.
Where those certificates come from is your choice.

| Approach | Suitable for |
|---|---|
| Self-signed (generated at startup) | Development and testing |
| cert-manager `Certificate` resource | Production — automated renewal |
| External PKI / corporate CA | Enterprise environments with existing PKI |
| Cloud provider managed certs | Cloud-native deployments |

If no certificate is provided, Orkestra generates a self-signed certificate at startup and uses it automatically. This is the default behaviour — you do not need to configure anything to get TLS working locally or in development. For production, replace the self-signed cert with one from the table above.

The Helm chart includes optional cert-manager integration:

```yaml
certManager:
  enabled: true   # chart creates a Certificate resource and mounts the Secret
```

!!! note "Conversion and webhooks share one certificate"
    `/convert`, `/validate`, and `/mutate` all run on the same HTTPS server on
    `:8443` with the same TLS certificate. One certificate covers all three endpoints.

---

## What environment variables does Orkestra read?

| Variable | Default | Description |
|---|---|---|
| `ORK_PORT` | `8080` | HTTP server port |
| `ENABLE_CONVERSION` | `false` | Enable the `/convert` HTTPS endpoint |
| `ENABLE_ADMISSION_WEBHOOK` | `false` | Enable `/validate` and `/mutate` (requires `ENABLE_CONVERSION`) |
| `TLS_CERT` | — | Path to TLS certificate |
| `TLS_KEY` | — | Path to TLS key |
| `ORK_REGISTRY` | — | Default registry URL for `imports.registry` entries without explicit URL |
| `DEFAULT_WORKERS` | `3` | Worker count per CRD when not set in Katalog |
| `DEFAULT_RESYNC` | `15s` | Resync interval when not set in Katalog |
| `QUEUE_DEPTH` | `0` (unlimited) | Max queue depth when not set in Katalog |
| `LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `NAMESPACE` | — | Namespace where Orkestra runs — used in webhook configurations |
| `ORK_SERVICE_NAME` | `orkestra` | Service name for webhook clientConfig |
| `CONVERSION_WINDOW` | `1000` | Rolling window size for conversion and admission latency percentiles |

---

## What RBAC permissions does Orkestra need?

**The Helm chart does not manage ClusterRoles.** It deploys the Orkestra runtime (Deployment + Service). To generate the correct RBAC for your specific Katalog, use:

```bash
ork generate bundle
```

---
To generate for a specific Orkestra component:
```bash
ork generate bundle --for runtime
```

This produces a scoped ClusterRole, ClusterRoleBinding, and a ConfigMap containing your Katalog — ready to apply to the cluster.

---

## What does `ork generate bundle` do, and when do I re-run it?

`ork generate bundle` reads your Katalog and produces a single YAML document stream — ready to apply — containing:

- **ServiceAccounts** for runtime, gateway and control center
- **ClusterRole** with the minimal permissions derived from your Katalog
- **ClusterRoleBinding**
- **ConfigMap** embedding the Katalog itself

```bash
ork generate bundle --file katalog.yaml -o bundle.yaml
kubectl apply -f bundle.yaml
```

The ClusterRole is derived, not hand-written: only the API groups declared in your Katalog, only the verbs those resources actually need. If your operator creates no Deployments, the runtime has no `apps/deployments` permission.

When the Katalog declares `clusterRoles:` or `roles:` in `onCreate`/`onReconcile`, two extra verbs are added automatically — `escalate` and `bind` on `rbac.authorization.k8s.io` roles and clusterroles. `escalate` lets the runtime create roles that grant permissions it doesn't hold. `bind` lets it create bindings that reference those roles. Both are required by Kubernetes privilege escalation prevention and are absent from the bundle whenever no RBAC resources are managed.

**Re-run it every time the Katalog changes.** Adding a CRD, a new resource type, or a new API group makes the deployed bundle stale — the runtime will lack the permissions it now needs. Run it in CI alongside `ork validate`:

```bash
ork validate --full   # preview exact permissions per CRD per component
ork generate bundle --file katalog.yaml -o bundle.yaml
```

Both commands run entirely offline. No cluster connection required.

!!! tip "GitOps"
    Commit `bundle.yaml` to your repository. A diff on the bundle in code review makes every RBAC change visible and reviewable before it reaches the cluster.

---

## How do I debug a CRD in production?

Use the **Control Center** — it gives you a full view of all CRDs, worker pools, queue depth, reconcile metrics, and dependency health without any additional tooling.

For quick terminal diagnostics, the runtime exposes HTTP endpoints:

```bash
# CRD health — 200 OK or 503 degraded
curl localhost:8080/katalog/website/health | jq

# Full CRD detail — stats, queue depth, active warnings
curl localhost:8080/katalog/website | jq

# All managed CRDs
curl localhost:8080/katalog | jq

# Prometheus metrics
curl localhost:8080/metrics | grep website
```

!!! tip "Port-forwarding in-cluster"
    When Orkestra runs in a cluster, forward the ports before hitting the endpoints:
    ```bash
    ork proxy
    ```

The most common issues:

| Symptom | Likely cause |
|---|---|
| `/health` returns 503 | CRD degraded — check reconcile error rate in `/katalog/{crd}` |
| Resource not created | `when:` condition not met — check CR fields vs condition |
| Webhook rejection | Validation rule firing — read the error message in `kubectl apply` output |
| Stuck in terminating | `onDelete` Job blocked — check Job status in the CR's namespace |
| Old field values | Reconciler not running — check if CRD is enabled and healthy |

---

## What is the Control Center?

The Control Center is a web UI that reads directly from the Orkestra runtime APIs — no instrumentation, no custom metrics, no extra cluster resources. Start it locally with:

```bash
ork control
# Opens at http://localhost:8081
```

Five views, each a drill-down from the last:

| View | What it shows |
|---|---|
| **Control Center** | All Katalogs from all configured runtimes on one page |
| **Control Panel** | Per-Katalog: CRD cards, worker pools, queue pressure, error rates |
| **CRD Detail** | Per-CRD: every worker's state, RBAC, dependencies, admission metrics |
| **Resources** | Live CR list for that CRD — the actual objects being reconciled |
| **CR Detail** | Single CR: status fields, conditions, and child Kubernetes resources (grouped by kind, each with ready state and replica counts) |

To watch multiple clusters at once:

```bash
ork control --urls "http://cluster1:8080,http://cluster2:8080"
```

The Control Center holds no state of its own. It polls `/katalog`, `/katalog/{crd}`, and `/katalog/{crd}/health` on each runtime and renders the results. Refresh interval defaults to 10 seconds (`--refresh 5s` to tighten it).

Default credentials are `orkestra` / `orkestra`. Set `ADMIN_USERNAME`, `ADMIN_PASSWORD`, and `SESSION_SECRET` environment variables before exposing it beyond localhost.

→ [Control Center reference](../orkestra-core/03-controlcenter/)

---

## Is Orkestra safe for production?

Yes. Orkestra is designed for and demonstrated in production.

- **Leader election** — only one instance actively reconciles; followers maintain warm caches for instant failover
- **safeReconcile** — panics in any reconciler are caught; other CRDs are unaffected
- **Per-CRD failure domains** — a degraded CRD does not affect others
- **Graceful shutdown** — in-flight reconciles complete before the process exits
- **Conversion in production** — 62 conversions, 0 failures, sub-millisecond latency

!!! note "Failover time"
    Worst-case leader failover is 15 seconds (the lease duration). In practice,
    a follower on a healthy node with a warm cache starts reconciling within
    16–17 seconds of a leader crash. During this window, CRs are not modified —
    they are queued and processed when the new leader starts.

See [Trust and Failure Model](/publications/trust-and-failure-model/) for every
failure mode, what it means, and how Orkestra handles it.

---

## What happens if my reconciler panics?

The panic is caught. The operator process keeps running.

Every reconcile call runs inside `safeReconcile` — a deferred `recover()` that intercepts panics before they can unwind past the worker goroutine. When a panic occurs:

- The panic and its full stack trace are logged against the CRD that triggered it
- The CR is requeued with backoff — it will be retried
- Every other CRD in the runtime keeps reconciling without interruption
- The `/katalog/<kind>/health` endpoint returns 503 for the degraded CRD; others stay 200

A nil pointer in a typed hook, an out-of-bounds slice access, a failed type assertion — none of these bring down the operator. The failure is isolated to the CRD that produced it.

To see this in action:

```bash
ork init --pack resilience/safe-reconcile
cd safe-reconcile
ork run
```

The pack runs three CRDs simultaneously. One has a deliberate nil pointer in its hook. Apply its CR and watch the panic appear in logs while the other two keep reconciling cleanly.

→ [Panic recovery](../concepts/operatorbox/01-reconcile-pipeline/03-panic-recovery.md)

---

## Does the deletion protection webhook protect Orkestra itself?

Yes — including the webhook itself.

Every resource Orkestra deploys via Helm carries `orkestra.io/deletion-protection: "true"` from installation. The webhook intercepts every DELETE request and blocks any resource carrying that label — the runtime Deployment, the gateway Deployment, the control center, all Services.

The self-protection loop: the webhook also blocks deletion of its own `ValidatingWebhookConfiguration`. You cannot delete the webhook while it is running, because the webhook intercepts its own deletion request.

In full runtime mode, if the protection label is manually removed from a resource, the reconciler detects the drift on the next reconcile cycle and reapplies it. The label is treated as desired state, same as any other field in the Katalog.

!!! note "Gateway-only mode"
    Without `ork run`, the webhook still blocks deletions — but there is no reconciler to restore labels if they are manually removed. In gateway-only mode you are responsible for maintaining protection labels yourself.
    You can enable `strictMode` in this case.

---

## What happens when Orkestra restarts?

Nothing is lost. Orkestra follows standard Kubernetes deployment semantics with leader election. When the running instance exits — planned rollout, node failure, OOMKill — a follower pod acquires the lease and resumes reconciling. CRs are not modified during the transition; they are queued and processed when the new leader starts.

The transition window is controlled by the lease duration:

```yaml
# charts/orkestra/values.yaml
leaderElection:
  leaseDuration: 15   # seconds until a follower declares the leader dead
  renewDeadline: 10   # seconds the leader has to renew before losing the lease
  retryPeriod: 5      # seconds between follower acquire attempts
```

Override via Helm values or the `LEASE_DURATION` environment variable.

→ [Is Orkestra safe for production?](#is-orkestra-safe-for-production) — per-CRD failure isolation, safeReconcile, and the full failover timing breakdown

---

## Further reading

- **[Usage](./03-usage.md)** — validation, mutation, built-in kinds
- **[Ecosystem](./04-ecosystem.md)** — comparisons and the Kubernetes roadmap
- **[Deploying](../deploying.md)** — full cluster setup
