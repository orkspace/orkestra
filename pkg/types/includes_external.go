package types

import (
	"fmt"
	"path/filepath"
)

// ExpandExternalCalls resolves include entries in a []ExternalCallSpec list.
// An entry with include: set is replaced in-place by the "calls:" list from the
// referenced file. Entries without include: are kept as-is.
// The include path is resolved relative to baseDir.
func ExpandExternalCalls(calls []ExternalCallSpec, baseDir string) ([]ExternalCallSpec, error) {
	var expanded []ExternalCallSpec
	for _, call := range calls {
		if call.Include == "" {
			expanded = append(expanded, call)
			continue
		}
		path := call.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("reading external include %q: %w", call.Include, err)
		}
		var f struct {
			Calls []ExternalCallSpec `yaml:"calls"`
		}
		if err := strictUnmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing external include %q: %w", call.Include, err)
		}
		expanded = append(expanded, f.Calls...)
	}
	return expanded, nil
}

func PopulateExternalCallsFromInclude(entry *CRDEntry, katalogDir string) error {
	var err error
	box := entry.OperatorBox

	if box.PreReconcile != nil {
		pr := box.PreReconcile
		pr.External, err = ExpandExternalCalls(box.PreReconcile.External, katalogDir)
		if err != nil {
			return fmt.Errorf("preReconcile.external: %w", err)
		}
		if pr.HasEnqueueGate() {
			pr.EnqueueGate.External, err = ExpandExternalCalls(pr.EnqueueGate.External, katalogDir)
			if err != nil {
				return fmt.Errorf("preReconcile.enqueueGate.external: %w", err)
			}
		}
		if pr.HasReconcileGate() {
			pr.ReconcileGate.External, err = ExpandExternalCalls(pr.ReconcileGate.External, katalogDir)
			if err != nil {
				return fmt.Errorf("preReconcile.reconcileGate.external: %w", err)
			}
		}
	}

	if box.OnReconcile != nil {
		box.OnReconcile.External, err = ExpandExternalCalls(box.OnReconcile.External, katalogDir)
		if err != nil {
			return fmt.Errorf("onReconcile.external: %w", err)
		}
	}
	if box.OnCreate != nil {
		box.OnCreate.External, err = ExpandExternalCalls(box.OnCreate.External, katalogDir)
		if err != nil {
			return fmt.Errorf("onCreate.external: %w", err)
		}
	}
	if r := box.Reconciler; r != nil && r.Hooks != nil {
		r.Hooks.External, err = ExpandExternalCalls(r.Hooks.External, katalogDir)
		if err != nil {
			return fmt.Errorf("hooks.external: %w", err)
		}
	}
	if entry.Validation != nil {
		entry.Validation.External, err = ExpandExternalCalls(entry.Validation.External, katalogDir)
		if err != nil {
			return fmt.Errorf("validation.external: %w", err)
		}
	}
	if entry.Mutation != nil {
		entry.Mutation.External, err = ExpandExternalCalls(entry.Mutation.External, katalogDir)
		if err != nil {
			return fmt.Errorf("mutation.external: %w", err)
		}
	}
	return nil
}
