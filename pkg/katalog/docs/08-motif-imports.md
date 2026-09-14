# 08 — Motif Import Levels

Motif imports in Orkestra operate at two distinct levels. Confusing them is the most common mistake when adding a new field that Motifs should contribute — the wiring compiles cleanly and the field silently does nothing.

---

## The two levels

| Import declaration | Where it lives | What it contributes |
|-------------------|---------------|---------------------|
| `spec.imports` | Top of `katalog.yaml` | **Profiles and notes only** — merged into the Katalog-wide `ProfileRegistry` and `NoteRegistry` |
| `spec.crds[name].imports` | Inside a CRD entry | **Resources, status, admission only** — merged into that CRD's `OperatorBox` |

The same Motif file can appear in both. A database Motif might contribute team-wide connection-string notes via `spec.imports` and the actual PVC/StatefulSet resources via `spec.crds[name].imports`.

---

## Why the split exists

Without it, a Katalog with three CRDs that each import the same shared Motif would attempt to merge the Motif's profiles three times — once per CRD. The first merge succeeds; the second and third hit a duplicate name error. Moving profiles and notes to `spec.imports` means they are resolved once at the Katalog level and the same registry is shared by all CRDs.

Resources are the inverse: they are CRD-specific by definition. A PostgreSQL Motif's PVC belongs to the Postgres CRD, not to the Redis CRD that happens to share the same team-notes Motif.

---

## Code path

Both levels call through `loadAndExpandImport` → `motif.LoadImport` → `motif.Expand`, which returns an `ExpandedMotif`:

```go
// pkg/registry/motif/expander.go
type ExpandedMotif struct {
    Name        string
    OnCreate    *orktypes.HookTemplates   // → spec.crds[name].imports only
    OnReconcile *orktypes.HookTemplates   // → spec.crds[name].imports only
    Status      *orktypes.StatusConfig    // → spec.crds[name].imports only
    Admission   *orktypes.Admission       // → spec.crds[name].imports only
    Notes       orktypes.NoteRegistry     // → spec.imports only
    Profiles    orktypes.ProfileRegistry  // → spec.imports only
}
```

The two expansion functions each consume exactly half of this struct and ignore the rest.

### `expandKatalogImports` (spec.imports)

Called in `KomposeRuntimeKatalog`, before `expandMotifImports`:

```go
// pkg/katalog/motif_imports.go
func (k *Katalog) expandKatalogImports() error {
    seen := make(map[string]string)
    for i, imp := range k.Spec.Imports {
        expanded, _ := k.loadAndExpandImport(&imp)
        // Profiles
        if !expanded.Profiles.Empty() {
            merged, _ := k.Profiles.Merge(expanded.Profiles, label)
            k.Profiles = merged
        }
        // Notes — conflict detection via seen map
        if !expanded.Notes.Empty() {
            merged, _ := k.Notes.MergeImport(expanded.Notes, label, seen)
            k.Notes = merged
        }
        // OnCreate, OnReconcile, Status, Admission — ignored here
    }
}
```

### `expandMotifImports` (spec.crds[name].imports)

Called after `expandKatalogImports`, once per enabled CRD:

```go
func (k *Katalog) expandMotifImports() error {
    for name, entry := range k.enabledCRDs {
        for i, imp := range entry.Imports {
            expanded, _ := k.loadAndExpandImport(&imp)
            k.mergeExpandedMotif(&entry, expanded)
            // mergeExpandedMotif merges OnCreate, OnReconcile, Status, Admission
            // Notes and Profiles on expanded — ignored here
        }
        entry.Imports = nil
    }
}
```

---

## Merger accumulation

Before either expansion function runs, the merger (`pkg/merger/file.go`) accumulates `spec.imports` entries and inline `notes:` / `profiles:` from every source (registry, file, helm):

```go
// loadKomposer — runs for each import source type
accSpecImports = append(accSpecImports, m.specImports...)
accNotes       = append(accNotes, m.notes...)
accProfiles, _ = accProfiles.Merge(m.profiles, srcLabel)

// end of loadKomposer — local file wins over imported
m.specImports = append(accSpecImports, doc.Spec.Imports...)
m.notes       = append(accNotes, doc.Notes...)
m.profiles, _ = accProfiles.Merge(doc.Profiles, path)
```

The merged result is then handed to the Katalog parser via `m.ToSpecImports()`, `m.ToNotes()`, and `m.ToProfiles()` before `expandKatalogImports` runs.

---

## Adding a new Katalog-wide field

If the new field should be shared across all CRDs (like profiles and notes):

1. Add the field to `orktypes.Motif` (`pkg/types/motif.go`)
2. Add the field to `ExpandedMotif` (`pkg/registry/motif/expander.go`) and populate it in `Expand()`
3. Add the field to `orktypes.NoteRegistry`-style type in `pkg/types/`
4. Add the `notes`-equivalent field to `Merger` (`pkg/merger/merger.go`) with a `To*()` method
5. Accumulate it in the three source blocks of `loadKomposer` in `pkg/merger/file.go`
6. Add it to `Katalog` struct (`pkg/katalog/type.go`) and wire from merger in `parser.go`
7. Expand it in `expandKatalogImports` (`pkg/katalog/motif_imports.go`)
8. Attach it to the `Resolver` via a `With*()` method (`pkg/template/resolver.go`)
9. Call `With*()` in `pkg/runtime/reconciler/generic.go` alongside `WithProfiles` and `WithUserNotes`

If the new field should be CRD-scoped (like resources, status, admission):

1. Steps 1–2 above
2. Merge it in `mergeExpandedMotif` (`pkg/katalog/motif_imports.go`)

---

## Common mistakes

**Wiring at the wrong level.** Adding a new field to `mergeExpandedMotif` when it should be in `expandKatalogImports` (or vice versa) compiles cleanly and produces no error — the field is simply never populated. If a new registry-type field is always empty at runtime, check which expansion function is consuming it.

**Forgetting the third accumulation block.** `loadKomposer` in `pkg/merger/file.go` has three source-type loops: registry, file, and helm. Accumulating a new field in registry and file but missing helm means Helm-sourced Katalogs silently drop the field. All three blocks must be updated together.

**Local-wins order.** The merger appends local declarations after accumulated imports: `append(accNotes, doc.Notes...)`. This means local entries come last in the slice. `Merge` / `MergeImport` must treat later-in-slice as lower-priority (or the local-wins contract breaks). Check that any new registry type follows this convention.
