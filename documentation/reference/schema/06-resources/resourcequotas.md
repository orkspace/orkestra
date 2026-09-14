# ResourceQuota

This declares one ResourceQuota to be managed by Orkestra.

Usage patterns:

1. Inline quota:

```yaml
onCreate:
  resourceQuotas:
    - name: "{{ .metadata.name }}-quota"
      hard:
        cpu: "4"
        memory: 8Gi
        pods: "20"
      reconcile: true
```

2. Environment-sized quota with template expressions:

```yaml
onCreate:
  resourceQuotas:
    - name: "{{ .metadata.name }}-quota"
      hard:
        cpu: '{{ if eq .spec.env "production" }}16{{ else }}2{{ end }}'
        memory: '{{ if eq .spec.env "production" }}32Gi{{ else }}4Gi{{ end }}'
```

3. Copy from existing ResourceQuota:

```yaml
onCreate:
  resourceQuotas:
    - name: team-quota
      fromResourceQuota: org-default-quota
      fromNamespace: platform
```

4. Copy to multiple namespaces:

```yaml
onCreate:
  resourceQuotas:
    - name: team-quota
      fromResourceQuota: org-default-quota
      fromNamespace: platform
      toNamespaces:
        - "{{ .metadata.namespace }}"
        - staging
```

---

## Lifecycle

Declare this resource under `onCreate` for an idempotent, one-time create: Orkestra creates it on the first reconcile and leaves it untouched afterward. Set `reconcile: true` on the same entry to also apply it as drift correction on every subsequent reconcile. This is a shorthand for declaring the identical entry under `onReconcile` as well — there's no need to do both.

Declare a resource under `onDelete` to run explicit cleanup before the CR's finalizer is removed. Most resources need no `onDelete` entry — they are garbage-collected automatically through owner references when the CR itself is deleted.

---

## Fields

### `name`

Type: string

Name — ResourceQuota name.

---

### `namespace`

Type: string

Namespace — target namespace. Default: "{{ .metadata.namespace }}"

---

### `toNamespaces`

Type: list

ToNamespaces — create one copy in each listed namespace. Each element supports template expressions.

---

### `fromResourceQuota`

Type: string

FromResourceQuota — name of an existing ResourceQuota to copy from. When set, Orkestra reads this ResourceQuota at reconcile time and copies its hard limits.

---

### `fromNamespace`

Type: string

FromNamespace — namespace where FromResourceQuota lives. Default: same namespace as the CR.

---

### `hard`

Type: map

Hard — resource limits. Keys are Kubernetes resource names (cpu, memory, pods, etc.). Values are resource quantities. Supports template expressions. See: [https://kubernetes.io/docs/concepts/policy/resource-quotas/#compute-resource-quota](https://kubernetes.io/docs/concepts/policy/resource-quotas/#compute-resource-quota)

---

### `labels`

Type: map

Labels — applied to ResourceQuota metadata.

---

### `when`

Type: list

Conditions (when:) — all must pass for this resource to be applied.

---

### `or`

Type: list

Or — at least one must pass.

---

### `reconcile`

Type: boolean

Reconcile: true — sync on every reconcile (drift correction).

---

### `forEach`

Type: object

ForEach — expand this entry once per item in a list or map field.

---

### `profile`

Type: string

Profile — named resource quota preset. Expands into hard limits. Allowed values: small, medium, large, xlarge. Mutually exclusive with Hard — set one or the other, not both.

---

### `sleep`

Type: string

Sleep injects an artificial delay. Accepts extended duration units (s, m, h, d, w, mo, y).

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
| `toNamespaces` | list |
| `fromResourceQuota` | string |
| `fromNamespace` | string |
| `hard` | map |
| `labels` | map |
| `when` | list |
| `or` | list |
| `reconcile` | boolean |
| `forEach` | object |
| `profile` | string |
| `sleep` | string |
| `forceConflict` | boolean |
