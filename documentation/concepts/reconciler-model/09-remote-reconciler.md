# Remote Reconciler

The remote reconciler is a first-class Orkestra reconciler model where the reconcile logic lives in a separate HTTP service — in any language, running anywhere. Orkestra owns every operator concern except the decision-making: the queue, the backoff, the informer, the SSA apply, the owner references, the RBAC, the health tracking, the events, the annotation stamping. The service owns the logic.

!!! tip "The one-sentence version"
    Point Orkestra at an HTTP endpoint. Orkestra handles Kubernetes. The service handles your business logic. The service doesn't need to know Kubernetes exists.

---

## How it works

After every watch event, Orkestra POSTs a `PreparedRequest` to the configured endpoint — the current CR as JSON, enriched with resolver context. The service returns a `RemoteReconcileResult` — resources to apply, status to patch, and whether to requeue. Orkestra takes it from there.

```text
watch event
     │
     ▼
Orkestra: gate evaluation, dedup, rate-limit
     │
     ▼
POST /reconcile
     │
     └── body: { key, gvk, object, prepared }
     │
     ▼
Your service: reads CR → decides resources and status
     │
     └── 200 OK: { result, status, resources }
     │
     ▼
Orkestra: SSA apply with owner refs, status patch, health update
```

The service receives a structured request. It returns a structured response. Everything between the informer and the cluster API is Orkestra's.

---

## Declaring it

```yaml
operatorBox:
  reconcile:
    default: false
    remote:
      endpoint: "http://my-reconciler.internal/reconcile"
      timeout: 15s
      managedResources:
        - group: apps
          plural: deployments
        - group: ""
          plural: services
```

`default: false` is required — it signals that the runtime should not start a built-in reconciler for this CRD. See [reconcile.remote schema reference](../../reference/schema/02-katalog/31-reconcile-remote.md) for all fields.

---

## What the service receives

Orkestra POSTs a JSON body to `endpoint` on every reconcile:

```json
{
  "key":      "default/my-webapp",
  "gvk":      { "Group": "myorg.io", "Version": "v1", "Kind": "MyApp" },
  "object":   { "apiVersion": "myorg.io/v1", "kind": "MyApp", ... },
  "prepared": { ... }
}
```

`object` is the full CR as-is from the informer cache. `prepared` carries Orkestra's enriched resolver context — cross-CRD state, sentinel values, and validation results. A service that only needs the CR can ignore `prepared` entirely.

---

## What the service returns

The service responds with `200 OK` and a JSON body:

```json
{
  "result":  "ok",
  "status":  { "phase": "Ready" },
  "resources": [
    { "type": "deployment", "fields": { "name": "my-webapp", "image": "nginx:latest", "replicas": 2 } },
    { "type": "service",    "fields": { "name": "my-webapp-svc", "port": 80, "targetPort": 8080 } }
  ]
}
```

| Field | Description |
|---|---|
| `result` | `"ok"` — done. `"requeue"` — re-enqueue after `requeueAfter`. `"error"` — failure, triggers backoff. |
| `requeueAfter` | Go duration string, used when `result` is `"requeue"` (e.g. `"60s"`). |
| `error` | Human-readable message. Non-empty sets `Ready=False` and triggers backoff. |
| `status` | Map of fields to patch onto `.status`. Omit or `null` to leave status unchanged. |
| `resources` | Resources to apply. Each entry is an **intent form** (`type` + `fields`) or a **full Kubernetes object** (`apiVersion`, `kind`, `metadata.name`). Only types declared in `managedResources` are accepted. |

The intent form lets the service name a type and return a flat field map — Orkestra constructs the full object. When more control is needed, return a complete Kubernetes object instead. Either way, undeclared types cause an immediate error before any resource is applied.

See the [schema reference](../../reference/schema/02-katalog/31-reconcile-remote.md#intent-form) for supported intent types and field maps.

---

## What Orkestra manages on the service's behalf

The service writes no Kubernetes code. Orkestra provides:

- **Informer and queue** — the service never watches the cluster; Orkestra delivers events
- **Worker pool and backoff** — concurrency, retry intervals, and error thresholds are declared in the Katalog
- **SSA apply** — resources in the response are applied using server-side apply with Orkestra's field manager
- **Owner references** — same-namespace resources receive owner references automatically; the service does not set them
- **RBAC** — `ClusterRole` and `ClusterRoleBinding` are generated from `managedResources` at startup
- **Health tracking** — success rates and degraded thresholds are tracked per-CRD in the kordinator
- **Health and metrics annotations** — stamped onto the CR after every reconcile; readable by future gate conditions
- **Events** — Kubernetes events are emitted on reconcile success and failure

The service does none of this. It reads a CR; it returns what to create.

---

## Auth

Use `auth:` when the endpoint requires a credential:

```yaml
remote:
  endpoint: "https://my-reconciler.internal/reconcile"
  auth:
    secretRef:
      name: reconciler-token
      key: token
    header: "Authorization"   # default — injected as "Bearer <value>"
```

Or from an environment variable in the operator pod:

```yaml
auth:
  env: RECONCILER_API_KEY
  header: "X-Api-Key"
```

Orkestra generates RBAC to read the Secret automatically. A static `name` value is scoped to `resourceNames:` in the generated `ClusterRole`.

---

## The "server doesn't need cluster access" point

A traditional operator uses a kubeconfig, registers a scheme, sets up an informer cache, writes a controller loop. The remote reconciler inverts this: **the service doesn't negotiate with Kubernetes at all.** No kubeconfig. No SDK. No informer. No scheme registration. It receives a plain JSON HTTP request and returns a plain JSON response.

This means:

- A bash script that reads JSON from stdin and writes JSON to stdout can be a reconciler.
- A Python service that already manages business objects can handle reconcile calls on the side.
- A payments API, a provisioning service, a data pipeline — any HTTP-capable process can own operator logic.
- Teams that are not Go engineers can write Kubernetes operators without learning the Kubernetes ecosystem.

The only Kubernetes concept the service needs to understand is the shape of the CR it manages — which it likely already knows, since it is probably the team that defined the CRD.

---

## Things to know

| | |
|---|---|
| `ork simulate` | Remote reconcilers are skipped in simulation — `ork simulate` prints a note and omits the CRD. Use `ork e2e` to test against a live cluster. |
| `endpoint` templates | Template expressions are supported and evaluated per-reconcile against the CR's resolver context: `"http://{{ .metadata.namespace }}-svc/reconcile"`. |
| `managedResources` | Drives RBAC generation and SSA validation. Types not listed are rejected before any apply — declare everything the service may return. |
| `type: grpc` | Reserved for a future release. `http` is the only supported value today. |

---

## Where to go next

- [reconcile.remote schema reference](../../reference/schema/02-katalog/31-reconcile-remote.md) — all fields, auth options, RBAC details
- [Reconciler models](index.md) — how the remote model compares to Generic Reconciler and your own `Reconcile()`
- [Simulate limitations](../simulate/06-limitations.md) — what is skipped in simulation
