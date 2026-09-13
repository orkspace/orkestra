# Collection Notes

Collection notes work with lists and maps from the CR spec. They cover both the `listMap` family (typed access to `[]interface{}` and `map[string]interface{}`) and the `as` family (type coercion for mixed-type inputs).

## Reference

| Note | Description |
|------|-------------|
| `listHas` | Check whether a list contains a value. |
| `listGet` | Return the element at a given index. |
| `listLen` | Return the number of elements in a list. |
| `mapGet` | Return a map value by key. |
| `mapKeys` | Return all keys of a map as sorted `[]string`. |
| `mapValues` | Return all values of a map as `[]interface{}`. |
| `asList` | Convert input to `[]interface{}`. |
| `asMap` | Convert input to `map[string]interface{}`. |
| `asString` | Convert any value to a string. |

## Examples

```yaml
# listHas
# when:
#   - field: spec.regions
#     operator: typeOf
#     value: slice
# value: "{{ listHas .spec.regions \"us-east-1\" }}"
# ["us-east-1", "eu-west-1"] → true
# ["eu-west-1"]              → false

# listGet
# value: "{{ listGet .spec.regions 0 }}"
# ["us-east-1", "eu-west-1"] → "us-east-1"
# []                         → nil (renders as "")

# listLen
# value: "{{ listLen .spec.regions }}"
# ["us-east-1", "eu-west-1"] → 2

# mapGet
# value: "{{ mapGet .metadata.labels \"app\" }}"
# labels: {app: frontend, tier: web} → "frontend"
# labels absent                      → nil (renders as "")

# mapKeys
# value: "{{ join (mapKeys .metadata.labels) \", \" }}"
# {app: frontend, tier: web} → "app, tier" (order not guaranteed)

# mapValues
# value: "{{ len (mapValues .spec.schedule) }}"
# {minute: "*/5", hour: "*", dayOfMonth: "*", month: "*", dayOfWeek: "*"} → 5

# asList
# spec.regions is a YAML list → asList returns it as-is
# spec.regions is a JSON string "[\"us-east-1\"]" → asList parses it
# spec.regions is neither → asList returns []

# asMap
# Use when a field may be a native map or a JSON-serialized map:
# value: "{{ (asMap .metadata.annotations).orkestra\\.io/config | default \"{}\" }}"

# asString
# value: "{{ asString .spec.replicas }}"
# 3 → "3"
# true → "true"
# {key: val} → "{\"key\":\"val\"}"
```
