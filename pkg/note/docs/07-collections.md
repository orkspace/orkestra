# 07 — Collection Notes

Collection notes work with lists and maps from the CR spec. They cover both the `listMap` family (typed access to `[]interface{}` and `map[string]interface{}`) and the `as` family (type coercion for mixed-type inputs).

## listMap notes

These notes expect Go's native unstructured types (`[]interface{}` for lists, `map[string]interface{}` for maps). They return safe zero values instead of panicking when the input is the wrong type.

### `listHas`

Check whether a list contains a value. Comparison is by `==`.

Keywords: list, collection, contains, check, slice, boolean, membership

```yaml
# when:
#   - field: spec.regions
#     operator: typeOf
#     value: slice
# value: "{{ listHas .spec.regions \"us-east-1\" }}"
# ["us-east-1", "eu-west-1"] → true
# ["eu-west-1"]              → false
```

---

### `listGet`

Return the element at a given index. Returns `nil` for out-of-range indices — no panic.

Keywords: list, collection, get, index, slice, access, element

```yaml
# value: "{{ listGet .spec.regions 0 }}"
# ["us-east-1", "eu-west-1"] → "us-east-1"
# []                         → nil (renders as "")
```

---

### `listLen`

Return the number of elements in a list. Returns `0` for non-lists.

Keywords: list, collection, length, count, slice, size

```yaml
# value: "{{ listLen .spec.regions }}"
# ["us-east-1", "eu-west-1"] → 2
```

Note: `len` (from [type notes](04-types.md)) also handles lists and is interchangeable here. `listLen` is explicit about the expected type.

---

### `mapGet`

Return a map value by key. Returns `nil` when the map is absent or the key is missing.

Keywords: map, collection, get, key, access, lookup

```yaml
# value: "{{ mapGet .metadata.labels \"app\" }}"
# labels: {app: frontend, tier: web} → "frontend"
# labels absent                      → nil (renders as "")
```

---

### `mapKeys`

Return all keys of a map as sorted `[]string`. Returns an empty slice for non-maps.

Keywords: map, collection, keys, slice, iterate, list

```yaml
# value: "{{ join (mapKeys .metadata.labels) \", \" }}"
# {app: frontend, tier: web} → "app, tier" (order not guaranteed)
```

---

### `mapValues`

Return all values of a map as `[]interface{}`. Returns an empty slice for non-maps.

Keywords: map, collection, values, slice, iterate, list

```yaml
# value: "{{ len (mapValues .spec.schedule) }}"
# {minute: "*/5", hour: "*", dayOfMonth: "*", month: "*", dayOfWeek: "*"} → 5
```

---

## as notes

The `as` notes convert between Go types by first trying YAML/JSON parsing. They handle the case where a field arrives as a JSON-serialized string rather than its native type.

### `asList`

Convert input to `[]interface{}`. Accepts native slice, YAML list string, or JSON array string.

Keywords: list, convert, parse, json, yaml, slice, coerce

```yaml
# spec.regions is a YAML list → asList returns it as-is
# spec.regions is a JSON string "[\"us-east-1\"]" → asList parses it
# spec.regions is neither → asList returns []
```

Useful when a field may arrive as a native list or as a serialized list string (e.g. from an annotation).

---

### `asMap`

Convert input to `map[string]interface{}`. Accepts native map, YAML map string, or JSON object string.

Keywords: map, convert, parse, json, yaml, coerce, object

```yaml
# Use when a field may be a native map or a JSON-serialized map:
# value: "{{ (asMap .metadata.annotations).orkestra\\.io/config | default \"{}\" }}"
```

---

### `asString`

Convert any value to a string. For maps and slices this produces a JSON encoding.

Keywords: string, convert, serialize, json, coerce, stringify

```yaml
# value: "{{ asString .spec.replicas }}"
# 3 → "3"
# true → "true"
# {key: val} → "{\"key\":\"val\"}"
```

---

## Quick reference

### listMap

| Note | Signature | Returns |
|------|-----------|---------|
| `listHas` | `(list any, val any)` | `bool` |
| `listGet` | `(list any, index int)` | `any` |
| `listLen` | `(list any)` | `int` |
| `mapGet` | `(m any, key string)` | `any` |
| `mapKeys` | `(m any)` | `[]string` |
| `mapValues` | `(m any)` | `[]interface{}` |

### as

| Note | Signature | Returns |
|------|-----------|---------|
| `asList` | `(input any)` | `[]interface{}` |
| `asMap` | `(input any)` | `map[string]interface{}` |
| `asString` | `(input any)` | `string` |

---

**Next →** [08 — Safe Access Notes](08-safe-access.md)
