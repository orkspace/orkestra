# Pod

This declares one Pod to be managed by Orkestra.

Prefer DeploymentTemplateSource for long-running workloads. Deployments manage Pod restarts, rolling updates, and replica sets automatically. Use This only when you need direct, single-instance Pod control.

Example:

```yaml
onCreate:
  pods:
    - name: "{{ .metadata.name }}-worker"
      image: "{{ .spec.workerImage }}"
      port: "9090"
```

---

## Lifecycle

Declare this resource under `onCreate` for an idempotent, one-time create: Orkestra creates it on the first reconcile and leaves it untouched afterward. Set `reconcile: true` on the same entry to also apply it as drift correction on every subsequent reconcile. This is a shorthand for declaring the identical entry under `onReconcile` as well — there's no need to do both.

Declare a resource under `onDelete` to run explicit cleanup before the CR's finalizer is removed. Most resources need no `onDelete` entry — they are garbage-collected automatically through owner references when the CR itself is deleted.

---

## Fields

### `name`

Type: string

Name — Pod name. Default when omitted: "{{ .metadata.name }}-pod"

---

### `image`

Type: string

Image — container image. Required. Static: "busybox:1.35" or Dynamic: "{{ .spec.image }}"

---

### `imagePullSecrets`

Type: list

ImagePullSecrets is an optional list of references to secrets in the same namespace to use for pulling any of the images used by this PodSpec. If specified, these secrets will be passed to individual puller implementations for them to use.

---

### `port`

Type: string

Port — container port as a string. Static: "8080" or Dynamic: "{{ .spec.port }}"

---

### `protocol`

Type: string

Protocol — network protocol for the container port. Accepted values: TCP (default), UDP, SCTP. Omit to use TCP.

---

### `namespace`

Type: string

Namespace — target namespace. Default when omitted: "{{ .metadata.namespace }}"

---

### `labels`

Type: map

Labels — applied to Pod metadata. Values support template expressions.

---

### `annotations`

Type: map

Annotations — applied to Pod metadata. Values support template expressions.

---

### `resources`

Type: object

Resources — CPU and memory requests/limits for the primary container. Set resources.profile for a named preset, or resources.requests/limits for explicit values. Profile and explicit values are mutually exclusive.

```yaml
resources:
  profile: burst
```

```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
```

---

### `nodeSelector`

Type: map

NodeSelector is a selector which must be true for the pod to fit on a node. Selector which must match a node's labels for the pod to be scheduled on that node. More info: [https://kubernetes.io/docs/concepts/configuration/assign-pod-node/](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/)

---

### `serviceAccountName`

Type: string

ServiceAccountName is the name of the ServiceAccount to use to run this pod. More info: [https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/)

---

### `when`

Type: list

Conditions declares the set of runtime predicates that must all evaluate to true for this resource template to be applied during reconciliation.

Each condition inspects a field on the live Custom Resource using dot-notation (e.g. "spec.enabled", "metadata.labels.tier") and compares it against a value using the chosen operator. All conditions in the list are AND‑ed together.

If any condition fails, the resource is skipped for that reconcile cycle. This is not an error — it simply means "do not create/update this resource right now". This enables expressive, data‑driven orchestration such as:

```yaml
when:
  - field: spec.exposePublicly
    equals: "true"
  - field: spec.environment
    prefix: "prod"
```

Conditions allow templates to be selectively activated based on the CR's state, enabling dynamic topologies, feature flags, environment‑specific behavior, and conditional provisioning without writing Go code.

---

### `forEach`

Type: object

ForEach declares dynamic expansion over a list field. When set, one source declaration becomes N declarations — one per list element. .item and .\<as> are available in template expressions within this declaration.

```yaml
forEach:
  field: spec.regions
  as: region
```

---

### `or`

Type: list

Or holds OR conditions — at least one must pass for this resource to be created. Works alongside the existing Conditions (when:) field which uses AND semantics.

```yaml
or:
  - field: spec.tier
    equals: pro
  - field: spec.tier
    equals: enterprise
```

---

### `probes`

Type: object

Probes — startup, liveness, and readiness probe configuration.

```yaml
probes:
  liveness:
    type: http
    path: /healthz
    profile: standard
```

---

### `reconcile`

Type: boolean

Reconcile: true — also apply this declaration as drift correction on every reconcile, not just on create. Equivalent to declaring the same entry under onReconcile. When false (default), only runs on onCreate (idempotent create).

---

### `securityContext`

Type: object

SecurityContext — container-level security settings. Set securityContext.profile for a named preset (baseline, restricted, hardened) or declare individual fields. Profile and explicit fields are mutually exclusive.

```yaml
securityContext:
  profile: restricted
```

---

### `podSecurity`

Type: object

PodSecurity — pod-level security settings applied to the pod spec. Set podSecurity.profile for a named preset or declare individual fields.

```yaml
podSecurity:
  profile: baseline
```

---

### `volumes`

Type: list

Volumes — pod volumes available for mounting into the container.

```yaml
volumes:
  - name: config
    configMap:
      name: myapp-config
```

---

### `volumeMounts`

Type: list

VolumeMounts — mounts for the primary container.

```yaml
volumeMounts:
  - name: config
    mountPath: /etc/myapp
```

---

### `sleep`

Type: string

Sleep injects an artificial delay into the reconcile of this resource. Useful for autoscale testing, latency simulation, and chaos engineering. Accepts extended duration units (s, m, h, d, w, mo, y).

---

### `forceConflict`

Type: boolean

ForceConflict, when true, sets Force: true when applying this resource, taking ownership of conflicting fields instead of returning a conflict error. Overrides the CRD-level ForceConflict setting.

---

## Quick reference

| YAML key | Type |
|---|---|
| `name` | string |
| `image` | string |
| `imagePullSecrets` | list |
| `port` | string |
| `protocol` | string |
| `namespace` | string |
| `labels` | map |
| `annotations` | map |
| `resources` | object |
| `nodeSelector` | map |
| `serviceAccountName` | string |
| `when` | list |
| `forEach` | object |
| `or` | list |
| `probes` | object |
| `reconcile` | boolean |
| `securityContext` | object |
| `podSecurity` | object |
| `volumes` | list |
| `volumeMounts` | list |
| `sleep` | string |
| `forceConflict` | boolean |
