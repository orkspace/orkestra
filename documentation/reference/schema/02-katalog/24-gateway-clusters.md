# gateway.clusters

Registers named remote clusters so the gateway can route intents to them.
Each entry holds the cluster's API server endpoint and one credential form.

Requires `gateway.enabled: true` at the Katalog level.

```yaml
gateway:
  enabled: true
  clusters:
    include: ./clusters.yaml    # optional — load entries from an external file
    prod:
      endpoint: https://prod.internal:6443
      secretRef:
        name: prod-credentials
        key: kubeconfig
    staging:
      endpoint: https://staging.internal:6443
      tokenRef:
        name: staging-sa-token
        key: token
      caRef:
        name: staging-ca
        key: ca.crt
    dev:
      endpoint: https://dev.internal:6443
      tokenRef:
        name: dev-sa-token
        key: token
      insecure: true
```

## Top-level fields

| Field | Description |
|-------|-------------|
| `include` | Path (relative to the Katalog file) to a YAML file containing a top-level `clusters:` map. Inline entries are merged on top — inline wins on name collision. |
| `<name>` | A named cluster entry. Names are the routing identifiers used in `serve.cluster` and `target.cluster`. |

## Cluster entry fields

| Field | Required | Description |
|-------|----------|-------------|
| `endpoint` | yes | The cluster's API server URL, e.g. `https://prod.internal:6443`. |
| `secretRef` | — | Kubeconfig credential form. Mutually exclusive with `tokenRef`/`caRef`. |
| `tokenRef` | — | Bearer token credential form. Mutually exclusive with `secretRef`. |
| `caRef` | — | CA certificate for TLS verification. Required with `tokenRef` unless `insecure: true`. |
| `insecure` | — | Skip TLS verification. Only valid with `tokenRef`. Default `false`. |

## Credential forms

Exactly one credential form must be declared per cluster entry.

### `secretRef` — kubeconfig

The secret holds a kubeconfig file. The gateway uses it to build a client at startup, honouring whatever cluster and user the kubeconfig points to.

```yaml
prod:
  endpoint: https://prod.internal:6443
  secretRef:
    name: prod-credentials
    key: kubeconfig
    namespace: orkestra-system   # optional — defaults to Orkestra's namespace
```

`insecure` is not valid with `secretRef` — the kubeconfig manages TLS settings internally.

### `tokenRef` + `caRef` — bearer token

The gateway constructs a client using a bearer token and CA certificate read separately from Secrets.

```yaml
staging:
  endpoint: https://staging.internal:6443
  tokenRef:
    name: staging-sa-token
    key: token
  caRef:
    name: staging-ca
    key: ca.crt
```

Use `ork clusters bootstrap` to provision the ServiceAccount and Secrets automatically.

### `tokenRef` + `insecure` — bearer token without TLS verification

For development clusters where TLS is not configured:

```yaml
dev:
  endpoint: https://dev.internal:6443
  tokenRef:
    name: dev-sa-token
    key: token
  insecure: true
```

## `secretRef` and `tokenRef` / `caRef` fields

All three use the same `APISecretRef` shape:

| Field | Description |
|-------|-------------|
| `name` | Kubernetes Secret name. Required. |
| `key` | Key within the Secret's `data` map. Required. |
| `namespace` | Secret namespace. Defaults to Orkestra's own namespace. |

## `include` file format

```yaml
# clusters.yaml — referenced via gateway.clusters.include
clusters:
  prod:
    endpoint: https://prod.internal:6443
    secretRef:
      name: prod-credentials
      key: kubeconfig
  staging:
    endpoint: https://staging.internal:6443
    tokenRef:
      name: staging-sa-token
      key: token
    caRef:
      name: staging-ca
      key: ca.crt
```

The file must have a top-level `clusters:` key. Entries from the include file and inline entries are merged — inline entries take precedence on name collision.

## Validation

`ork validate` checks the following at validate time:

- `endpoint` is required per entry
- Exactly one credential form must be declared
- `secretRef` and `tokenRef`/`caRef` are mutually exclusive
- `secretRef`: `name` and `key` required; `insecure` not allowed
- `tokenRef`: `name` and `key` required; `caRef` required unless `insecure: true`
- `caRef`: `name` and `key` required
- Static `serve.cluster` and `target.cluster` values are checked against registered cluster names
- Template `serve.cluster` and `target.cluster` expressions are validated against the full user-defined funcMap

## CLI

```bash
# List all registered clusters
ork clusters

# Validate offline (credential forms, serve.cluster references)
ork clusters validate

# Show which CRDs route to each cluster
ork clusters validate --full

# Connect to each cluster and verify CRDs are installed
ork clusters check

# Check a specific cluster only
ork clusters check --clusters staging

# Provision a new cluster's credentials automatically
ork clusters bootstrap --context kind-prod --name prod
```

See [ork clusters bootstrap](../../cli/clusters.md) for the full onboarding workflow.

## Where to go next

- [`serve.cluster`](20-serve.md) — default cluster for a CRD entry
- [`target.cluster`](21-serve-nested-spec.md) — per-target cluster override
- [Multi-cluster routing](../../../concepts/self-service/10-multi-cluster-routing.md) — concept overview
