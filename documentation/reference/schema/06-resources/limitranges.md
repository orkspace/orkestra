# LimitRange

This declares one LimitRange to be managed by Orkestra.

Usage patterns:

1. Container defaults:

```yaml
onCreate:
  limitRanges:
    - name: "{{ .metadata.name }}-limits"
      limits:
        - type: Container
          default:
            cpu: 500m
            memory: 512Mi
          defaultRequest:
            cpu: 250m
            memory: 256Mi
      reconcile: true
```

2. Environment-sized limits with template expressions:

```yaml
onCreate:
  limitRanges:
    - name: "{{ .metadata.name }}-limits"
      limits:
        - type: Container
          default:
            cpu: '{{ if eq .spec.env "production" }}500m{{ else }}200m{{ end }}'
            memory: '{{ if eq .spec.env "production" }}512Mi{{ else }}256Mi{{ end }}'
```

3. Copy from existing LimitRange:

```yaml
onCreate:
  limitRanges:
    - name: team-limits
      fromLimitRange: org-default-limits
      fromNamespace: platform
```

4. Copy to multiple namespaces:

```yaml
onCreate:
  limitRanges:
    - name: team-limits
      fromLimitRange: org-default-limits
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

Name — LimitRange name.

---

### `namespace`

Type: string

Namespace — target namespace. Default: "{{ .metadata.namespace }}"

---

### `toNamespaces`

Type: list

ToNamespaces — create one copy in each listed namespace. Each element supports template expressions.

---

### `fromLimitRange`

Type: string

FromLimitRange — name of an existing LimitRange to copy from. When set, Orkestra reads this LimitRange at reconcile time and copies its limits.

---

### `fromNamespace`

Type: string

FromNamespace — namespace where FromLimitRange lives. Default: same namespace as the CR.

---

### `profile`

Type: string

Profile — named LimitRange preset. Expands into a Limits list at reconcile time. Mutually exclusive with Limits — set one or the other, not both.

---

### `limits`

Type: list

Limits — the list of limit range items. Each item applies to one type: Container, Pod, or PersistentVolumeClaim.

---

### `labels`

Type: map

Labels — applied to LimitRange metadata.

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
| `fromLimitRange` | string |
| `fromNamespace` | string |
| `profile` | string |
| `limits` | list |
| `labels` | map |
| `when` | list |
| `or` | list |
| `reconcile` | boolean |
| `forEach` | object |
| `sleep` | string |
| `forceConflict` | boolean |
