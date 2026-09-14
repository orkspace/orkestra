# Custom Resource

This declares one arbitrary Kubernetes resource — any Kind not covered by one of Orkestra's built-in resource types (a third-party CRD such as cert-manager's Certificate, or your own CRD). It is schema-agnostic: the spec block is written and applied exactly as declared in the Katalog, with no built-in knowledge of its structure. Kubernetes enforces the resource's own CRD schema at apply time.

Example:

```yaml
onCreate:
  custom:
    - apiVersion: "cert-manager.io/v1"
      kind: Certificate
      metadata:
        name: "{{ .metadata.name }}-tls"
        namespace: "{{ .metadata.namespace }}"
      spec:
        secretName: "{{ .metadata.name }}-tls-secret"
        dnsNames:
          - "{{ .spec.hostname }}"
        issuerRef:
          name: letsencrypt-prod
          kind: ClusterIssuer
```

---

## Lifecycle

Declare this resource under `onCreate` for an idempotent, one-time create: Orkestra creates it on the first reconcile and leaves it untouched afterward. Set `reconcile: true` on the same entry to also apply it as drift correction on every subsequent reconcile. This is a shorthand for declaring the identical entry under `onReconcile` as well — there's no need to do both.

Declare a resource under `onDelete` to run explicit cleanup before the CR's finalizer is removed. Most resources need no `onDelete` entry — they are garbage-collected automatically through owner references when the CR itself is deleted.

---

## Fields

### `apiVersion`

Type: string

APIVersion — the group/version of the target resource, e.g. "cert-manager.io/v1". Required.

---

### `kind`

Type: string

Kind — the target resource's Kind, e.g. "Certificate". Required.

---

### `metadata`

Type: object

Metadata — name, namespace, labels, and annotations for the resource. name is required. namespace is required for namespaced CRDs and must be omitted for cluster-scoped ones; Orkestra determines the correct scope via discovery unless namespaced is set explicitly.

```yaml
metadata:
  name: "{{ .metadata.name }}-tls"
  namespace: "{{ .metadata.namespace }}"
  labels:
    app: "{{ .metadata.name }}"
```

---

### `spec`

Type: map

Spec is the conventional spec block for CRDs. It is schema-agnostic and may contain templated values. Only template syntax is validated by Orkestra; structural/schema validation is deferred to the API server.

---

### `status`

Type: map

Status — an initial status block, useful when bootstrapping a resource that expects one to be present immediately. Orkestra only writes this if hasStatus resolves to true. Prefer letting the resource's own controller populate status rather than setting this.

---

### `hasStatus`

Type: boolean

HasStatus is an explicit hint about whether the CRD exposes a status subresource. Three states are useful:

  - nil: auto-detect via discovery at runtime
  - true: force status writes (patches)
  - false: never attempt status writes

Use this to avoid API errors for CRDs that do not support status.

---

### `reconcile`

Type: boolean

Reconcile: true — also apply this declaration as drift correction on every reconcile. Equivalent to declaring the same entry under both onCreate and onReconcile. When false (default), only runs on onCreate (idempotent create).

---

### `when`

Type: list

Conditions declares the set of runtime predicates that must all evaluate to true for this resource template to be applied during reconciliation.

Each condition inspects a field on the live Custom Resource using dot-notation (e.g. "spec.enabled", "metadata.labels.tier") and compares it against a value using the chosen operator. All conditions in the list are AND‑ed together.

If any condition fails, the resource is skipped for that reconcile cycle. This is not an error — it simply means "do not create/update this resource right now". This enables expressive, data‑driven orchestration such as:

```yaml
when:
  - field: spec.exposePublicly
    equals: "true"
  - field: spec.environment
    prefix: "prod"
```

Conditions allow templates to be selectively activated based on the CR's state, enabling dynamic topologies, feature flags, environment‑specific behavior, and conditional provisioning without writing Go code.

---

### `or`

Type: list

Or holds OR conditions — at least one must pass for this resource to be created. Works alongside the existing Conditions (when:) field which uses AND semantics.

```yaml
or:
  - field: spec.tier
    equals: pro
  - field: spec.tier
    equals: enterprise
```

---

### `forEach`

Type: object

ForEach declares dynamic expansion over a list field. When set, one source declaration becomes N declarations — one per list element. .item and .\<as> are available in template expressions within this declaration.

```yaml
forEach:
  field: spec.regions
  as: region
```

---

### `sleep`

Type: string

Sleep injects an artificial delay into the reconcile of this resource. Useful for autoscale testing, latency simulation, and chaos engineering. Accepts extended duration units (s, m, h, d, w, mo, y).

---

### `forceConflict`

Type: boolean

ForceConflict, when true, sets Force: true when applying this resource, taking ownership of conflicting fields instead of returning a conflict error. Overrides the CRD-level ForceConflict setting.

---

## Quick reference

| YAML key | Type |
|---|---|
| `apiVersion` | string |
| `kind` | string |
| `metadata` | object |
| `spec` | map |
| `status` | map |
| `hasStatus` | boolean |
| `reconcile` | boolean |
| `when` | list |
| `or` | list |
| `forEach` | object |
| `sleep` | string |
| `forceConflict` | boolean |
