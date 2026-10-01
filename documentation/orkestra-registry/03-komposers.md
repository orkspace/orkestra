# Komposers

A Komposer is the **composition plane**: it declares where to pull operator patterns from and how to compose them into a running platform. Komposers are the only Pattern with a top-level `imports` block (registry, file, and Helm sources). Katalogs import only Motifs, and only at the CRD level — they cannot pull other Katalogs or Komposers.

---

## The `komposer.yaml` in a Katalog pattern

When you publish a Katalog pattern, you can include a `komposer.yaml` alongside it. This companion file ships as part of the same artifact and shows consumers how to use the pattern in a Komposer composition:

```text
postgres/
  katalog.yaml    # the operator definition (required)
  komposer.yaml   # optional — companion showing how to compose this pattern
  crd.yaml
  cr.yaml
  e2e.yaml
```

```bash
ork push postgres:v14 ./patterns/postgres/
```

The `komposer.yaml` is not a separate publish step — it travels with the Katalog. Consumers who want to load it instead of `katalog.yaml` set `useKomposer: true` in their import entry.

---

## Writing a Komposer
See full description in [Writing your first Komposer](../getting-started/04-writing-your-first-komposer.md).

---

## Two kinds of registry import

Every registry entry in your Komposer resolves either the `katalog.yaml` or the `komposer.yaml` from the upstream artifact. The `useKomposer` field controls which:

### `useKomposer: false` (default)

Orkestra loads `katalog.yaml` from the artifact. Only the CRD entries from `spec.crds` are extracted. You get the raw CRD definitions and compose them your own way:

```yaml
imports:
  registry:
    - url: ghcr.io/myorg/patterns/postgres:v14
      oci: true
      # useKomposer defaults to false → loads postgres/katalog.yaml
```

Use this when you want the upstream's CRD data but will apply your own overrides or composition inline.

### `useKomposer: true`

Orkestra loads `komposer.yaml` from the artifact instead. The upstream's composition — its imports, its defaults — is resolved in full. If the upstream `komposer.yaml` itself imports other patterns, those are resolved too:

```yaml
imports:
  registry:
    - url: ghcr.io/myorg/patterns/postgres:v14
      oci: true
      useKomposer: true
      # loads postgres/komposer.yaml — resolves all of its imports recursively
```

Use this when you want to accept the upstream's pre-composed form as-is.

Orkestra validates that the `kind:` field in the loaded file matches what `useKomposer` expects. If you set `useKomposer: true` but the file has `kind: Katalog`, the pull fails with a clear error.

---

## Overriding on import

Your inline `spec.crds` always wins over imported values, regardless of `useKomposer`. Environment-specific overrides require no changes to the upstream pattern:

```yaml
imports:
  registry:
    - url: postgres:v14
      oci: true
      useKomposer: true
spec:
  crds:
    database:
      workers: 10    # production override — takes precedence over upstream defaults
```

---

## Transitive imports

When you load an upstream `komposer.yaml` with `useKomposer: true`, its own registry, file, and Helm imports are also resolved. If the upstream composition pulls three other patterns, you get all three. Understand the upstream's dependency tree before enabling this in production.

---

## Where to go next

- **[Katalogs](./02-katalogs.md)** — publishing operator patterns
- **[E2E](./04-e2e.md)** — gating publication with declarative tests
- **[Schema: Komposer](../reference/schema/03-komposer/index.md)** — full field reference
