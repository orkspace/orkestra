# StatefulSet

This declares one StatefulSet to be managed by Orkestra.

Example:

```yaml
onCreate:
  statefulSets:
    - name: "{{ .metadata.name }}-db"
      image: postgres:16
      replicas: "3"
      port: "5432"
      volumeClaimTemplates:
        - storageClass: standard
          storageSize: 10Gi
          mountPath: /var/lib/postgresql/data
```

---

## Lifecycle

Declare this resource under `onCreate` for an idempotent, one-time create: Orkestra creates it on the first reconcile and leaves it untouched afterward. Set `reconcile: true` on the same entry to also apply it as drift correction on every subsequent reconcile. This is a shorthand for declaring the identical entry under `onReconcile` as well — there's no need to do both.

Declare a resource under `onDelete` to run explicit cleanup before the CR's finalizer is removed. Most resources need no `onDelete` entry — they are garbage-collected automatically through owner references when the CR itself is deleted.

---

## Fields

### `name`

Type: string

Name — StatefulSet name. Default: "{{ .metadata.name }}".

---

### `namespace`

Type: string

Namespace — target namespace. Default: CR namespace.

---

### `image`

Type: string

Image — container image. Required.

---

### `imagePullSecrets`

Type: list

ImagePullSecrets is an optional list of references to secrets in the same namespace to use for pulling any of the images used by this PodSpec. If specified, these secrets will be passed to individual puller implementations for them to use.

---

### `tag`

Type: string

Tag — image tag. Default: "latest".

---

### `replicas`

Type: string

Replicas — number of pod replicas. Default: "1".

---

### `port`

Type: string

Port — container port. "0" or empty means no port exposed.

---

### `protocol`

Type: string

Protocol — network protocol for the container port. Accepted values: TCP (default), UDP, SCTP. Omit to use TCP.

---

### `serviceName`

Type: string

ServiceName — name of the headless Service governing the StatefulSet. Default: same as Name.

---

### `volumeClaimTemplates`

Type: list

VolumeClaimTemplates — one or more PVC templates; each pod gets its own volume per entry.

```yaml
volumeClaimTemplates:
  - storageClass: standard
    storageSize: 10Gi
    mountPath: /var/lib/postgresql/data
```

---

### `nodeSelector`

Type: map

NodeSelector is a selector which must be true for the pod to fit on a node. Selector which must match a node's labels for the pod to be scheduled on that node. More info: [https://kubernetes.io/docs/concepts/configuration/assign-pod-node/](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/) +optional +mapType=atomic

---

### `serviceAccountName`

Type: string

ServiceAccountName is the name of the ServiceAccount to use to run this pod. More info: [https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/) +optional

---

### `labels`

Type: map

Labels — applied to the StatefulSet ObjectMeta and the pod template. Label values support template expressions.

---

### `annotations`

Type: map

Annotations — applied to the StatefulSet ObjectMeta only. Annotation values support template expressions.

---

### `env`

Type: list

Env — environment variables for the primary container, in Kubernetes-native list format. Each entry has a name and either a value or a valueFrom source. If omitted, no environment variables are added.

```yaml
env:
  - name: LOG_LEVEL
    value: info
  - name: API_KEY
    valueFrom:
      secretKeyRef:
        name: myapp-secrets
        key: api-key
```

---

### `envFrom`

Type: object

EnvFrom — bulk-load environment variables from Secrets and/or ConfigMaps into the primary container, in addition to any individual entries in env. Each secretRef/configMapRef entry names an existing Secret or ConfigMap; every key in it becomes an environment variable.

```yaml
envFrom:
  secretRef:
    - name: myapp-secrets
  configMapRef:
    - name: myapp-config
```

---

### `resources`

Type: object

Resources — CPU and memory requests/limits for the primary container. Set resources.profile for a named preset, or resources.requests/limits for explicit values. Profile and explicit values are mutually exclusive.

```yaml
resources:
  profile: burst
```

---

### `reconcile`

Type: boolean

Reconcile: true — also apply this declaration as drift correction on every reconcile. Equivalent to declaring the same entry under both onCreate and onReconcile. When false (default), only runs on onCreate (idempotent create).

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

### `forEach`

Type: object

ForEach declares dynamic expansion over a list field. When set, one source declaration becomes N declarations — one per list element. .item and .\<as> are available in template expressions within this declaration.

```yaml
forEach:
  field: spec.regions
  as: region
```

---

### `autoscale`

Type: object

Autoscale declares workload autoscaling behaviour for this StatefulSet. When set, the reconciler evaluates scale-up and scale-down conditions on every reconcile and patches spec.replicas when conditions pass and cooldown has elapsed.

```yaml
autoscale:
  min: 2
  max: 10
  cooldown: 2m
  scaleUp:
    conditions:
      when:
        - field: "{{ promAboveThreshold \"cpu_usage\" 80 }}"
          equals: "true"
    increment: 1
  scaleDown:
    conditions:
      when:
        - field: "{{ promBelowThreshold \"cpu_usage\" 20 }}"
          equals: "true"
    decrement: 1
```

---

### `probes`

Type: object

Probes — startup, liveness, and readiness probe configuration.

```yaml
probes:
  liveness:
    type: tcp
    profile: standard
```

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

### `rollingUpdate`

Type: object

RollingUpdate — rolling update strategy for this StatefulSet. Set rollingUpdate.profile for a named preset (safe, fast, blue-green), or declare maxSurge/maxUnavailable explicitly.

```yaml
rollingUpdate:
  profile: safe
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
| `namespace` | string |
| `image` | string |
| `imagePullSecrets` | list |
| `tag` | string |
| `replicas` | string |
| `port` | string |
| `protocol` | string |
| `serviceName` | string |
| `volumeClaimTemplates` | list |
| `nodeSelector` | map |
| `serviceAccountName` | string |
| `labels` | map |
| `annotations` | map |
| `env` | list |
| `envFrom` | object |
| `resources` | object |
| `reconcile` | boolean |
| `when` | list |
| `or` | list |
| `forEach` | object |
| `autoscale` | object |
| `probes` | object |
| `securityContext` | object |
| `podSecurity` | object |
| `rollingUpdate` | object |
| `volumes` | list |
| `volumeMounts` | list |
| `sleep` | string |
| `forceConflict` | boolean |
