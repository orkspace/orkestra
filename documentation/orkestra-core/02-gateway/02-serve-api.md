# Serve API

The Serve API is the Gateway's intent delivery surface. It accepts flat, human-readable fields from a caller and produces a fully validated, provenance-stamped Kubernetes object applied to the cluster — without the caller needing any knowledge of CRDs, API groups, namespaces, or Kubernetes object shape.

The caller declares what they want. The Gateway handles the rest.

---

## Intent delivery chain

Every create or update request passes through six stages in sequence:

**1. Target resolution** — The caller names a target. The Gateway resolves it to the specific CRD declared in the Katalog. In CR mode, the target is inferred from the submitted CR's `apiVersion` and `kind`.

**2. Token check** — The named token is verified against the target and operation. Tokens declare which targets they can reach, which operations they can perform, and which namespaces they can operate in. A request with an invalid or insufficiently scoped token is denied before any CR is constructed.

**3. CR construction** — The flat intent fields are mapped to CR fields using the `serve.fields` declarations in the Katalog. `serve.name` and `serve.namespace` template expressions are resolved. Required fields are enforced. The result is a fully shaped Kubernetes object.

**4. Provenance** — The Gateway stamps `orkestra.orkspace.io/serve-target`, `orkestra.orkspace.io/serve-alias`, and `orkestra.orkspace.io/serve-source` annotations on the CR. These record the delivery surface that created the object and survive through the full object lifecycle.

**5. Admission validation** — Validation and mutation rules are evaluated against the constructed CR. Deny-action violations return an error immediately — the CR is never written. Mutation rules apply their defaults and overrides.

**6. Apply and respond** — The CR is applied to the cluster via server-side apply. The response is shaped by `serve.config.response` — either the full CR, a payload subset, or both.

---

## Targets

Every CRD exposes one or more named targets through `serve.target`. Exactly one entry carries `primary: true`; any other entries are additional surfaces on the same CRD:

```yaml
serve:
  target:
    myapp:
      primary: true
    preview:
      tokens:
        ci-pipeline:
          permissions:
            global: [create, update]
    internal:
      tokens:
        platform-team:
          permissions:
            global: ["*"]
```

Callers reference any of these by name in `POST /api/v1/apply`:

```json
{"target": "preview", "name": "my-service", ...}
```

All targets share the same CRD, the same operator, and the same Kubernetes resource. Each target can independently restrict which tokens are valid for it, override apply and response behaviour, and enable or disable the surface without affecting the others.

The primary entry acts as the CRD-level config authority — its `tokens` and `config` serve as fallbacks when an entry declares neither.

---

## Tokens

Tokens are declared in `serve.tokens` (CRD level) or in a target entry's `tokens` map. A token absent from a target's map is denied for that target, even if it is valid at the CRD level. Entry tokens can only narrow access, not widen it.

```yaml
serve:
  tokens:
    platform-team:
      permissions:
        global: ["*"]
    ci-read:
      permissions:
        resources: [get, list]
      namespaces: [staging, preview]
```

Each token entry takes `permissions` and optionally `namespaces`:

| Key | Effect |
|-----|--------|
| `permissions.global` | Applies to all endpoint classes that declare no class-specific list |
| `permissions.schema` | Overrides global for `GET /api/v1/schema/...` endpoints |
| `permissions.resources` | Overrides global for `/api/v1/resources/...` endpoints |
| `namespaces` | Restricts the token to these namespaces. Empty means all |

Valid operations: `get`, `list`, `create`, `update`, `delete`, `*`.

---

## Fields

`serve.fields`, `serve.labels`, and `serve.annotations` all use the same map-keyed-by-field-name shape. The only difference is the destination:

| Map | Written to |
|-----|-----------|
| `serve.fields` | `spec.*` — type inferred from CRD schema |
| `serve.labels` | `metadata.labels` — declare `type` explicitly |
| `serve.annotations` | `metadata.annotations` — declare `type` explicitly |

```yaml
serve:
  fields:
    environment:
      path: spec.environment
      required: true
      enum: [staging, production]

    image:
      values:
        spec.image.registry:    '{{ imageRegistry   .value }}'
        spec.image.repository:  '{{ imageRepository .value }}'
        spec.image.tag:         '{{ imageTag        .value }}'

    ttl:
      path: spec.ttl
      default: "24h"

  labels:
    team:
      required: true
      type: string

  annotations:
    costCenter:
      type: string
```

Key behaviours (same across all three maps):

- `path` — dot-notation destination path. Defaults to the field name when absent.
- `required` — enforced both browser-side (form) and server-side (Gateway API).
- `value` — template expression applied to the submitted value before writing. `.value` is the submitted field, `.request` is the full intent payload.
- `values` — fanout: one submitted field writes to multiple paths. Mutually exclusive with `value`. `path` is ignored when `values` is present.
- `default` / `override` — synthesise mutation rules. `default` sets when absent; `override` always sets.
- `enum` — generates an `in` membership check.
- `when` / `or` — conditional visibility in the form. Evaluated client-side.
- `disabled` — renders the field greyed out with the given reason string.
- `order` — form position. Also determines which field's error is shown first when multiple fail.

`serve.ignore` lists field names hidden from the serve form entirely.

---

## Name and namespace

`serve.name` and `serve.namespace` are template expressions the Gateway resolves at apply time:

```yaml
serve:
  name: '{{ repoSlug .spec.repository }}'
  namespace: '{{ .request.team }}'
```

`name` overrides whatever the caller submitted (or generates one when the caller omitted it). Use it when instances are 1:1 with a stable caller-supplied identity and redeploys should update the same CR rather than create a new one.

`namespace` is required for namespaced CRDs when `serve.enabled: true`. Callers never submit a namespace — the Gateway resolves it from the intent.

---

## Modes

Two submission modes are independently controllable:

| Mode | What it means | Default |
|------|--------------|---------|
| `serve.modes.target` | Submit flat fields with a `target` identifier | enabled |
| `serve.modes.cr` | Submit a full Kubernetes CR (`apiVersion` + `kind`) | enabled |

```yaml
serve:
  modes:
    cr: false    # disallow raw CR submission; only target mode accepted
```

Per-target overrides are set on the target entry's `modes` block.

---

## Response shaping

`serve.config.response` controls what the caller receives. Each target entry can declare its own `config.response` independently:

```yaml
serve:
  config:
    response:
      default: true
      payload:
        phase:      '{{ .status.phase }}'
        serviceURL: 'https://{{ .metadata.name }}.myorg.io'
      exclude:
        - metadata.managedFields

  target:
    preview:
      primary: false
      config:
        response:
          default: false        # payload fields only, no raw CR
          payload:
            phase: '{{ .status.phase }}'
```

| Field | Effect |
|-------|--------|
| `default` | `true` (default) starts the response with the full CR; `false` starts empty |
| `payload` | Named template expressions merged into the response |
| `exclude` | Dot-notation paths stripped after payload is applied |
| `poll.field` | Appends `?field=<value>` to the derived poll URL |
| `poll.url` | Replaces the default poll URL entirely |

---

## Read operations

For `get`, `list`, and `delete` the Gateway performs a token check and target resolution, then proxies the operation to the cluster. The same response shaping applies to `get` and `list` — a caller using a read-scoped token sees only the payload fields declared for their surface.

---

## Operations

| Operation | What the Gateway does |
|-----------|----------------------|
| `create` | Constructs CR, runs admission, applies via SSA |
| `update` | Same as create — SSA is idempotent |
| `get` | Token check, then cluster GET with response shaping |
| `list` | Token check, then cluster LIST with response shaping per item |
| `delete` | Token check, then cluster DELETE |

---

## Where to go next

**Concepts**

- [Target mode](../../concepts/self-service/02-target-mode.md) — how flat intent fields map to a CR and when to use target mode vs CR mode
- [Token scoping](../../concepts/self-service/03-token-scoping.md) — per-CRD and per-target token permissions in depth
- [Aliases and provenance](../../concepts/self-service/04-aliases-and-provenance.md) — multiple named targets on one CRD, provenance annotations, immutable routing surface
- [Field translation](../../concepts/self-service/08-field-translation.md) — `value`, `values`, and `WithRequest` expressions
- [Gateway as delivery layer](../../concepts/self-service/05-gateway-as-delivery-layer.md) — running the gateway standalone, without the Orkestra runtime

**Schema reference**

- [serve](../../reference/schema/02-katalog/20-serve.md) — full `serve:` block reference
- [serve — apply config](../../reference/schema/02-katalog/20-serve-apply.md) — `apply.overrides` and conflict resolution
- [serve — field translation](../../reference/schema/02-katalog/22-serve-field-translation.md) — `value`, `values`, `path` field config reference
- [serve — per-target operatorBox](../../reference/schema/02-katalog/26-serve-target-operatorbox.md) — per-target reconciler overrides
