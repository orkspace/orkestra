# Katalog Patterns

A Katalog pattern is a complete operator declaration published to the registry. It defines one or more CRDs, their reconcile templates, status fields, admission rules, and any supporting files. The consumer imports it and gets a full operator — no Go required.

## Pattern directory

```text
postgres/
  katalog.yaml    # required — the operator declaration
  crd.yaml        # optional — CRD manifest to apply before the operator starts
  cr.yaml         # optional — sample CR for testing
  e2e.yaml        # optional — E2E expectations (gates publication)
  komposer.yaml   # optional — companion showing how to import this pattern in a Komposer
  README.md       # optional — shown in registry UI
```

`katalog.yaml` is the only required file. `crd.yaml` is optional because the consumer may already have the CRD installed, or may want to bring their own version — Orkestra does not force a CRD on import. Including it is recommended for patterns intended as a complete drop-in, but omitting it lets the consumer control the CRD independently. `cr.yaml` gives consumers a working sample, and `e2e.yaml` means the pattern was verified before it was published.

Including a `komposer.yaml` is optional but useful: it ships alongside the Katalog in the same artifact and shows consumers exactly how to compose this pattern. Consumers who want to load it instead of `katalog.yaml` set `useKomposer: true` in their import entry — see [Komposers](./03-komposers.md).

## Writing a Katalog
See full description in [Writing your first Katalog](../getting-started/03-writing-your-first-katalog.md).

## Publishing

```bash
ork push postgres:v14 ./patterns/postgres/
```

If `e2e.yaml` exists, the push runs it first. The pattern is only published if all expectations pass. See [E2E](04-e2e.md) for how this works.

To skip the gate:

```bash
ork push postgres:v14 ./patterns/postgres/ --force
```

The E2E result — whether it passed, was skipped, or was force-overridden — is baked into the OCI artifact as annotations. `ork inspect` and `ork patterns` show this status for every pattern.

## Importing

In a Komposer:

```yaml
imports:
  registry:
    - url: ghcr.io/orkspace/orkestra-registry/postgres@v14
      oci: true
```

Or using the short name (resolves against `ORK_REGISTRY`):

```yaml
imports:
  registry:
    - url: postgres:v14
```

## Overriding on import

The Komposer's inline `spec.crds` block always wins on conflict. Override any CRD-level setting without touching the upstream pattern:

```yaml
imports:
  registry:
    - url: postgres:v14
spec:
  crds:
    postgres:
      operatorBox:
        reconcile:
          workers: 8
```

This includes `apiTypes`. If the upstream pattern targets `demo.orkestra.io/v1alpha1` but you want the same reconcile behaviour against your own CRD group and version, override it directly:

```yaml
imports:
  registry:
    - url: postgres:v14
spec:
  crds:
    postgres:
      apiTypes:
        group: platform.myorg.io
        version: v1
        kind: PostgresCluster
        plural: postgresclusters
        object: PostgresCluster
```

The reconcile templates, status fields, and admission rules from the upstream Katalog are applied unchanged — only the CRD the operator watches is different. This lets a single community pattern serve multiple organisations with different API conventions.
