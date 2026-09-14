# serve

Controls whether this CRD is exposed through the Gateway API and the Control Center's Serve form. Requires `gateway.api.enabled: true` at the Katalog level.

```yaml
spec:
  crds:
    platformResource:
      serve:
        enabled: true
        category: "Compute"
        description: "Deploy and manage platform workloads"
        ignore:
          - spec.internalRef
          - spec.managedBy
        include: ./serve/platformresource.yaml   # or inline fields:
        fields:
          environment:
            label: "Environment"
            placeholder: "staging"
            category: "Basics"
            order: 1
          workloadType:
            label: "Workload Type"
            category: "Basics"
            order: 2
          productionApproval:
            label: "Approval Ticket"
            hint: "Required for production deployments"
            category: "Governance"
            order: 10
            when:
              - field: environment
                equals: production
          certIssuer:
            label: "Certificate Issuer"
            category: "TLS"
            order: 20
            or:
              - field: workloadType
                equals: cert
          maintenanceMode:
            label: "Maintenance Mode"
            disabled: "This field is managed by the platform team"
            order: 99
```

## `serve` top-level fields

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | `true` — this CRD gets a **[+ Create]** button in the Control Center and its schema is served at `GET /api/v1/schema/{kind}`. |
| `category` | — | Category label shown in the schema catalog (`GET /api/v1/schema/`). Groups related CRDs in the service catalog. |
| `description` | — | Short description shown in the catalog. Overrides the CRD-level description. |
| `ignore` | — | Dot-notation field paths excluded from the Serve form. Use for internal or platform-managed fields that should not be visible to users. |
| `fields` | — | Optional form hints. Each key matches a field name in the CRD spec. Missing keys are rendered from the OpenAPI schema alone. |
| `labels` | — | Label keys exposed as self-service form fields, written to `metadata.labels` on apply. Each entry needs an explicit `type`. |
| `annotations` | — | Annotation keys exposed as self-service form fields, written to `metadata.annotations` on apply. Each entry needs an explicit `type`. |
| `include` | — | Path (relative to the katalog file) to a YAML file containing `fields:`, `labels:`, and/or `annotations:` keys. Inline entries take precedence. Expanded at load time. |
| `namespace` | — | Template expression resolving the namespace a new CR is created in. Required on a namespaced CRD with `serve.enabled: true`; rejected on a cluster-scoped one. See [`serve.namespace`](#servenamespace) below. |
| `clusters` | — | List of registered cluster names (static or template expressions). Declares which clusters this CRD's intents may be applied to, and is the default fan-out when no target override is set. Absent means local cluster only. See [`serve.clusters`](#serveclusters) below. |

`serve/platformresource.yaml` (the include file):

```yaml
fields:
  environment:
    label: "Environment"
    placeholder: "staging"
    category: "Basics"
    order: 1
  team:
    label: "Owning Team"
    category: "Basics"
    order: 2
```

## `serve.fields.<name>`

| Field | Description |
|-------|-------------|
| `label` | Display label. Overrides the OpenAPI schema `description`. |
| `hint` | Helper text shown below the field. |
| `placeholder` | Input placeholder. |
| `category` | Section heading in the form. Fields with the same category are grouped together. Empty defaults to `"Spec"`. |
| `order` | Sort order within the form *and* validation-rule priority — see note below. `0`/unset follows every field that declares one. |
| `when` | All conditions must pass for this field to be shown (AND). Uses the same `Condition` type as resource templates — see [06-when-conditions.md](06-when-conditions.md). Evaluated client-side in the Control Center; gateway/admission is the backstop. |
| `or` | At least one condition must pass for this field to be shown (OR). When both `when` and `or` are declared, both blocks must pass. |
| `required` | When `true`, marks the field as mandatory — enforced both client-side (the browser shows an asterisk and blocks submission while Empty() and server-side: an implicit `exists` rule with `action: deny` is synthesized automatically at load time, so every caller of the Gateway API is covered, not just the Control Center form. No matching `validation.rules` entry needs to be hand-written. Has no effect on fields currently hidden by a `when:` or `or:` condition. |
| `disabled` | Non-empty string — field is rendered greyed-out with this message. Useful for platform-managed fields that should be visible but not editable. |
| `path` | — | Dot-notation path mapping the field to a nested location in the CRD `spec`. When set, the field value is written to `spec.<path>` instead of `spec.<name>`. See [`path` — nested spec paths](#servefieldspath) below. |
| `value` | — | Template expression that transforms the submitted value before writing to `spec.<path>` (or `spec.<name>`). Use `.value` for the raw submitted value. Mutually exclusive with `values`. → [Field translation](22-serve-field-translation.md) |
| `values` | — | Fanout map — one intent field → multiple CRD spec paths. Keys are dot-notation spec paths; values are template expressions. Mutually exclusive with `value`. → [Field translation](22-serve-field-translation.md) |

---
`order` isn't just cosmetic form layout. When multiple `required`/`type: enum` fields fail validation at once, only the first violation is reported as the headline denial reason — and synthesized rules are evaluated in the same order `order` puts the fields in, so the field a developer sees *first* on the form is also the one whose error they see first if several are wrong simultaneously. Two fields on the same CRD sharing a non-zero `order` value is a load-time error (`ork validate`) for exactly this reason — `0`/unset is the only value any number of fields may share, since it means "no preference," not a real position.

## `path` — nested spec paths

By default, `serve.fields` maps field names directly to top-level `spec` paths:

```yaml
fields:
  repository:
    label: "Repository"
  # → spec.repository
```

Use `path` to map a field to a nested location:

```yaml
fields:
  repository:
    path: app.repository
    label: "Repository"
  # → spec.app.repository

  cpu:
    path: app.resources.cpu
    label: "CPU Request"
  # → spec.app.resources.cpu
```

Callers submit flat field names — they don't need to know the nesting structure. The gateway maps the field to the correct location in the CRD.

```json
POST /api/v1/apply
{
  "target": "app",
  "repository": "myorg/app",
  "cpu": "500m"
}
```

→ [Full `path` reference](21-serve-nested-spec.md)


## `serve.name`

`metadata.name` exists on every CR regardless of scope, so `serve.name` doesn't care whether the CRD is namespaced or cluster-scoped — it applies uniformly either way. It's optional, not required, though: most CRDs still want the caller to choose a name, since multiple concurrent instances of the same underlying app are normal (PR previews, ephemeral environments), and a name is the only thing distinguishing them. Set `serve.name` only when instances are 1:1 with some other identity the caller already supplies, and a redeploy is meant to update that same CR in place rather than create a new one — a stable environment where only the image tag changes between deploys:

```yaml
serve:
  enabled: true
  name: '{{ repoSlug .spec.repository }}'
```

`serve.name` is a template expression the gateway resolves server-side, against exactly what the caller submitted (labels, annotations, spec — the same data `validation.rules` sees), and always wins over whatever (if anything) the caller sent. Once set, the Control Center form never renders a Name field.

**When it's not set:** the Gateway API requires the caller to supply a name themselves — `metadata.name` in full CR mode, a flat `"name"` field in target mode — and rejects the request immediately with a structured violation (`metadata.name is required`) if it's empty, the same clean-rejection treatment every other Gateway API failure gets, rather than letting the request fall through to a raw Kubernetes "name is required" error from the SSA patch.

**`requireServeName`** — `GET /katalog` (and therefore the Control Center) exposes this as a computed field, `true` unless `serve.name` is declared. It's not something you set directly; it's derived from whether `serve.name` is present.

If a templated `serve.name` resolves to something surprising, it's immediately visible in the Gateway API's own response (`name`, `pollUrl`) rather than silently breaking anything.

## `serve.namespace`

For a namespace-scoped CRD, something has to decide which namespace a self-service `POST /api/v1/apply` creates the CR in — and a form or a CI `curl` shouldn't be the one deciding it, any more than it should be the one choosing which Deployment image tag is "correct." `serve.namespace` works the same way as `serve.name` above — a template expression the gateway resolves server-side against exactly what the caller submitted, always winning over whatever (if anything) the caller sent — but only applies to namespaced CRDs, and (unlike `serve.name`) is required rather than optional:

```yaml
serve:
  enabled: true
  namespace: '{{ teamName }}'    # or a note, or a literal: "platform-workloads"
```

Once this is set, the Control Center form never renders a namespace field, and no Gateway API caller — curl, CI, the Control Center — needs to know or supply one. `serve.name` plus `serve.namespace` (when both are set) leaves the declared fields as the whole contract, no Kubernetes identity questions left for the developer or CI to answer at all.

Three things `ork validate` enforces about it:

- **Required on a namespaced CRD with `serve.enabled: true`.** CRDs are namespaced by default (`namespaced: false` opts out) — without `serve.namespace`, self-service creation has no way to know where the CR belongs.
- **Rejected on a cluster-scoped CRD** (`namespaced: false`) — there's no namespace to resolve into, so declaring one is always a mistake, not a preference. (A cluster-scoped CRD sidesteps the whole namespace question a different way — see the note below.)
- **Rejected when templated *and* the CRD's informer is pinned to one fixed namespace** (`allowedNamespaces` with exactly one entry, or the legacy `namespace:` field). A templated expression can resolve differently per submission; a CR placed outside the one namespace the informer watches would exist in the cluster but never be reconciled, silently.

`serve.namespace` **routes into** a namespace — it doesn't create one. Whatever it can resolve to must already exist; the platform team provisions it ahead of time (`kubectl apply -f namespace.yaml` in a real rollout, `setup.apply` in an e2e fixture), the same way a namespaced CRD already requires today.

This only affects the Gateway API. A raw `kubectl apply` is unaffected either way — `kubectl` always resolves *some* namespace client-side before a request ever reaches the API server (typically `default`), so there's never a genuinely empty namespace for anything server-side to notice and fill in the way an omitted JSON field lets the Gateway API detect intent. `serve.namespace` is deliberately not a mutating-webhook default for that reason — it would only ever see the wrong namespace to silently override, never a blank one to fill in.

**The cluster-scoped alternative.** A CRD can sidestep this entirely by being cluster-scoped (`namespaced: false`) and having `onCreate` provision a namespace as a *child resource* of the CR instead — the CR itself has no namespace, so there's nothing for `serve.namespace` to resolve. Two different answers to the same "a developer shouldn't have to pick a namespace" problem, matched to two different scope choices: cluster-scoped + `onCreate`-provisions-a-child-namespace, or namespaced + `serve.namespace`-routes-into-a-platform-provisioned-one.

## `serve.modes`

Controls which apply modes are available for this CRD. Both modes default to `true` for backward compatibility.

```yaml
serve:
  enabled: true
  modes:
    target: true   # target mode — submit fields with a target identifier
    cr: false      # full CR mode — submit a complete Kubernetes CR
```

**`target`** — when `true`, callers can use the target mode format: `{"target": "app", "fields": ...}`. This is the intent-first delivery model where callers submit flat fields and the gateway builds the CR.

**`cr`** — when `true`, callers can use the full CR format: `{"apiVersion": "...", "kind": "...", "spec": {...}}`. This is the traditional Kubernetes CR submission model.

Both modes default to `true` when omitted. At least one mode must be enabled. `ork validate` enforces this.

**Examples**

Only target mode — enforce intent-first delivery:

```yaml
serve:
  enabled: true
  target: app
  modes:
    target: true
    cr: false
```

Only full CR mode — disable intent-first delivery:

```yaml
serve:
  enabled: true
  modes:
    target: false
    cr: true
```

Both modes enabled (default):

```yaml
serve:
  enabled: true
  # modes omitted — both true by default
```

**Validation rules** — `ork validate` checks that:

1. At least one mode is enabled.
2. If `target` is `false`, `serve.target` must not be set (a target is only meaningful when target mode is enabled).

---
## `serve.clusters`

Declares which registered clusters this CRD's intents are allowed to be applied to,
and is the default fan-out target set when no target-level override is declared.
Requires `gateway.clusters` to be configured.

```yaml
serve:
  enabled: true
  namespace: default
  clusters:
    - prod
    - staging     # all applies fan-out to both prod and staging
```

Each entry is either a static cluster name or a template expression:

```yaml
serve:
  clusters:
    - prod
    - '{{ if eq .request.region "eu" }}eu-west{{ else }}us-east{{ end }}'
```

**Static name** — validated at `ork validate` time against `gateway.clusters`. A
name not present in the registry is a validation error.

**Template expression** — validated at `ork validate` time for parse correctness
and function existence. Name resolution is deferred to apply time. If the resolved
name is not registered at apply time, the intent is rejected for that cluster.
`toList` works here the same way it does in `exclude:` — useful for resolving a
note function that returns a list of cluster names for the current context (weekday
vs weekend routing, region-based sets, etc.).

**Absent** — the CR is applied to the local cluster only (the one the gateway runs
on). This is the default for katalogs with no cluster routing configured.

**Fan-out behaviour** — when `serve.clusters` lists more than one entry, a single
apply request is sent to each resolved cluster. The response carries a `clusters`
array with per-cluster results. Use `target.clusters` on a named target to restrict
the fan-out to a subset.

```yaml
# Per-cluster token differentiation via separate targets.
# admin-token can apply to prod; dev-token can apply to staging only.
serve:
  clusters:
    - prod
    - staging
  target:
    prod-deploy:
      primary: true
      clusters:
        - prod
      tokens:
        admin-token:
          permissions: [create, update, delete]
    staging-deploy:
      clusters:
        - staging
      tokens:
        dev-token:
          permissions: [create]
        admin-token:
          permissions: [create, update, delete]
```

→ [`gateway.clusters`](24-gateway-clusters.md) — configuring registered clusters  
→ [`serve.target` / `target.clusters`](#servetarget) — per-target fan-out scoping  
→ [Multi-cluster routing](../../../concepts/self-service/10-multi-cluster-routing.md) — concept overview

---

## `serve labels/annotations`

Exposes label and annotation keys as self-service form fields, written to `metadata.labels`/`metadata.annotations` on apply instead of `spec`. Flat — `labels` and `annotations` are the only two buckets; there is no third kind of Kubernetes object-metadata field to add later.

```yaml
serve:
  enabled: true
  labels:
    team:
      label: "Team"
      placeholder: "team-payments"
      required: true
  annotations:
    canary.myorg.io:
      label: "Enable canary rollout"
      type: boolean
    cost-center.myorg.io:
      label: "Cost Center"
      type: enum
      enum: ["finance", "engineering", "sales"]
```

Each entry is a `ServeFieldConfig` — the same shape as `serve.fields.<name>` above, plus two fields that only matter here (spec fields always infer these from the CRD's OpenAPI schema instead):

| Field | Description |
|-------|-------------|
| `type` | Required for labels/annotations — they have no CRD schema to infer type from. One of `string` (default), `integer`, `number`, `boolean`, `enum`. |
| `enum` | Valid values when `type: enum`. Required in that case — `ork validate` rejects `type: enum` with no `enum:` list. |

Validated at `ork validate` time: every key must be a syntactically valid Kubernetes label/annotation key (`[prefix/]name`), and no key may collide with `serve.fields` or the other bucket.

→ [concepts/self-service — Additional Fields](../../../concepts/self-service/01-labels-and-annotations.md) — why this exists, and the boolean-checkbox gotcha with `hasAnnotation`

Without any `serve:` block on the CRD entry, the CRD is not exposed via the Gateway API regardless of what the Katalog-level `gateway.api` config says.

---

## `serve.target`

`serve.target` is the caller-facing identifier for the CRD, decoupled from the Kubernetes kind. It accepts a scalar string or a named map.

**Scalar shorthand** — single primary target, no aliases:

```yaml
serve:
  enabled: true
  target: smartapp   # callers use this, not "AppRequest"
```

When omitted, defaults to the lowercased kind (`AppRequest` → `apprequest`).

**Map form** — primary entry plus optional aliases. Exactly one entry must have `primary: true`:

```yaml
serve:
  enabled: true
  targetOverride: true
  target:
    smartapp:
      primary: true
      targetOverride: false   # overrides the global setting
    preview:
      enabled: true           # default; omit to keep it simple
      include: ./serve/aliases/preview.yaml
    legacy:
      enabled: false          # surface closed; config still present
      tokens:
        platform-team:
          permissions:
            global: [get, list]
```

### `serve.target.<name>` field reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `primary` | bool | `false` | Marks this as the primary entry. Exactly one entry must be `true`. `ork validate` enforces this. |
| `enabled` | bool | `true` | When `false`, this surface is invisible to callers and the schema catalog. Returns the same error as an unknown target — no signal leaks. The primary's config authority remains active even when disabled. |
| `include` | string | — | Path **relative to the katalog file** to a YAML file with `tokens:` and/or `config:` keys. Resolved at load time; inline fields take precedence on merge. |
| `tokens` | map | — | Per-entry token restrictions. Same shape as `serve.tokens`. When set, only tokens listed here may access this surface. A token absent from this map is denied even if allowed at the CRD level. |
| `config.response` | object | — | Same `default`, `payload`, `exclude`, and `poll` sub-fields as `serve.config.response`. Overrides the CRD-level response for callers on this surface. |

### `include:` file location

`include:` paths are resolved relative to the **katalog file**, not the calling template. A common convention:

```text
katalog.yaml
serve/
  aliases/
    preview.yaml      ← include: ./serve/aliases/preview.yaml
    internal.yaml     ← include: ./serve/aliases/internal.yaml
```

An include file may contain `tokens:` and `config:` at the top level:

```yaml
# ./serve/aliases/preview.yaml
tokens:
  control-center:
    permissions:
      global: [get, list]

config:
  response:
    default: false
    payload:
      phase: '{{ .status.phase }}'
      alias: '{{ getServeAlias . }}'
```

### Caller usage

```bash
# Target mode — primary target
curl -X POST /api/v1/apply \
  -d '{"target": "smartapp", "repository": "...", "image": "..."}'

# Target mode — alias
curl -X POST /api/v1/apply \
  -d '{"target": "preview", "repository": "...", "image": "..."}'

# Full CR mode — still works regardless
curl -X POST /api/v1/apply \
  -d '{"apiVersion": "platform.myorg.io/v1", "kind": "AppRequest", ...}'
```

## `serve.config.response`

Configures how the Gateway API response and resource GET responses are shaped.

### `serve.config.response.default`

Controls whether the full CR is included in the response before `payload` and `exclude` are applied.

| Value | Behavior |
|-------|----------|
| `true` (default) | Start with the full CR; `payload` fields are merged in, `exclude` paths are stripped |
| `false` | Start with an empty map; only `payload` fields appear |

### `serve.config.response.payload`

A map of named template expressions added to the response. Keys become top-level fields in the returned JSON.

```yaml
serve:
  config:
    response:
      payload:
        phase: '{{ .status.phase }}'
        serviceURL: '{{ serviceURL }}'
        queueDepth: '{{ .external.queueDepth.value | default 0 }}'
        nextSteps: '{{ nextSteps }}'
```

**Evaluation timing:**

| Endpoint | What's available |
|----------|------------------|
| `POST /api/v1/apply` | `.spec`, `.metadata`, labels, annotations — `.status` is absent |
| `GET /api/v1/resources/...` | Full CR including `.status` (runtime has written it) |

**Payload is always a flat map of only the declared fields — not the full CR.**

### `serve.config.response.poll`

Configures how the `pollUrl` in the Gateway API response is generated.

```yaml
serve:
  config:
    response:
      poll:
        url: "{{ devServerURL }}"      # replaces the default URL
        field: status.phase            # appends ?field=value
```

| Field | Behavior |
|-------|----------|
| `url` | Replaces the default `/api/v1/resources/{kind}/{namespace}/{name}` with a custom template |
| `field` | Appends `?field=<value>` to whatever URL is resolved (default or custom) |

**When both are set:** `field` is appended to the custom URL.

**Default `pollUrl`:** `/api/v1/resources/{kind}/{namespace}/{name}`

**Examples:**

```yaml
# Only field — appends to default
poll:
  field: status.phase
# → /api/v1/resources/AppRequest/ns/name?field=status.phase

# Custom URL + field
poll:
  url: "{{ devServerURL }}"
  field: status.phase
# → http://dev-server:9999?field=status.phase

# Custom URL only
poll:
  url: 'https://monitor.myorg.io/status/{{ .metadata.name }}'
# → https://monitor.myorg.io/status/payments-api
```

### `serve.config.response.exclude`

A list of dot-notation field paths to strip from the response after `payload` is applied.

```yaml
serve:
  config:
    response:
      exclude:
        - metadata.managedFields
        - status.observedGeneration
        - '{{ toList (getAnnotation . "exclude-fields") }}'  # dynamic
```

Each entry can be:
- A plain string path — `metadata.managedFields`
- A template expression that resolves to a comma-separated string — `{{ toList (getAnnotation . "exclude-fields") }}`

**Exclusions apply to the GET response, not the payload itself.**

## `serve.tokens`

Restricts which Gateway API tokens can access this CRD, with per-token permissions and namespace scoping.

```yaml
serve:
  tokens:
    control-center:
      namespaces: ["default"]
      permissions:
        global: ["*"]              # full access
    ci-pipeline:
      namespaces: ["team-payments-staging"]
      permissions:
        resources: ["create", "update", "get", "list"]
        schema: ["get", "list"]
    security-audit:
      namespaces: ["default", "team-payments-staging"]
      permissions:
        resources: ["get", "list"]
```

### Token entry structure

| Field | Description |
|-------|-------------|
| `namespaces` | List of allowed namespaces. Empty means all namespaces. Ignored for cluster-scoped CRDs. |
| `permissions` | `global`, `schema`, and `resources` lists (see below). |

### `permissions` scopes

| Scope | Endpoints | Valid operations |
|-------|-----------|------------------|
| `global` | All endpoints | `get`, `list`, `create`, `update`, `delete`, `*` |
| `schema` | `GET /api/v1/schema`, `GET /api/v1/raw-schema` | `get`, `list` (only) |
| `resources` | `GET/POST/DELETE /api/v1/resources`, `POST /api/v1/apply` | `get`, `list`, `create`, `update`, `delete`, `*` |

### Resolution priority (per endpoint class)

1. Class-specific list (`schema:` or `resources:`) when non-empty
2. `global:` list when class-specific is empty
3. No access when both are empty

### Validation

`ork validate` checks:
- Every token name in `serve.tokens` exists in `gateway.api.auth.tokens`
- Schema permissions only contain `get` or `list`
- Namespaces are within CRD's `allowedNamespaces` and outside `restrictedNamespaces`
- When `global` is non-empty, class lists must be subsets

## `gateway.api.auth.include:`

`include:` is supported in `gateway.api.auth`, following the same pattern as `status.include`, `validation.include`, and `serve.include`. Inline tokens override included tokens with the same name.

```yaml
gateway:
  api:
    auth:
      include: ./shared/tokens.yaml
      tokens:
        - name: control-center   # overrides if in included file
          secretRef:
            name: ork-apply-token
            key: token
```

---

## Complete Example

```yaml
# katalog.yaml
serve:
  enabled: true
  target: smartapp
  name: '{{ repoSlug .spec.repository }}'
  namespace: '{{ teamName }}-{{ environmentName }}'
  include: ./fields.yaml

  config:
    response:
      default: true
      poll:
        url: "{{ devServerURL }}"
        field: status.phase
      payload:
        phase: '{{ .status.phase }}'
        serviceURL: '{{ serviceURL }}'
        queueDepth: '{{ .external.queueDepth.value | default 0 }}'
        nextSteps: '{{ nextSteps }}'
      exclude:
        - metadata.managedFields
        - '{{ toList (getAnnotation . "platform.myorg.io/exclude-fields") }}'
```

```yaml
# fields.yaml
fields:
  repository:
    label: "Repository"
    order: 1
  image:
    label: "Container Image"
    order: 2
  replicas:
    label: "Replicas"
    type: integer
    order: 4

labels:
  team:
    label: "Team"
    order: 0
  environment:
    label: "Environment"
    order: 3
```

```yaml
# allowed_tokens.yaml
control-center:
  namespaces: ["default"]
  permissions:
    global: ["*"]

ci-pipeline:
  namespaces: ["team-payments-staging"]
  permissions:
    resources: ["create", "update", "get", "list"]
    schema: ["get", "list"]
```

```yaml
# tokens.yaml
- name: control-center
  secretRef:
    name: ork-apply-token
    key: token
- name: ci-pipeline
  secretRef:
    name: ci-pipeline-token
    key: token
```

## `serve.target` — target map

`serve.target` accepts either a scalar shorthand or a named map. In the map form, every entry is either the primary (exactly one with `primary: true`) or an alias. Both share the same field shape and merge rules.

### Scalar shorthand

```yaml
serve:
  enabled: true
  target: apifixture
```

Equivalent to the map form with one primary entry and no aliases. `serve.tokens` and `serve.config` at the CRD level apply to all callers.

### Map form — primary and aliases

```yaml
serve:
  enabled: true
  target:
    apifixture:
      primary: true
    preview:
      include: ./serve/aliases/preview.yaml
    internal:
      tokens:
        platform-team:
          permissions:
            global: ["*"]
      config:
        response:
          default: false
          payload:
            alias: '{{ getServeAlias . }}'
            target: '{{ getServeTarget . }}'
  # CRD-level fallback — used by any entry that does not declare its own
  tokens:
    control-center:
      permissions:
        global: ["*"]
  config:
    response:
      default: true
      payload:
        phase: '{{ .status.phase }}'
```

### `serve.target.<name>` fields

| Field | Type | Description |
|-------|------|-------------|
| `primary` | bool | Marks this as the primary entry. Exactly one entry must set this to `true`. Validated by `ork validate`. |
| `enabled` | bool (default: true) | When `false`, this surface is closed to callers and hidden from the schema catalog. The primary's config authority (namespace resolution, fallback tokens) remains active regardless. |
| `include` | string | Path **relative to the katalog file** to a YAML file with `tokens:` and/or `config:` keys. Resolved at load time. Inline fields take precedence on merge. |
| `tokens` | map | Per-entry token restrictions — same shape as `serve.tokens`. When set, only tokens listed here are checked for access to this surface. |
| `config.response` | object | Same `default`, `payload`, `exclude`, and `poll` fields as `serve.config.response`. |
| `operatorBox` | object | Per-target operatorBox — overrides the CRD-level `operatorBox` for CRs routed through this surface. See [`serve.target.<name>.operatorBox`](#servetargetnameoperatorbox) below. |

### `serve.target.<name>.operatorBox`

When a CR is submitted through the gateway, the apply handler stamps `orkestra.orkspace.io/serve-target` on it. The runtime reconciler reads this annotation at reconcile time and uses the matching target's `operatorBox` instead of the CRD-level one. CRs applied via `kubectl apply` (no annotation) always fall back to the CRD-level `operatorBox`.

This makes it possible to deploy different resources depending on which surface submitted the intent — without branching on `when:` conditions inside a single shared operatorBox:

```yaml
spec:
  crds:
    website:
      operatorBox:             # fallback — used by kubectl apply / unknown targets
        onCreate:
          deployments:
            - name: "{{ .metadata.name }}"
          services:
            - name: "{{ .metadata.name }}-svc"

      serve:
        enabled: true
        target:
          web:
            primary: true
            operatorBox:       # used when CR arrives via the "web" target
              onCreate:
                deployments:
                  - name: "{{ .metadata.name }}-web"
          apifixture:
            operatorBox:       # used when CR arrives via the "apifixture" target
              onCreate:
                deployments:
                  - name: "{{ .metadata.name }}-apifixture"
```

**Resolution order** (most specific first):

1. The target entry whose name matches `serve-alias` annotation (alias wins over primary)
2. The target entry whose name matches `serve-target` annotation
3. CRD-level `operatorBox` — fallback when no annotation or no matching target

**Cleanup on target change** — when a CR moves between targets (e.g. re-submitted via a different surface), the previous target's resources are cleaned up automatically via a label-selector sweep on `orkestra-owner=<name>.<prevTarget>`. No manual cleanup is needed. To retain old-target resources deliberately, set `keepPreviousSurface: true` in `target.<name>.apply.overrides`.

**What stays fixed at the CRD level** — reconciler settings (`workers`, `resync`, `autoscale`) are always taken from the CRD-level `operatorBox`. Everything else — templates, gates, status, hooks, and `hooks.args` — falls back to the CRD-level value when absent on the target, and can be overridden when present.

→ [Full per-target operatorBox reference](26-serve-target-operatorbox.md) — preReconcile gates, surface switch cleanup, `keepPreviousSurface`, simulate patterns

**Simulating a specific target** — use `spec.target` in the simulate file, or `--target` on the CLI:

```yaml
# simulate-web.yaml
spec:
  katalog: ./katalog.yaml
  cr: ./cr.yaml
  target: web
  expect:
    ops:
      - cycle: 1
        verb: create
        resource: deployments
        name: my-app-web
```

### `include:` files for target entries

An entry's `include:` path is resolved relative to the katalog file, not the calling file. The referenced file may contain `tokens:` and `config:` at the top level:

```yaml
# ./serve/aliases/preview.yaml
#
# Path: relative to katalog.yaml, so katalog.yaml alongside serve/aliases/ resolves correctly.
tokens:
  control-center:
    permissions:
      global: [get, list]

config:
  response:
    default: false
    payload:
      phase: '{{ .status.phase }}'
      alias: '{{ getServeAlias . }}'
      workloadType: '{{ .spec.workloadType }}'
```

Inline fields in the katalog always take precedence over included fields.

### Token resolution

Permissions resolve through a three-layer chain, most specific first:

1. **Entry tokens** — when `target.<name>.tokens` is declared, only those entries are checked.
2. **CRD tokens** — when no entry tokens are declared, `serve.tokens` (CRD-level) applies.
3. **Allow all** — when neither level restricts, all valid gateway tokens are allowed.

A token absent from an entry's `tokens` map is denied for that surface even if it is declared in `gateway.api.auth.tokens` and allowed at the CRD level.

### `enabled: false` — closing a surface

Setting `enabled: false` on any entry makes it invisible to callers and the schema catalog. It returns the same response as an unknown target — no signal leaks. The primary entry may be disabled; its config authority (namespace template, CRD-level token fallback) still applies to enabled aliases.

```yaml
target:
  apifixture:
    primary: true
    enabled: false   # primary surface closed — callers must use a named alias
  internal:
    tokens:
      platform-team:
        permissions:
          global: ["*"]
```

`ork validate` warns when all surfaces are disabled — the CRD is unreachable.

### Intent provenance

Every CR applied through the gateway receives three annotations stamped by the apply handler:

| Annotation | Value |
|---|---|
| `orkestra.orkspace.io/serve-target` | The primary serve target (e.g. `apifixture`) |
| `orkestra.orkspace.io/serve-alias` | The alias name, or `""` for the primary target |
| `orkestra.orkspace.io/serve-source` | Verified OIDC `sub` claim of the caller, or `""` for static token auth |

Three notes expose these in operatorBox templates, response payloads, and `when:` conditions:

| Note | Returns |
|------|---------|
| `getServeTarget .` | Primary target name — always set |
| `getServeAlias .` | Alias name — `""` when applied via primary target |
| `getServeSource .` | Caller identity — set when auth was via an OIDC token |

`getServeAlias` in a `when:` condition lets the operatorBox route to different child resources depending on which surface delivered the intent:

```yaml
onReconcile:
  custom:
    # Full production Application — primary target and internal alias only
    - apiVersion: argoproj.io/v1alpha1
      kind: Application
      metadata:
        name: "{{ .metadata.name }}"
        namespace: argocd
      when:
        - field: '{{ ne (getServeAlias .) "preview" }}'
          equals: "true"

    # Lightweight preview Application — preview alias only
    - apiVersion: argoproj.io/v1alpha1
      kind: Application
      metadata:
        name: "{{ .metadata.name }}-preview"
        namespace: argocd
      when:
        - field: '{{ eq (getServeAlias .) "preview" }}'
          equals: "true"
```

### Validation

`ork validate` checks:
- Exactly one entry has `primary: true` in the map form
- Every entry name is a valid DNS label (`[a-z0-9-]+`)
- No entry name collides with a primary or alias on another CRD in the katalog
- Every token in an entry `tokens:` block exists in `gateway.api.auth.tokens`
- When `serve.tokens` is declared, entry tokens must be a subset of it
- Warning when primary surface is disabled and no enabled aliases are declared

### `ork serve validate --full`

```text
● platRsc
  target: apifixture  /  kind: PlatformResource
  aliases:   2
    · internal  (1 token, custom response)
    · preview   (1 token, custom response)
```

---

## `gateway.api.auth.tokens`

Each entry must declare exactly one credential source: `token`, `secretRef`, `githubOIDC`, `gitlabOIDC`, `vaultOIDC`, or `oidc`.

### Static token sources

```yaml
gateway:
  api:
    auth:
      tokens:
        # env var reference — resolved at gateway startup
        - name: control-center
          token: "${CONTROL_CENTER_TOKEN}"

        # secretRef — gateway creates the Secret if absent; rotates after 90 days
        - name: ci-pipeline
          secretRef:
            name: ci-pipeline-token
            key: token
            rotateAfter: 90d
```

### OIDC token sources

Short-lived JWTs issued by GitHub Actions, GitLab CI, or any OIDC-compliant provider. No stored secret — the token is verified against the provider's public JWKS on every request.

```yaml
gateway:
  api:
    auth:
      tokens:
        # GitHub Actions — issuer hardcoded to token.actions.githubusercontent.com
        - name: github-payments-ci
          githubOIDC:
            allow:
              repository: myorg/payments      # must match exactly
              ref: refs/heads/main            # main branch only
              environment: production         # optional — gates prod-deploy jobs

        # GitLab CI — issuer hardcoded to gitlab.com
        - name: gitlab-infra-ci
          gitlabOIDC:
            allow:
              namespacePath: mygroup/infra
              refProtected: "true"

        # HashiCorp Vault — discovery via {url}/v1/identity/oidc
        - name: vault-platform-ci
          vaultOIDC:
            url: https://vault.myorg.io       # required — Vault server URL
            namespace: platform               # optional — Vault Enterprise namespace
            audience: orkestra                # optional
            allow:
              entityName: ci-agent            # Vault entity name
              entityID: ""                    # Vault entity UUID (stable)
              namespace: platform             # Vault namespace of the entity

        # Generic OIDC — any provider following the discovery standard
        - name: internal-ci
          oidc:
            issuer: https://auth.myorg.io     # required for generic oidc
            audience: orkestra                # optional
            allow:
              sub: "system:serviceaccount:ci:runner"
```

`allow` fields work as an AND filter — all declared fields must match the verified JWT claims. Undeclared fields are not checked. An empty `allow` block accepts any valid token from that issuer.

When an OIDC token matches, the verified `sub` claim is stamped on the CR as `orkestra.orkspace.io/serve-source` — available in templates via `getServeSource .`.

### `gateway.api.auth.tokens` validation

`ork validate` enforces token format at static analysis time:

| Rule | Error |
|------|-------|
| Exactly one source field per entry | Must set one of: `token`, `secretRef`, `githubOIDC`, `gitlabOIDC`, `vaultOIDC`, `oidc` |
| `token:` must be `${ENV_VAR}` | Literal values rejected — use `extraEnv` in Helm |
| `secretRef:` must supply `name` and `key` | Missing field reported |
| `oidc.issuer` required for generic `oidc` | Use a named preset for providers with hardcoded issuers |
| `githubOIDC.allow` must not be empty | Declare at least one field — empty block accepts any GitHub Actions token |
| `gitlabOIDC.allow` must not be empty | Declare at least one field — empty block accepts any GitLab CI token |
| `vaultOIDC.url` required | Set to the Vault server URL |
| `vaultOIDC.allow` must not be empty | Declare at least one field — empty block accepts any Vault entity token |
| Duplicate token names | Reported at first duplicate |

## Where to go next

- **Conceptual overview:** → [idp](../../../concepts/self-service/)
- **Aliases and intent provenance:** → [concepts/self-service/04-aliases-and-provenance.md](../../../concepts/self-service/04-aliases-and-provenance.md)
- **Token scoping:** → [concepts/self-service/03-token-scoping.md](../../../concepts/self-service/03-token-scoping.md)
- **CLI reference:** → [ork serve](../../cli/13-serve.md) — validate, inspect targets, tokens, response config, and aliases without a cluster

**Gateway API:** → [gateway-api](17-gateway-api.md)
