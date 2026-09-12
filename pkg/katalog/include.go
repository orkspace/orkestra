package katalog

import (
	"fmt"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

func populateAllServeFieldsFromInclude(entry *orktypes.CRDEntry, katalogDir string) error {
	if err := orktypes.ExpandServeInclude(entry.Serve, katalogDir); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	if err := orktypes.ExpandServeTargetShorthand(entry.Serve); err != nil {
		return fmt.Errorf("serve.target: %w", err)
	}
	if err := orktypes.ExpandServeTargetIncludes(entry.Serve, katalogDir); err != nil {
		return fmt.Errorf("serve.target: %w", err)
	}
	return nil
}

func populateStatusFieldsFromInclude(entry *orktypes.CRDEntry, katalogDir string) error {
	if err := orktypes.ExpandStatusInclude(entry.OperatorBox.Status, katalogDir); err != nil {
		return fmt.Errorf("status: %w", err)
	}
	return nil
}

func populateValidationRulesFromInclude(entry *orktypes.CRDEntry, katalogDir string) error {
	if err := orktypes.ExpandValidationInclude(entry.Validation, katalogDir); err != nil {
		return fmt.Errorf("validation: %w", err)
	}
	return nil
}

func populateMutationRulesFromInclude(entry *orktypes.CRDEntry, katalogDir string) error {
	if err := orktypes.ExpandMutationInclude(entry.Mutation, katalogDir); err != nil {
		return fmt.Errorf("mutation: %w", err)
	}
	return nil
}

func populateConversionPathsFromInclude(entry *orktypes.CRDEntry, katalogDir string) error {
	if err := orktypes.ExpandConversionInclude(entry.Conversion, katalogDir); err != nil {
		return fmt.Errorf("conversion: %w", err)
	}
	return nil
}

func populateObserveInclude(entry *orktypes.CRDEntry, katalogDir string) error {
	if entry.OperatorBox.Observe != nil {
		if err := orktypes.ExpandObserveInclude(entry.OperatorBox.Observe, katalogDir); err != nil {
			return fmt.Errorf("operatorBox.observe: %w", err)
		}
	}

	if entry.Serve == nil {
		return nil
	}

	for name, cfg := range entry.Serve.Target.Entries {
		if cfg == nil || cfg.OperatorBox == nil || cfg.OperatorBox.Observe == nil {
			continue
		}

		if err := orktypes.ExpandObserveInclude(cfg.OperatorBox.Observe, katalogDir); err != nil {
			return fmt.Errorf("serve.target[%q].operatorBox.observe: %w", name, err)
		}
	}

	return nil
}

func populateReconcilerFromInclude(entry *orktypes.CRDEntry, katalogDir string) error {
	if err := orktypes.ExpandReconcilerInclude(entry.OperatorBox.Reconciler, katalogDir); err != nil {
		return fmt.Errorf("operatorBox.reconciler: %w", err)
	}
	if entry.Serve == nil {
		return nil
	}
	for name, cfg := range entry.Serve.Target.Entries {
		if cfg == nil || cfg.OperatorBox == nil {
			continue
		}
		if err := orktypes.ExpandReconcilerInclude(cfg.OperatorBox.Reconciler, katalogDir); err != nil {
			return fmt.Errorf("serve.target[%q].operatorBox.reconciler: %w", name, err)
		}
	}
	return nil
}
