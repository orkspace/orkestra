// pkg/katalog/motif_imports.go
//
// Two import paths:
//
//	spec.imports (Katalog-wide) — expandKatalogImports
//	  Merges profiles: and notes: from each Motif into the Katalog-wide registries.
//	  Resources, status, and admission in the Motif are ignored at this level.
//
//	spec.crds[name].imports (CRD-scoped) — expandMotifImports
//	  Merges resources, status, and admission into the target CRD.
//	  Profiles and notes in the Motif are ignored at this level.
package katalog

import (
	"fmt"
	"path/filepath"

	"github.com/orkspace/orkestra/pkg/registry"
	"github.com/orkspace/orkestra/pkg/registry/motif"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// expandKatalogImports resolves spec.imports entries on the Katalog.
// For each import, profiles: and notes: from the expanded Motif are merged
// into the Katalog-wide ProfileRegistry and NoteRegistry respectively.
// Resources, status, and admission are ignored at this level.
func (k *Katalog) expandKatalogImports() error {
	seen := make(map[string]string) // note name → motif label, for conflict detection
	for i, imp := range k.Spec.Imports {
		expanded, err := k.loadAndExpandImport(&imp)
		if err != nil {
			return fmt.Errorf("spec.imports[%d]: %w", i, err)
		}
		label := fmt.Sprintf("spec.imports[%d] motif %q", i, expanded.Name)
		if !expanded.Profiles.Empty() {
			merged, err := k.Profiles.Merge(expanded.Profiles, label)
			if err != nil {
				return fmt.Errorf("spec.imports[%d]: merging profiles from motif %q: %w", i, expanded.Name, err)
			}
			k.Profiles = merged
		}
		if !expanded.Notes.Empty() {
			merged, err := k.Notes.MergeImport(expanded.Notes, label, seen)
			if err != nil {
				return fmt.Errorf("spec.imports[%d]: merging notes from motif %q: %w", i, expanded.Name, err)
			}
			k.Notes = merged
		}
	}
	return nil
}

// expandMotifImports resolves all imports entries across enabled CRDs.
// For each import, it expands the motif and merges the result into the CRD entry.
// After expansion, the imports list is cleared (they have been inlined).
//
// Note: Imports are defined at the CRD level (spec.crds[].imports),
// not inside operatorBox. This allows motifs to contribute to multiple
// aspects of the CRD (resources, status, admission rules) without being
// tied to the operatorbox configuration.
func (k *Katalog) expandMotifImports() error {
	for name, entry := range k.enabledCRDs {
		if len(entry.Imports) == 0 {
			continue
		}

		for i, imp := range entry.Imports {
			expanded, err := k.loadAndExpandImport(&imp)
			if err != nil {
				return fmt.Errorf("CRD %q: operatorBox.imports[%d]: %w", name, i, err)
			}
			if err := k.mergeExpandedMotif(&entry, expanded); err != nil {
				return fmt.Errorf("CRD %q: operatorBox.imports[%d]: merging motif %q: %w",
					name, i, imp.Motif, err)
			}
			if !expanded.Profiles.Empty() {
				warning := fmt.Sprintf("CRD %q: import[%d] motif %q: profiles: are ignored at CRD-level imports — use spec.imports to apply profiles Katalog-wide",
					name, i, expanded.Name)
				entry.Warnings.AddWarning(warning)
				k.Warnings.AddWarning(warning)
			}
			if !expanded.Notes.Empty() {
				warning := fmt.Sprintf("CRD %q: import[%d] motif %q: notes: are ignored at CRD-level imports — use spec.imports to apply notes Katalog-wide",
					name, i, expanded.Name)
				entry.Warnings.AddWarning(warning)
				k.Warnings.AddWarning(warning)
			}
		}

		// Clear imports after successful expansion
		entry.Imports = nil
		k.enabledCRDs[name] = entry
	}
	return nil
}

// loadAndExpandImport loads the motif from its source (file, OCI, Git) and expands
// it using the provided bindings. Returns the expanded motif or an error.
//
// Relative file paths (./foo, ../foo, foo.yaml) are resolved against k.katalogDir
// so the path is stable regardless of which directory ork simulate / ork run is
// invoked from. This mirrors how crdFile paths are resolved in crdfile.go.
func (k *Katalog) loadAndExpandImport(imp *orktypes.MotifImport) (*motif.ExpandedMotif, error) {
	resolved := imp
	if k.katalogDir != "" && registry.IsFilePath(imp.Motif) && !filepath.IsAbs(imp.Motif) {
		copy := *imp
		copy.Motif = filepath.Join(k.katalogDir, imp.Motif)
		resolved = &copy
	}
	m, err := motif.LoadImport(resolved)
	if err != nil {
		return nil, fmt.Errorf("loading motif %q: %w", imp.Motif, err)
	}
	expanded, err := motif.Expand(m, imp.With)
	if err != nil {
		return nil, fmt.Errorf("expanding motif %q: %w", imp.Motif, err)
	}
	return expanded, nil
}

// mergeExpandedMotif merges the resources, status, and admission rules from an
// expanded motif into the target CRD entry. It respects the existing fields
// (appending rules, preserving order, and merging condition flags sensibly).
func (k *Katalog) mergeExpandedMotif(entry *orktypes.CRDEntry, expanded *motif.ExpandedMotif) error {
	// resources.onCreate: → CRD onCreate (update=false, preserves once: true guard)
	if expanded.OnCreate != nil {
		if !entry.HasOnCreate() {
			entry.OperatorBox.OnCreate = &orktypes.HookTemplates{}
		}
		entry.OperatorBox.OnCreate.MergeFrom(expanded.OnCreate)
	}
	// resources flat fields → CRD onReconcile (drift correction)
	if expanded.OnReconcile != nil {
		if !entry.HasOnReconcile() {
			entry.OperatorBox.OnReconcile = &orktypes.HookTemplates{}
		}
		entry.OperatorBox.OnReconcile.MergeFrom(expanded.OnReconcile)
	}

	// Merge status fields
	if expanded.HasStatus() {
		if !entry.HasStatusFields() {
			entry.OperatorBox.Status = &orktypes.StatusConfig{}
		}
		entry.OperatorBox.Status.Fields = append(entry.OperatorBox.Status.Fields, expanded.Status.Fields...)
		if entry.OperatorBox.Status.Conditions == nil && expanded.Status.Conditions != nil {
			entry.OperatorBox.Status.Conditions = expanded.Status.Conditions
		}
	}

	// Merge admission (validation + mutation) rules – these are at CRD level, not operatorBox
	if expanded.HasAdmission() {
		// Merge validation rules
		if expanded.Admission.HasValidationRules() {
			if entry.Validation == nil {
				entry.Validation = &orktypes.ValidationConfig{}
			}
			entry.Validation.Rules = append(entry.Validation.Rules, expanded.Admission.Validation.Rules...)
		}
		// Merge mutation rules
		if expanded.Admission.HasMutationRules() {
			if entry.Mutation == nil {
				entry.Mutation = &orktypes.MutationConfig{}
			}
			entry.Mutation.Rules = append(entry.Mutation.Rules, expanded.Admission.Mutation.Rules...)
		}
	}

	return nil
}
