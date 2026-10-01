# Validation pipeline

Every Orkestra Pattern goes through a strict validation pipeline before anything runs. The whole pipeline runs entirely offline — no cluster required.

---

## Strict YAML parsing

Orkestra uses strict YAML unmarshaling. Unknown fields produce an immediate error — there are no silent defaults, no ignored typos. A field you didn't mean to write and one you did mean to write are both caught.

```text
error: unknown field "reconcileMode" in operatorBox — did you mean "reconcile"?
```

This applies to every Pattern: Katalog, Komposer, Motif, E2E, Simulate.

---

## `ork validate`

Validates an Orkestra Pattern fully without touching a cluster.

```bash
ork validate
ork validate -f path/to/katalog.yaml
```

What it checks:

- Schema: every field against the declared types and constraints
- Template expressions: `{{ }}` syntax is parsed and validated
- Dependency graph: cycles in `dependsOn` chains are a hard error
- Namespace rules: `allowedNamespaces`/`restrictedNamespaces` consistency
- CRD references: `crdFile` paths resolve, CRD group/version/kind/plural are valid

`ork validate` runs entirely offline. No `~/.kube/config` is read. No cluster is contacted.

### `--full`: security posture and permissions

```bash
ork validate --full
ork validate -f katalog.yaml --full
```

Adds three sections to the output — all computed offline from the Katalog declaration, no cluster required:

**Per-CRD permissions** — printed under each CRD header, showing the exact RBAC rules the CRD will require: its own resource and status subresource, any typed-mode managed resources (hooks and constructor templates), and any built-in Kubernetes resources its templates reference.

```text
✓ website
    kind: Website / group: demo.io/v1alpha1 / ...
    permissions:
      demo.io   websites         get list watch create update patch delete
      demo.io   websites/status  get update patch
      apps      deployments      get list watch create update patch delete
```

**Per-CRD profiles** — printed under each CRD header alongside permissions, showing every named profile declared in the CRD's templates: security context, pod security, resources, rolling update strategy, HPA behavior, PDB configuration, and probes.

```text
    profiles:
      security (container)  restricted   Deployment/web [onReconcile]
      security (pod)        hardened     Deployment/web [onReconcile]
      resources             burst        Deployment/web [onReconcile]
      rolling               safe         Deployment/web [onReconcile]
```

**System-level RBAC** — printed at the bottom, separated into `runtime` (always present) and `gateway` (only when the Katalog requires webhooks or deletion protection). The gateway section includes inline notes explaining why each permission is needed:

```text
runtime
  coordination.k8s.io  leases  get create update
  core                 events  create patch

gateway
  core  secrets     get list watch ...  ← Orkestra provisions and rotates certs —
                                           set TLS_CERT / TLS_KEY in orkestra-deployment
                                           to bring your own
  core  namespaces  get patch            ← labels orkestra-system to activate the
                                           deletion-protection admission scope
```

**Startup order** — if any CRD declares `dependsOn`, a startup order section shows the dependency graph in topological order with conditions:

```text
startup order
  1  database
  2  cache     ← database [healthy]
  3  api       ← database [started] · cache [started]
```

`--full` is the recommended pre-deploy review step: it surfaces everything the Katalog will request from the cluster before a single resource is applied.

---

## Commands that run offline

The following commands require no cluster connection:

| Command | What it does |
|---------|-------------|
| `ork init` | Scaffold a new operator project |
| `ork validate` | Full Pattern validation |
| `ork template` | Render the merged, resolved Katalog |
| `ork simulate` | Reconcile against a fake in-memory cluster |
| `ork notes` | Browse and search template expression library |
| `ork generate` | Generate bundle, CRD, or RBAC manifests |
| `ork plan --bundle` | Diff local Katalog against a local bundle file |

---

## Commands that require a cluster

Only two commands actively connect to and manage a real Kubernetes cluster:

| Command | Why it needs a cluster |
|---------|----------------------|
| `ork run` | Applies CRDs, starts reconcilers, watches resources |
| `ork gate` | Registers webhooks, processes admission requests |

`ork e2e` creates or reuses a kind cluster. `ork plan` (without `--bundle`) reads the cluster's ConfigMap. All other commands are fully local.

---

## Defense in depth

Validation runs at multiple points independently:

1. **Parse time** — strict YAML unmarshaling rejects unknown fields immediately
2. **`ork validate`** — full offline validation before any cluster interaction
3. **`ork simulate`** — exercises the reconcile loop in-memory before running against a real cluster
4. **Admission webhooks** — the Gateway rejects invalid CRs at CREATE/UPDATE time, before etcd storage
5. **Reconcile time** — the runtime re-checks rules on every reconcile as a backstop

A Pattern that passes `ork validate` and `ork simulate` is very unlikely to fail at runtime — the same validation logic runs in all five stages.

---

## Further reading

- **[Admission Control](./03-admission.md)** — deny and warn rules enforced by the webhook
- **[Namespace Protection](./05-namespace-protection.md)** — two-point namespace enforcement
- **[ork plan](../reference/cli/02-plan.md)** — diff a Katalog before applying it
