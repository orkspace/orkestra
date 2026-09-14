# 07 — crdFile: Auto-populating APITypes from a CRD YAML

## What it does

When a Katalog CRD entry declares `crdFile:`, Orkestra reads the referenced CRD YAML
and extracts `group`, `version`, `kind`, and `plural` from it automatically. The
`apiTypes:` block becomes optional for custom CRDs — the CRD file itself is the source
of truth.

```yaml
spec:
  crds:
    website:
      crdFile: ./crd.yaml   # apiTypes populated automatically from this file
      workers: 2
      operatorBox:
        # ...
```

## Where it fits in the lifecycle

crdFile population runs in `KomposeRuntimeKatalog`, **before** `EnrichCRDEntry` is
called. By the time any validation step runs, `APITypes` is fully populated.

```
merger.Merge()
  ↓
KomposeRuntimeKatalog
  ├─ populateAPITypesFromCRDFile (crdfile.go) ← reads CRD YAML, fills APITypes
  └─ EnrichCRDEntry (enrichment.go)           ← sees fully-specified APITypes, skips built-in lookup
  ↓
Validate
  ├─ setGroupVersionKind                      ← APITypes already set, just copies to GVK fields
  └─ ...
```

## Source of truth rules

- If only `crdFile:` is declared → APITypes come entirely from the CRD YAML.
- If both `apiTypes:` and `crdFile:` are declared → `crdFile` wins on `group`, `version`,
  `kind`, `plural`. Typed-mode fields (`object`, `objectList`, `alias`, `location`) come
  from the inline `apiTypes:` declaration and are preserved.
- Remote URLs (`http://`, `https://`) are passed through unchanged — kubectl handles them
  at runtime.

## Version selection

When a CRD declares multiple versions, `populateAPITypesFromCRDFile` picks:

1. The version with `storage: true` (canonical API version)
2. The first version with `served: true` (fallback)
3. The first version in the list (last resort)

## Dev-mode only

`crdFile` auto-applies the CRD at `ork run` startup via `applyCRDFilesIfNeeded` in
`cmd/cli/run_dev.go`. This function is behind `//go:build !runtime` and only runs when
`!utils.IsRunningInCluster()` — it never executes inside a Helm-deployed Orkestra.

For Helm-based deployments, the CRD must be pre-installed (`kubectl apply -f crd.yaml`)
before deploying the Orkestra chart. The `crdFile` field is still valid in the Katalog
(APITypes are still populated from it) — only the auto-apply step is skipped.

## Implementation

| File | Role |
|------|------|
| `pkg/katalog/crdfile.go` | `populateAPITypesFromCRDFile`, `readAPITypesFromCRDFile`, `selectCRDVersion` |
| `pkg/katalog/parser.go` | calls `populateAPITypesFromCRDFile` in the enrichment loop in `KomposeRuntimeKatalog` |
| `cmd/cli/run_dev_apply.go` | `applyCRDFilesIfNeeded` — applies CRD files before starting reconcilers (dev only) |
| `pkg/merger/merger.go` | `FirstEntryDir()` — returns the directory of the first entry point for relative path resolution |

→ Back to: [README.md](../README.md)
