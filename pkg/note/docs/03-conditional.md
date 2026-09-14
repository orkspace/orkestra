# 03 — Conditional Notes

Conditional notes express branching logic inside a single template expression, removing the need for `{{ if }}...{{ else }}...{{ end }}` blocks in field values.

## Reference

### `ternary`

Return `trueVal` when `condition` is truthy, `falseVal` otherwise. Condition truthiness follows Go rules: non-empty string, non-zero number, non-empty slice/map, `true` bool.

Keywords: conditional, branch, ternary, if-else, logic, truthy

```yaml
# value: "{{ ternary .spec.debug \"debug\" \"info\" }}"
# spec.debug=true  → "debug"
# spec.debug=false → "info"

# value: "{{ ternary .spec.image \"custom\" \"default\" }}"
# spec.image set   → "custom"
# spec.image empty → "default"
```

---

### `boolTernary`

Like `ternary` but requires a strict `bool` argument. Use this when the field is declared as a boolean in the CRD schema — it avoids the truthiness ambiguity of `ternary` for values like `0`, `""`, or `[]`.

Keywords: conditional, branch, boolean, ternary, if-else, strict

```yaml
# value: "{{ boolTernary .spec.suspend \"Suspended\" \"Active\" }}"
# spec.suspend=true  → "Suspended"
# spec.suspend=false → "Active"
```

`boolTernary` is the correct choice for Kubernetes boolean fields like `spec.suspend`, `spec.enabled`, `spec.paused`.

---

### `boolDefault`

Return the field's boolean value if it is a `bool`, otherwise return `def`. Useful when a boolean field may be absent (nil) in the CR.

Keywords: conditional, boolean, default, fallback, absent, nil

```yaml
# value: "{{ boolTernary (boolDefault .spec.suspend false) \"Suspended\" \"Active\" }}"
# spec.suspend absent → boolDefault returns false → "Active"
# spec.suspend=true   → boolDefault returns true  → "Suspended"
```

---

### `eqTernary`

Return `trueVal` when `val` equals `target` (string comparison), `falseVal` otherwise. Shorthand for the `boolTernary (eq val target) trueVal falseVal` pattern — useful when branching on a string field value such as a status string, a mode flag, or a cross-CRD `found` result.

Keywords: conditional, branch, equality, string, ternary, compare, match

```yaml
# value: '{{ eqTernary .cross.db.found "true" "ready" "waiting" }}'
# found="true"  → "ready"
# found="false" → "waiting"

# value: '{{ eqTernary .spec.mode "production" "strict" "permissive" }}'
# mode="production" → "strict"
# mode="staging"    → "permissive"
```

---

### `default`

Return `val` if non-empty, otherwise return `def`. "Empty" means nil, `""`, `0`, `false`, empty slice, or empty map.

Keywords: conditional, default, fallback, absent, empty, nil

```yaml
# value: "{{ default .spec.replicas 2 }}"
# spec.replicas absent  → 2
# spec.replicas=0       → 2   (zero is Empty()
# spec.replicas=5       → 5

# value: "{{ default .spec.logLevel \"info\" }}"
# spec.logLevel absent  → "info"
# spec.logLevel="debug" → "debug"
```

---

### `coalesce`

Return the first non-empty value from a variadic list. Useful when a field can come from multiple sources with a final fallback.

Keywords: conditional, coalesce, fallback, first, multiple, chain

```yaml
# value: "{{ coalesce .spec.image .spec.defaultImage \"nginx:latest\" }}"
# spec.image set        → spec.image
# spec.image absent, spec.defaultImage set → spec.defaultImage
# both absent           → "nginx:latest"
```

---

### `empty`

Return `true` when the value is empty (nil, `""`, `0`, `false`, empty slice, empty map, or `"<no value>"`).

Keywords: conditional, empty, check, boolean, nil, absent

```yaml
# Use in a conditional:
# value: "{{ if empty .spec.image }}nginx:latest{{ else }}{{ .spec.image }}{{ end }}"
```

Equivalent to `not notEmpty`.

---

### `notEmpty`

Return `true` when the value is non-empty. The inverse of `empty`.

Keywords: conditional, empty, check, boolean, present, exists

```yaml
# Use in when: conditions via template expression:
# value: "{{ notEmpty .spec.image }}"
```

---

## Choosing between `ternary` and `boolTernary`

| Situation | Use |
|-----------|-----|
| Field is declared `type: boolean` in CRD schema | `boolTernary` |
| Field may be absent — pair with `boolDefault` | `boolTernary (boolDefault .spec.field false)` |
| Condition is a string, number, or presence check | `ternary` |
| Multiple fallbacks | `coalesce` |
| Simple "use this or that default" | `default` |

---

## Quick reference

| Note | Signature | Returns |
|------|-----------|---------|
| `ternary` | `(condition, trueVal, falseVal any)` | `any` |
| `boolTernary` | `(condition bool, trueVal, falseVal any)` | `any` |
| `boolDefault` | `(v any, def bool)` | `bool` |
| `default` | `(val, def any)` | `any` |
| `coalesce` | `(vals ...any)` | `any` |
| `empty` | `(v any)` | `bool` |
| `notEmpty` | `(v any)` | `bool` |

---

**Next →** [04 — Type Notes](04-types.md)
