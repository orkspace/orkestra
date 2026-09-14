# pkg/katalog/pipeline

Owns the top-level Katalog build sequence. Import this package when you need a fully ready `*katalog.Katalog` — merged, enriched, validated, and runtime-wired.

## Functions

**`NewKatalog(kfg, m)`** — runtime path. Calls `utils.Exit` on any error. Used by the operator runtime startup.

**`BuildExpanded(kfg, m)`** — CLI path. Returns an error instead of exiting. Used by `ork validate`, `ork serve validate`, and similar commands.

## Sequence

```
merger.Merger
    ↓
KomposeRuntimeKatalog    — decode YAML, enrich CRD entries, populate APITypes
    ↓
validate.Execute         — full validation pipeline
    ↓
CheckDeprecationPolicy   — runtime policy checks (NewKatalog only)
    ↓
UpdateResourceMapAndReturn — build GVK → type index
    ↓
*katalog.Katalog (ready)
```

## Package boundaries

- `pkg/katalog` — pure layer: struct definition, lookup methods, type methods. Never imports sub-packages.
- `pkg/katalog/validate` — all validators + `Execute`. Imports `pkg/katalog`.
- `pkg/katalog/pipeline` — this package. Imports both. Owns the build sequence.
