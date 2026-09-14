# Conditional Notes

Conditional notes express branching logic inside a single template expression, removing the need for `{{ if }}...{{ else }}...{{ end }}` blocks in field values.

## Reference

| Note | Description |
|------|-------------|
| `ternary` | Return `trueVal` when `condition` is truthy, `falseVal` otherwise. |
| `boolTernary` | Like `ternary` but requires a strict `bool` argument. |
| `boolDefault` | Return the field's boolean value if it is a `bool`, otherwise return `def`. |
| `eqTernary` | Return `trueVal` when `val` equals `target` (string comparison), `falseVal` otherwise. |
| `default` | Return `val` if non-empty, otherwise return `def`. |
| `coalesce` | Return the first non-empty value from a variadic list. |
| `empty` | Return `true` when the value is empty (nil, `""`, `0`, `false`, empty slice, empty map, or `"<no value>"`). |
| `notEmpty` | Return `true` when the value is non-empty. |

## Examples

```yaml
# ternary
# value: "{{ ternary .spec.debug \"debug\" \"info\" }}"
# spec.debug=true  → "debug"
# spec.debug=false → "info"

# value: "{{ ternary .spec.image \"custom\" \"default\" }}"
# spec.image set   → "custom"
# spec.image empty → "default"

# boolTernary
# value: "{{ boolTernary .spec.suspend \"Suspended\" \"Active\" }}"
# spec.suspend=true  → "Suspended"
# spec.suspend=false → "Active"

# boolDefault
# value: "{{ boolTernary (boolDefault .spec.suspend false) \"Suspended\" \"Active\" }}"
# spec.suspend absent → boolDefault returns false → "Active"
# spec.suspend=true   → boolDefault returns true  → "Suspended"

# eqTernary
# value: '{{ eqTernary .cross.db.found "true" "ready" "waiting" }}'
# found="true"  → "ready"
# found="false" → "waiting"

# value: '{{ eqTernary .spec.mode "production" "strict" "permissive" }}'
# mode="production" → "strict"
# mode="staging"    → "permissive"

# default
# value: "{{ default .spec.replicas 2 }}"
# spec.replicas absent  → 2
# spec.replicas=0       → 2   (zero is Empty()
# spec.replicas=5       → 5

# value: "{{ default .spec.logLevel \"info\" }}"
# spec.logLevel absent  → "info"
# spec.logLevel="debug" → "debug"

# coalesce
# value: "{{ coalesce .spec.image .spec.defaultImage \"nginx:latest\" }}"
# spec.image set        → spec.image
# spec.image absent, spec.defaultImage set → spec.defaultImage
# both absent           → "nginx:latest"

# empty
# Use in a conditional:
# value: "{{ if empty .spec.image }}nginx:latest{{ else }}{{ .spec.image }}{{ end }}"

# notEmpty
# Use in when: conditions via template expression:
# value: "{{ notEmpty .spec.image }}"
```
