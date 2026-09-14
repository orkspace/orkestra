# RoleBinding

This declares one RoleBinding to be managed by Orkestra.

Example:

```yaml
onCreate:
  roleBindings:
    - name: "{{ .metadata.name }}-rolebinding"
      namespace: "{{ .metadata.name }}-ns"
      roleRef:
        name: "{{ .metadata.name }}-role"
      subjects:
        - kind: ServiceAccount
          name: "{{ .metadata.name }}-sa"
          namespace: "{{ .metadata.name }}-ns"
```

---

## Lifecycle

Declare this resource under `onCreate` for an idempotent, one-time create: Orkestra creates it on the first reconcile and leaves it untouched afterward. Set `reconcile: true` on the same entry to also apply it as drift correction on every subsequent reconcile. This is a shorthand for declaring the identical entry under `onReconcile` as well — there's no need to do both.

Declare a resource under `onDelete` to run explicit cleanup before the CR's finalizer is removed. Most resources need no `onDelete` entry — they are garbage-collected automatically through owner references when the CR itself is deleted.

---

## Fields

### `name`

Type: string

Name — RoleBinding name. Default when omitted: "{{ .metadata.name }}-rolebinding"

---

### `namespace`

Type: string

Namespace — target namespace. Default when omitted: "{{ .metadata.namespace }}"

---

### `labels`

Type: map

Labels — applied to RoleBinding metadata. Values support template expressions.

---

### `roleRef`

Type: object

RoleRef — the Role or ClusterRole this binding grants. Required. kind defaults to "Role" when omitted; set it to "ClusterRole" to bind a cluster-scoped role instead.

```yaml
roleRef:
  name: "{{ .metadata.name }}-role"
  kind: Role
```

---

### `subjects`

Type: list

Subjects — the users, groups, or ServiceAccounts this binding grants the role to. Required: at least one subject.

```yaml
subjects:
  - kind: ServiceAccount
    name: "{{ .metadata.name }}-sa"
    namespace: "{{ .metadata.namespace }}"
```

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

### `reconcile`

Type: boolean

Reconcile: true — also apply this declaration as drift correction on every reconcile. Equivalent to declaring the same entry under both onCreate and onReconcile. When false (default), only runs on onCreate (idempotent create).

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
| `labels` | map |
| `roleRef` | object |
| `subjects` | list |
| `when` | list |
| `reconcile` | boolean |
| `forEach` | object |
| `or` | list |
| `sleep` | string |
| `forceConflict` | boolean |
