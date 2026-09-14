# pkg/katalog

The schema registry for an Orkestra operator. Parses the Katalog YAML, enriches each CRD entry with runtime metadata, and exposes the result as a queryable `*Katalog`. Nothing in Orkestra hardcodes a CRD — everything is driven by what the Katalog says.

## Sub-packages

| Package | Role |
|---------|------|
| [`validate/`](validate/README.md) | Full validation pipeline — all validators, `Execute` entry point |
| [`pipeline/`](pipeline/README.md) | Top-level build sequence — merge → validate → wire runtime objects |

## What the Katalog holds

| What | Accessor |
|------|----------|
| Enabled CRD entries (enriched, validated) | `k.EnabledCRDs()` |
| Katalog metadata (name, version, author) | `k.Metadata()` |
| Security configuration | `k.Security` |
| Dependency DAG | `NewDependencyGraph(k)` |
| Gateway configuration | `k.Gateway` |

## CRD entry lifecycle

Each `CRDEntry` goes through several phases before it is ready:

1. **Parse** — decoded from YAML via the merger.
2. **crdFile population** — if `crdFile:` is declared, `APITypes` is populated from the CRD YAML. Overwrites any inline `apiTypes:` block.
3. **Enrich** — fills computed fields (API path, plural, GVK). Built-in Kubernetes kinds use [`pkg/children`](../children/README.md) for group/version/plural.
4. **Validate** — handled by [`pkg/katalog/validate`](validate/README.md).
5. **Defaults** — workers, resync, namespace, finalizers.
6. **Runtime objects** — dynamic → `*unstructured.Unstructured`; typed → `ObjectRegistry` lookup.
7. **Reconcilers** — hooks from `HookRegistry`, constructors from `ReconcilerRegistry`.
8. **Status flags** — set from the built-in registry in [`pkg/children`](../children/README.md).

After this pipeline, `enabledCRDs` is fully prepared and never mutated again.

## Motif support

The package handles **Motif** imports — reusable infrastructure templates a Katalog can import instead of writing stateful resource declarations by hand.

| File | Role |
|------|------|
| `motif_imports.go` | `ResolveMotifImports` — expands `imports:` into concrete declarations |
| `motif_validate.go` | `ValidateMotif`, `ValidateMotifImports` — structural and semantic validation |

## Developer documentation

| I want to… | Go to |
|-----------|-------|
| Query the Katalog at runtime | [docs/01-querying.md](docs/01-querying.md) |
| Understand the dependency graph | [docs/02-dependencies.md](docs/02-dependencies.md) |
| Understand deletion protection | [docs/03-deletion-protection.md](docs/03-deletion-protection.md) |
| Understand security defaults | [docs/04-security.md](docs/04-security.md) |
| Understand built-in type handling | [docs/05-builtins.md](docs/05-builtins.md) |
| Understand Motif validation and import expansion | [docs/06-motif-validation.md](docs/06-motif-validation.md) |
| Understand crdFile — auto-populating APITypes from a CRD YAML | [docs/07-crd-file.md](docs/07-crd-file.md) |
