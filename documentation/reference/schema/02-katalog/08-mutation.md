# mutation

Declarative mutation rules applied at admission time (before creation/update) and optionally at reconcile time.
Declared on a CRDEntry or inside a Motif's `admission` block.

```yaml
mutation:
  mutateFirst: false     # run mutation before validation at reconcile (default: false)
  rules:
    - field: spec.replicas
      default: 1
      valueType: int

    - field: spec.engine
      override: postgres

    - field: spec.image
      default: "{{ .Spec.Registry }}/myapp:latest"
```

## `mutation.include`

Pull rules from an external file to keep the Katalog compact. The file contains a `rules:` key with the same list structure as inline rules.

```yaml
mutation:
  include: ./admission/apprequest.yaml   # relative to the katalog file
  rules:
    - field: spec.logLevel               # appended after included rules
      default: info
```

`admission/apprequest.yaml`:

```yaml
rules:
  - field: spec.replicas
    default: 1
    valueType: int
  - field: spec.engine
    default: postgres
```

Included rules come first. Inline `rules:` append after. The `include:` path is resolved relative to the katalog file's directory — the katalog can be run from any working directory. The field is cleared from the runtime bundle after expansion.

## `mutation.rules`

Each rule sets one field. Rules are applied in order.

| Field | Required | Description |
|-------|----------|-------------|
| `field` | yes | Dot-notation path in the CR (e.g. `spec.replicas`). Supports Go templates. |
| `default` | one of | Set only if the field is **absent or empty**. Supports Go templates resolved against the CR and notes FuncMap. |
| `override` | one of | **Always** set, regardless of current value. Supports Go templates. |
| `valueType` | no | `string` (default), `int`, `float`, `bool` |
| `when` | no | All conditions must pass for this rule to be applied (AND). Empty means unconditional. Conditions support Go template expressions via `EvaluateConditions`. |
| `or` | no | At least one condition must pass for this rule to be applied (OR). When both `when` and `or` are declared, both blocks must pass. |
| `fires.reconcile` | no | `true` (default). Set `false` to make this rule admission-only — the reconciler skips it. |

Declare either `default` or `override` on each rule, not both.

`when` and `or` use the same `Condition` type as resource templates — see [06-when-conditions.md](06-when-conditions.md) for the full operator reference.

```yaml
mutation:
  rules:
    - field: spec.replicas
      default: 1
      valueType: int

    - field: spec.targetRevision
      default: main
      when:
        - field: spec.environment
          equals: staging

    - field: spec.targetRevision
      override: "{{ .spec.approvedRevision }}"
      when:
        - field: spec.environment
          equals: production
```

### Default and Override fields are enforced automatically

`default:` and `override:` on a `serve.fields`, `serve.labels`, or `serve.annotations` entry aren't just form hints — at katalog load time, each one synthesizes an implicit mutation rule. This enforcement applies to every client — the Control Center form, `curl`, a CI pipeline, a custom UI, `kubectl apply` — not only the one that renders the form.

- **`default:`** synthesizes a mutation rule that sets the field to the declared value when the caller does not submit it.
- **`override:`** synthesizes a mutation rule that always sets the field to the declared value, regardless of what the caller submitted.

For `serve.fields`, only `override:` is honored — spec fields carry their defaults from the CRD schema. For `serve.labels` and `serve.annotations`, both `default:` and `override:` synthesize a rule.

```yaml
serve:
  fields:
    environment:
      label: "Environment"
      override: production           # always set — caller cannot change it
  labels:
    team:
      label: "Team"
      default: platform              # set when caller omits it
# → synthesizes mutation rules for both; no hand-written mutation.rules entry needed
```

## `mutation.external`

External HTTP calls can be declared directly under `mutation:`. They fire before any mutation rule is applied, and their results are available in `default:` and `override:` template expressions as `.external.<name>.*`.

```yaml
mutation:
  external:
    - name: defaults
      url: "{{ .spec.configServiceUrl }}/defaults"
      continueOnError: true
      fires:
        reconcile: false   # admission-only

  rules:
    - field: spec.replicas
      default: "{{ .external.defaults.replicas }}"
      valueType: int
```

`fires.reconcile: false` means the call only runs at admission time. When omitted (default), the call runs on every reconcile as well.

See [13-external.md](13-external.md) for the full field reference.

## `mutateFirst`

When `true`, mutation rules run before validation rules during each reconcile cycle.
Useful when a mutation sets a default that a validation rule then checks.

```yaml
mutation:
  mutateFirst: true
  rules:
    - field: spec.engine
      default: postgres

validation:
  rules:
    - field: spec.engine
      equals: postgres
      message: engine must be postgres
```

## Template values

Both `default` and `override` support Go templates evaluated against the CR:

```yaml
rules:
  - field: spec.endpoint
    override: "{{ .Name }}.{{ .Namespace }}.svc.cluster.local"

  - field: spec.image
    default: "{{ .Spec.Registry }}/app:{{ .Spec.Version }}"
```

## When mutation runs

- **At admission**: if `security.webhooks.admission.enabled: true` and the CRD's `webhooks.mutation: true`.
- **At reconcile**: always — even without a webhook, mutation rules are applied each cycle before the operator writes resources.

---
