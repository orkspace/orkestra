# HorizontalPodAutoscaler

This declares one HorizontalPodAutoscaler to be managed by Orkestra.

Example:

```yaml
onReconcile:
  hpa:
    - name: "{{ .metadata.name }}-hpa"
      deploymentRef: "{{ .metadata.name }}"
      minReplicas: "{{ .spec.minReplicas }}"
      maxReplicas: "{{ .spec.maxReplicas }}"
      targetCPUUtilizationPercentage: "80"
      forEach:
        field: spec.services
        as: item
```

---

## Lifecycle

Declare this resource under `onCreate` for an idempotent, one-time create: Orkestra creates it on the first reconcile and leaves it untouched afterward. Set `reconcile: true` on the same entry to also apply it as drift correction on every subsequent reconcile. This is a shorthand for declaring the identical entry under `onReconcile` as well — there's no need to do both.

Declare a resource under `onDelete` to run explicit cleanup before the CR's finalizer is removed. Most resources need no `onDelete` entry — they are garbage-collected automatically through owner references when the CR itself is deleted.

---

## Fields

### `name`

Type: string

Name — HPA resource name. Default: "{{ .metadata.name }}-hpa"

---

### `namespace`

Type: string

Namespace — target namespace. Default: CR namespace.

---

### `scaleTargetRef`

Type: object

ScaleTargetRef — the target workload this HPA scales. Supports Deployment, ReplicaSet, StatefulSet, or any scalable resource. Supports template expressions: "{{ .metadata.name }}"

```yaml
scaleTargetRef:
  apiVersion: apps/v1
  kind: Deployment
  name: "{{ .metadata.name }}-deployment"
```

---

### `minReplicas`

Type: string

MinReplicas — minimum replica count as a string. Supports template expressions.

---

### `maxReplicas`

Type: string

MaxReplicas — maximum replica count as a string. Supports template expressions.

---

### `targetCPUUtilizationPercentage`

Type: string

TargetCPUUtilizationPercentage — CPU utilization target (0-100). Supports templates. When behavior.profile is set and this field is empty, the profile provides the default.

---

### `behavior`

Type: object

Behavior — scale-up and scale-down behavior configuration. Set profile for a complete preset, or configure scaleUp/scaleDown explicitly. Profile and explicit fields are mutually exclusive.

```yaml
behavior:
  profile: web
```

```yaml
behavior:
  scaleUp:
    stabilizationWindowSeconds: 0
  scaleDown:
    stabilizationWindowSeconds: 300
```

---

### `labels`

Type: map

Labels applied to HPA metadata. Values support template expressions.

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
  field: spec.services
  as: item
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
| `scaleTargetRef` | object |
| `minReplicas` | string |
| `maxReplicas` | string |
| `targetCPUUtilizationPercentage` | string |
| `behavior` | object |
| `labels` | map |
| `reconcile` | boolean |
| `when` | list |
| `or` | list |
| `forEach` | object |
| `sleep` | string |
| `forceConflict` | boolean |
