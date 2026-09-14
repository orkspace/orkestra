# 04 — Type Notes

Type notes inspect and convert the runtime Go types of values in the CR. Because Kubernetes stores everything as JSON, field types depend on how the API server serialized them — these notes let you branch on that safely.

## Why runtime types matter

A CR field declared as `type: object` in the CRD schema can arrive as either a string or a map depending on how the user wrote their YAML. Type notes let a single Katalog handle both:

```yaml
spec:
  schedule: "*/5 * * * *"      # typeOf returns "string"

spec:
  schedule:                     # typeOf returns "map"
    minute: "*/5"
    hour: "*"
```

## Reference

### `typeOf`

Return the runtime type name of any value. The canonical way to branch on field type.

Keywords: type, inspect, runtime, branch, kind, detect

```yaml
# value: "{{ typeOf .spec.schedule }}"
```

| Go type | Returns |
|---------|---------|
| `string` | `"string"` |
| `float64`, `int64`, `int` | `"number"` |
| `bool` | `"bool"` |
| `map[string]interface{}` | `"map"` |
| `[]interface{}` | `"slice"` |
| `nil` | `"null"` |
| anything else | `"unknown"` |

`typeOf` in template expressions returns the same strings as the `operator: typeOf` condition operator — they share the same underlying function.

---

### `typeMap` / `typeList` / `typeString` / `typeNumber` / `typeBool` / `typeNull`

Shorthand predicates — return `true` when the value matches that type. Cleaner than `eq (typeOf .field) "map"` in normalize expressions.

Keywords: type, predicate, check, boolean, map, list, string, number, bool, null

```yaml
normalize:
  spec:
    schedule: >
      {{ if typeMap .spec.schedule }}
        {{ cronFromMap .spec.schedule }}
      {{ else }}
        {{ cronNormalize .spec.schedule }}
      {{ end }}
```

```yaml
# value: "{{ typeList .spec.regions }}"
# spec.regions: [us-east-1, eu-west-1] → true
# spec.regions: "us-east-1"           → false
```

---

### `Empty`

Return `true` when the value is nil, an empty string, an empty slice, or an empty map.

Keywords: type, empty, nil, check, boolean, absent

```yaml
# value: "{{ Empty .spec.annotations }}"
# {}          → true
# nil         → true
# {app: foo}  → false
```

---

### `len`

Return the length of a string, slice, or map. Overrides Go's built-in `len` with one that handles all three types uniformly.

Keywords: type, length, count, string, slice, map, size

```yaml
# value: "{{ len .spec.regions }}"
# ["us-east-1", "eu-west-1"] → 2

# value: "{{ len .spec.schedule }}"
# {minute: "*/5", hour: "*", ...} → 5  (map with 5 fields)

# value: "{{ len .metadata.name }}"
# "my-app" → 6  (string length)
```

---

### `toInt`

Convert any value to `int64`. Truncates floats.

Keywords: type, convert, integer, coerce, number, cast

```yaml
# value: "{{ toInt .spec.replicas }}"
# "3"   → 3
# 3.7   → 3
# true  → 1
# false → 0
```

---

### `toFloat`

Convert any value to `float64`.

Keywords: type, convert, float, coerce, number, cast, decimal

```yaml
# value: "{{ toFloat .spec.threshold }}"
# "0.75" → 0.75
# 3      → 3.0
```

---

### `toBool`

Convert a value to `bool`. Returns an error for unrecognized strings.

Keywords: type, convert, boolean, coerce, parse, cast, bool

| Truthy | Falsy |
|--------|-------|
| `true`, `True`, `TRUE` | `false`, `False`, `FALSE` |
| `1`, `yes`, `YES`, `on`, `ON` | `0`, `no`, `NO`, `off`, `OFF`, `""` |

```yaml
# value: "{{ toBool .spec.enabled }}"
# "yes"  → true
# "no"   → false
# "true" → true
```

---

### `toString`

Convert any value to its string representation. Uses `fmt.Sprintf("%v", v)`.

Keywords: type, convert, string, coerce, format, cast, stringify

```yaml
# value: "{{ toString .spec.replicas }}"
# 3     → "3"
# true  → "true"
# 3.14  → "3.14"
```

---

### `toJson`

Convert any value to its JSON representation. Returns an error for unrecognized types.

Keywords: type, convert, json, serialize, format, encode

```yaml
# value: "{{ toJson .spec }}"    →  `{"replicas":3,"enabled":true}`
```

---

### `toList`

Convert a comma-separated string to a list of trimmed strings. Returns an empty list for empty strings. Essential for dynamic exclusion lists in `serve.config.response` and other places where a comma-separated value needs to be treated as a list.

Keywords: type, convert, list, slice, split, parse, csv

```yaml
# value: "{{ toList .spec.excludeFields }}"
# "a,b,c"   → ["a", "b", "c"]
# "a, b, c" → ["a", "b", "c"]  (spaces trimmed)
# ""        → []
# "foo"     → ["foo"]
```

**Typical usage with `serve.config.response`:**

```yaml
serve:
  config:
    response:
      exclude: '{{ toList (getAnnotation . "platform.myorg.io/exclude-fields") }}'
```

**Usage with notes block:**

```yaml
notes:
  - name: excludeForExternal
    expression: |
      {{ if eq (getLabel . "visibility") "external" }}
        "metadata.managedFields,status.detailed,spec.internalSecrets"
      {{ else }}
        ""
      {{ end }}

serve:
  config:
    response:
      exclude: '{{ toList (excludeForExternal) }}'
```
---

## The `typeOf` + `when:` connection

The same type names that `typeOf` returns in templates are used by the `operator: typeOf` condition in `when:` blocks:

```yaml
when:
  - field: spec.schedule
    operator: typeOf
    value: map       # same string that typeOf returns
```

This symmetry means you can use `typeOf` in status fields to surface the detected type for debugging, then use the `typeOf` condition operator to route reconcile logic — and the values always match.

---

## Quick reference

| Note | Signature | Returns |
|------|-----------|---------|
| `typeOf` | `(v any)` | `string` |
| `typeMap` | `(v any)` | `bool` |
| `typeList` | `(v any)` | `bool` |
| `typeString` | `(v any)` | `bool` |
| `typeNumber` | `(v any)` | `bool` |
| `typeBool` | `(v any)` | `bool` |
| `typeNull` | `(v any)` | `bool` |
| `Empty` | `(v any)` | `bool` |
| `len` | `(v any)` | `int` |
| `toInt` | `(v any)` | `int64` |
| `toFloat` | `(v any)` | `float64` |
| `toBool` | `(v any)` | `bool` |
| `toString` | `(v any)` | `string` |
| `toJson` | `(v any)` | `string` |
| `toList` | `(v any)` | `[]string` |


---

**Next →** [05 — Cron Notes](05-cron.md)
