# Condition-Based Deletion

`onDelete:`, `onCreate:`, and `onReconcile:` blocks support `when:` and `or:` conditions at the block level. When conditions are declared, the entire block is skipped unless they pass.

This is distinct from per-resource conditions (also `when:`/`or:` but on individual resource entries). Block-level conditions gate everything in the block at once.

---

## YAML

```yaml
onDelete:
  when:
    - field: .status.phase
      equals: Ready
  jobs:
    - name: "{{ .metadata.name }}-drain"
  deployments:
    - name: "{{ .metadata.name }}"
```

The `when:` block is evaluated before any resource in `onDelete:` runs. If the condition is false, the entire block is skipped and the finalizer is removed immediately.

---

## Condition semantics

`when:` uses AND semantics — all conditions must pass.

`or:` uses OR semantics — at least one condition must pass.

Both can be combined: the block runs when `when:` passes **or** any `or:` condition passes.

```yaml
onDelete:
  when:
    - field: .status.phase
      equals: Ready
  or:
    - field: .status.phase
      equals: Degraded
  jobs:
    - name: "{{ .metadata.name }}-drain"
```

---

## Ordered deletion with per-group conditions

When `ordered: true`, each group in `groups:` is also a full `HookTemplates` block and can carry its own `when:`/`or:`. A group whose conditions are not met is skipped; the sequence continues with the next group.

```yaml
onDelete:
  ordered: true
  timeout: 10m
  groups:
    # Always runs - no conditions
    - jobs:
        - name: "{{ .metadata.name }}-drain"

    # Only runs if the drain job left data behind
    - deployments:
        - name: "{{ .metadata.name }}"
      when:
        - field: .status.drainState
          equals: partial

    # Always runs — final cleanup
    - secrets:
        - name: "{{ .metadata.name }}-credentials"
```

---

## Block-level vs per-resource conditions

| Level | What it gates | Where declared |
|---|---|---|
| Block | Entire `onDelete:`/`onCreate:`/`onReconcile:` | `when:`/`or:` on the block |
| Resource | A single resource entry | `when:`/`or:` on the resource |

Both levels can be used together. A resource whose block condition passes but whose own condition fails is still skipped.

---

## When to use

Use block-level conditions when the decision applies to the whole lifecycle event — "skip cleanup entirely if the CR never finished provisioning", or "skip onCreate if this is a read-only replica". Use per-resource conditions when individual resources have independent eligibility.
