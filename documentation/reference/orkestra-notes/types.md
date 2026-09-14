# Type Notes

Type notes inspect and convert the runtime Go types of values in the CR. Because Kubernetes stores everything as JSON, field types depend on how the API server serialized them — these notes let you branch on that safely.

## Reference

| Note | Description |
|------|-------------|
| `typeOf` | Return the runtime type name of any value. |
| `typeMap` | Shorthand predicates — return `true` when the value matches that type. |
| `typeList` | Shorthand predicates — return `true` when the value matches that type. |
| `typeString` | Shorthand predicates — return `true` when the value matches that type. |
| `typeNumber` | Shorthand predicates — return `true` when the value matches that type. |
| `typeBool` | Shorthand predicates — return `true` when the value matches that type. |
| `typeNull` | Shorthand predicates — return `true` when the value matches that type. |
| `len` | Return the length of a string, slice, or map. |
| `toInt` | Convert any value to `int64`. |
| `toFloat` | Convert any value to `float64`. |
| `toBool` | Convert a value to `bool`. |
| `toString` | Convert any value to its string representation. |
| `toJson` | Convert any value to its JSON representation. |
| `toList` | Convert a comma-separated string to a list of trimmed strings. |

## Examples

```yaml
# typeOf
# value: "{{ typeOf .spec.schedule }}"

# typeMap
normalize:
  spec:
    schedule: >
      {{ if typeMap .spec.schedule }}
        {{ cronFromMap .spec.schedule }}
      {{ else }}
        {{ cronNormalize .spec.schedule }}
      {{ end }}
# value: "{{ typeList .spec.regions }}"
# spec.regions: [us-east-1, eu-west-1] → true
# spec.regions: "us-east-1"           → false
# value: "{{ Empty .spec.annotations }}"
# {}          → true
# nil         → true
# {app: foo}  → false

# typeList
normalize:
  spec:
    schedule: >
      {{ if typeMap .spec.schedule }}
        {{ cronFromMap .spec.schedule }}
      {{ else }}
        {{ cronNormalize .spec.schedule }}
      {{ end }}
# value: "{{ typeList .spec.regions }}"
# spec.regions: [us-east-1, eu-west-1] → true
# spec.regions: "us-east-1"           → false
# value: "{{ Empty .spec.annotations }}"
# {}          → true
# nil         → true
# {app: foo}  → false

# typeString
normalize:
  spec:
    schedule: >
      {{ if typeMap .spec.schedule }}
        {{ cronFromMap .spec.schedule }}
      {{ else }}
        {{ cronNormalize .spec.schedule }}
      {{ end }}
# value: "{{ typeList .spec.regions }}"
# spec.regions: [us-east-1, eu-west-1] → true
# spec.regions: "us-east-1"           → false
# value: "{{ Empty .spec.annotations }}"
# {}          → true
# nil         → true
# {app: foo}  → false

# typeNumber
normalize:
  spec:
    schedule: >
      {{ if typeMap .spec.schedule }}
        {{ cronFromMap .spec.schedule }}
      {{ else }}
        {{ cronNormalize .spec.schedule }}
      {{ end }}
# value: "{{ typeList .spec.regions }}"
# spec.regions: [us-east-1, eu-west-1] → true
# spec.regions: "us-east-1"           → false
# value: "{{ Empty .spec.annotations }}"
# {}          → true
# nil         → true
# {app: foo}  → false

# typeBool
normalize:
  spec:
    schedule: >
      {{ if typeMap .spec.schedule }}
        {{ cronFromMap .spec.schedule }}
      {{ else }}
        {{ cronNormalize .spec.schedule }}
      {{ end }}
# value: "{{ typeList .spec.regions }}"
# spec.regions: [us-east-1, eu-west-1] → true
# spec.regions: "us-east-1"           → false
# value: "{{ Empty .spec.annotations }}"
# {}          → true
# nil         → true
# {app: foo}  → false

# typeNull
normalize:
  spec:
    schedule: >
      {{ if typeMap .spec.schedule }}
        {{ cronFromMap .spec.schedule }}
      {{ else }}
        {{ cronNormalize .spec.schedule }}
      {{ end }}
# value: "{{ typeList .spec.regions }}"
# spec.regions: [us-east-1, eu-west-1] → true
# spec.regions: "us-east-1"           → false
# value: "{{ Empty .spec.annotations }}"
# {}          → true
# nil         → true
# {app: foo}  → false

# len
# value: "{{ len .spec.regions }}"
# ["us-east-1", "eu-west-1"] → 2

# value: "{{ len .spec.schedule }}"
# {minute: "*/5", hour: "*", ...} → 5  (map with 5 fields)

# value: "{{ len .metadata.name }}"
# "my-app" → 6  (string length)

# toInt
# value: "{{ toInt .spec.replicas }}"
# "3"   → 3
# 3.7   → 3
# true  → 1
# false → 0

# toFloat
# value: "{{ toFloat .spec.threshold }}"
# "0.75" → 0.75
# 3      → 3.0

# toBool
# value: "{{ toBool .spec.enabled }}"
# "yes"  → true
# "no"   → false
# "true" → true

# toString
# value: "{{ toString .spec.replicas }}"
# 3     → "3"
# true  → "true"
# 3.14  → "3.14"

# toJson
# value: "{{ toJson .spec }}"    →  `{"replicas":3,"enabled":true}`

# toList
# value: "{{ toList .spec.excludeFields }}"
# "a,b,c"   → ["a", "b", "c"]
# "a, b, c" → ["a", "b", "c"]  (spaces trimmed)
# ""        → []
# "foo"     → ["foo"]
serve:
  config:
    response:
      exclude: '{{ toList (getAnnotation . "platform.myorg.io/exclude-fields") }}'
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
when:
  - field: spec.schedule
    operator: typeOf
    value: map       # same string that typeOf returns
```
