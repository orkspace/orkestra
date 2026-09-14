# CronJob

This declares one CronJob to be managed by Orkestra.

Example:

```yaml
onCreate:
  cronJobs:
    - name: "{{ .metadata.name }}-sync"
      schedule: "{{ .spec.syncSchedule }}"
      image: "{{ .spec.syncImage }}"
      command: ["sh", "-c", "sync.sh"]
```

---

## Lifecycle

Declare this resource under `onCreate` for an idempotent, one-time create: Orkestra creates it on the first reconcile and leaves it untouched afterward. Set `reconcile: true` on the same entry to also apply it as drift correction on every subsequent reconcile. This is a shorthand for declaring the identical entry under `onReconcile` as well — there's no need to do both.

Declare a resource under `onDelete` to run explicit cleanup before the CR's finalizer is removed. Most resources need no `onDelete` entry — they are garbage-collected automatically through owner references when the CR itself is deleted.

---

## Fields

### `name`

Type: string

Name — CronJob name. Default when omitted: "{{ .metadata.name }}-cronjob"

---

### `schedule`

Type: string

Schedule — cron schedule expression. Required. Static: "0 \* \* \* \*" (every hour) Dynamic: "{{ .spec.schedule }}"

---

### `image`

Type: string

Image — container image. Required.

---

### `imagePullSecrets`

Type: list

ImagePullSecrets is an optional list of references to secrets in the same namespace to use for pulling any of the images used by this PodSpec. If specified, these secrets will be passed to individual puller implementations for them to use.

---

### `command`

Type: list

Command — container entrypoint. Each element supports template expressions.

---

### `args`

Type: list

Args — container arguments. Each element supports template expressions.

---

### `namespace`

Type: string

Namespace — target namespace. Default when omitted: "{{ .metadata.namespace }}"

---

### `labels`

Type: map

Labels — applied to CronJob metadata. Values support template expressions.

---

### `nodeSelector`

Type: map

NodeSelector is a selector which must be true for the pod to fit on a node. Selector which must match a node's labels for the pod to be scheduled on that node. More info: [https://kubernetes.io/docs/concepts/configuration/assign-pod-node/](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/) +optional +mapType=atomic

---

### `serviceAccountName`

Type: string

ServiceAccountName is the name of the ServiceAccount to use to run this pod. More info: [https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/) +optional

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

### `suspend`

Type: string

Suspend — when "true", the schedule stops firing new Jobs; existing runs are unaffected. Accepts a template expression. Default: "false".

---

### `successfulJobsHistoryLimit`

Type: string

SuccessfulJobsHistoryLimit — number of completed, successful Jobs to keep around for inspection. Default: 3.

---

### `failedJobsHistoryLimit`

Type: string

FailedJobsHistoryLimit — number of failed Jobs to keep around for inspection. Default: 1.

---

### `concurrencyPolicy`

Type: string

ConcurrencyPolicy — how to handle a scheduled run that overlaps with a still-running previous Job. Accepted values: allow (default — runs concurrently), forbid (skip the new run), replace (cancel the running Job and start the new one).

---

### `startingDeadlineSeconds`

Type: string

StartingDeadlineSeconds — if a scheduled run is missed (e.g. the controller was down) by more than this many seconds, it is counted as failed instead of started late. Omit for no deadline.

---

### `resources`

Type: object

Resources — CPU and memory requests/limits for the container. Set resources.profile for a named preset, or resources.requests/limits for explicit values. Profile and explicit values are mutually exclusive.

```yaml
resources:
  profile: burst
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

### `workingDirectory`

Type: string

WorkingDirectory sets the container's working directory (container.WorkingDir). Useful for Git-backed pipelines where build/test commands must run inside a checked-out repository path.

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
| `schedule` | string |
| `image` | string |
| `imagePullSecrets` | list |
| `command` | list |
| `args` | list |
| `namespace` | string |
| `labels` | map |
| `nodeSelector` | map |
| `serviceAccountName` | string |
| `reconcile` | boolean |
| `when` | list |
| `suspend` | string |
| `successfulJobsHistoryLimit` | string |
| `failedJobsHistoryLimit` | string |
| `concurrencyPolicy` | string |
| `startingDeadlineSeconds` | string |
| `resources` | object |
| `forEach` | object |
| `or` | list |
| `workingDirectory` | string |
| `securityContext` | object |
| `podSecurity` | object |
| `sleep` | string |
| `forceConflict` | boolean |
