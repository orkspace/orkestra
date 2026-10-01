# pkg/types

`types` is the foundational contract package for the Orkestra control plane. It defines every shared struct, interface, and registry that the runtime, reconcilers, generators, and CLI tooling share. No other Orkestra package should duplicate a type that belongs here.

The package is large by design — centralising types avoids import cycles. Callers import `pkg/types` (aliased as `orktypes` by convention) and access everything they need from one import.

## File organisation

| File(s) | What they define |
|---------|-----------------|
| `types.go` | Core CRD entry shape — `CRDEntry`, `OperatorBox`, `HookTemplates` |
| `katalog.go`, `katalog_spec_providers.go` | `Katalog` and `Komposer` document shapes |
| `sources.go` | Per-resource template source structs (`DeploymentTemplateSource`, `ServiceTemplateSource`, …) |
| `foreach.go` | `ForEachSpec` — the `forEach` field shared by all template sources |
| `hook_temp.go`, `hooks_resources.go`, `hooks_probes.go`, `hooks_sleep.go` | Hook template structs and nested blocks |
| `hook_methods.go` | Methods on hook types (`UsesTemplates`, template detection helpers) |
| `methods.go` | Methods on `CRDEntry` (`IsEnabled`, `DefaultReconcile`, `ShouldEnrich`, …) |
| `registry.go` | Runtime type registries (`ObjectRegistry`, `ListRegistry`, `HookRegistry`, `ReconcilerRegistry`) |
| `status.go` | Status field declarations and `StatusField` shape |
| `conditions.go` | Condition helpers used by reconcilers |
| `enrichment.go` | `EnrichmentSpec` — the `enrich:` list on a CRD entry |
| `security.go` | Security policy types (`SecuritySpec`, deletion protection, namespace restriction) |
| `notification.go` | Notification policy types |
| `autoscale.go` | Worker autoscale policy |
| `secret_rotation.go` | Secret rotation policy |
| `when.go` | `WhenSpec` — conditional field expressions |
| `admission.go`, `conversion.go` | Webhook configuration types |
| `external.go` | External call types |
| `cross.go`, `cross_methods.go`, `cross_oncop.go` | Cross-CRD dependency types |
| `custom_resource.go` | Dynamic unstructured CR helpers |
| `func.go` | Function declaration types (hooks, constructors) |
| `normalize.go` | Field normalisation helpers |
| `ns_allowed.go`, `ns_restricted.go` | Namespace restriction helpers |
| `motif.go` | `Motif` document type |
| `e2e.go` | E2E document type |

## Convention

All callers import this package with the `orktypes` alias:

```go
import orktypes "github.com/orkspace/orkestra/pkg/types"
```

## Contributor docs

| Document | What it covers |
|---|---|
| [docs/hook-visitor-pattern.md](docs/hook-visitor-pattern.md) | How to write cross-hook validators using `VisitResources` + typed interfaces. Read this before adding a new field validator to `pkg/katalog/`. |
