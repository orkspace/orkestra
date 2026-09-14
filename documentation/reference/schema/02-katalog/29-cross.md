# cross

The `cross:` block under a CRD declares which other CRDs this CRD observes. After the cross read runs, the observed data is available in templates as `.cross.<as>.*`.

```yaml
crds:
  application:
    operatorBox:
      cross:
        - crd: database
          selector:
            name: "{{ .metadata.name }}-db"
          as: db
```

From that point on, anywhere in the `application` operatorBox:

```text
{{ .cross.db.found }}                → "true" if the CR was found
{{ .cross.db.status.phase }}         → the observed CR's status.phase
{{ .cross.db.spec.storageGb }}       → the observed CR's spec field
```

---

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `crd` | string | one of `crd`/`labelSelector` | Target CRD name — the key in `spec.crds`. |
| `labelSelector` | map | one of `crd`/`labelSelector` | Selects a CRD by its labels instead of by name. |
| `selector` | object | yes | Identifies which CR instance to observe. |
| `as` | string | no | Key under `.cross.*` for template access. Defaults to `crd`. |
| `source` | object | no | For cross-binary or cross-cluster reads. When absent, the informer cache is used. |

---

## `selector:`

Identifies which CR to read from the target CRD.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | CR name. Template expressions supported. |
| `namespace` | string | CR namespace. Defaults to the current CR's namespace. |
| `matchLabels` | map | Label selector — picks the first matching CR (or all, when `strategy: all`). When set, `name` and `namespace` are ignored. |

```yaml
# By name
selector:
  name: "{{ .metadata.name }}-db"
  namespace: data-system

# By labels
selector:
  matchLabels:
    tier: platform
    tenant: "{{ .spec.tenant }}"
```

---

## `source:` — cross-binary and cross-cluster reads

When the target CRD is in a different binary or cluster, declare `source:` to reach it over HTTP.

```yaml
cross:
  - crd: loader
    selector:
      name: "{{ .metadata.name }}-loader"
    source:
      host: "http://loader-runtime.loader-system:8080"
      protocol: cr
      cacheFor: 10s
    as: loader
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `host` | string | — | Base URL of the remote Orkestra runtime. Combined with `type` to build the endpoint URL. |
| `type` | string | `cr` | Endpoint type. One of `cr`, `health`, `metrics`, `info`, `events`. |
| `endpoint` | string | — | Fully-qualified URL. When set, `host` and `type` are ignored. Template expressions supported. |
| `namespace` | string | — | Override namespace when building info/events URLs. Defaults to the CR's namespace. |
| `cacheFor` | duration | `30s` | How long to cache the result. Prevents calling the remote on every reconcile. |
| `auth` | object | — | Authentication for the remote endpoint. |

### Endpoint types

| Type | URL built | What you get |
|------|-----------|-------------|
| `cr` | `/katalog/<crd>/cr/<ns>/<name>` | Full CR: spec, status, children, metrics |
| `health` | `/katalog/<crd>/health` | Operator health state and last error |
| `metrics` | `/katalog/<crd>` | Operator-level metrics |
| `info` | `/katalog/<crd>` | CRD info: list, metrics, children |
| `events` | `/katalog/<crd>/cr/<ns>/<name>/events` | CR-scoped event stream |

---

## `source.auth:` — authentication

When the remote runtime requires a bearer token, declare it under `auth:`. Exactly one of `token` or `secretRef` must be set — `ork validate` rejects both set together.

```yaml
source:
  host: "http://loader-runtime.loader-system:8080"
  auth:
    token: "$LOADER_TOKEN"    # ENV_VAR syntax supported
```

Or read the token from a Kubernetes Secret at startup:

```yaml
source:
  host: "http://loader-runtime.loader-system:8080"
  auth:
    secretRef:
      name: loader-cross-token
      namespace: loader-system
      key: token
```

| Field | Type | Description |
|-------|------|-------------|
| `token` | string | Bearer token. `$ENV_VAR` syntax resolves the value from the runtime's environment at startup. |
| `secretRef.name` | string | Kubernetes Secret name. |
| `secretRef.namespace` | string | Kubernetes Secret namespace. |
| `secretRef.key` | string | Key within the Secret whose value is the token. |

`secretRef` is read once at startup and held in memory. If the Secret changes, a runtime restart is required to pick up the new value.

---

## Same-binary vs cross-binary

When the target CRD is in the same Katalog, `source:` is unnecessary — the data comes from the informer cache with zero API calls:

```yaml
# Same binary — no source: needed
cross:
  - crd: database
    selector:
      name: "{{ .metadata.name }}-db"
    as: db
```

When the target is in a different binary, `source.host` routes the read to the remote runtime's live API. The result shape is the same either way — `.cross.db.*` works identically in templates regardless of where the data came from.

See [ONCOP](../../concepts/oncop/index.md) for the full cross-binary observation model.
