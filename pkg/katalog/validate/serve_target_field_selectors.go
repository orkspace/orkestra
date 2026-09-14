package validate

import (
	"fmt"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
)

// validateServeFieldSelector validates serve.target fieldSelector configuration.
//
// It enforces:
//  1. Each target has at most 3 field selectors — keep it simple, avoid overlapping.
//  2. Field selectors are unique across targets — no two targets can match the same CR.
//  3. Field selectors must exist in the CRD schema — avoid silent misrouting. (TODO)
//  4. If CR mode is disabled for a target, it must have at least one fieldSelector
//     — otherwise the target is unreachable via full CR mode.
//  5. Field selectors must be valid dot-notation paths (e.g., "spec.mealPlan").
func (e *executor) validateServeFieldSelector() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		if !crd.ServeEnabled() {
			continue
		}

		// Skip if no targets with fieldSelector
		if !crd.HasServeTargetFieldSelector() {
			continue
		}

		// Track all selectors for uniqueness (path:value combination)
		allSelectors := make(map[string]string) // "path:value" -> target name

		for targetName, cfg := range crd.Serve.Target.Entries {
			if !cfg.HasServeTargetFieldSelector() {
				continue
			}

			selector := cfg.FieldSelector
			maxFields := orktypes.MaxServeTargetFieldSelector

			// 1. Max 3 field selectors per target
			if len(selector) > maxFields {
				return fmt.Errorf(
					"%s CRD %q: target %q has %d field selectors — maximum is %d",
					failureMark(), crdName, targetName, len(selector), maxFields,
				)
			}

			// 2. Validate each field selector format and value
			for path, value := range selector {
				// Validate that there is no template in path or value
				pathIsTemplate := isTemplate(path)
				valueIsTemplate := isTemplate(value)

				if pathIsTemplate && valueIsTemplate {
					return fmt.Errorf(
						"%s CRD %q: target %q has invalid field selector %q: contains template syntax in path and value",
						failureMark(), crdName, targetName, path,
					)
				}

				if pathIsTemplate {
					return fmt.Errorf("%s CRD %q: target %q has invalid field selector path %q: contains template syntax in path",
						failureMark(), crdName, targetName, path)
				}

				if valueIsTemplate {
					return fmt.Errorf(
						"%s CRD %q: target %q has invalid field selector value %q: contains template syntax in value",
						failureMark(), crdName, targetName, value,
					)
				}

				// Validate dot-notation format
				if err := validateFieldSelectorPath(path); err != nil {
					return fmt.Errorf(
						"%s CRD %q: target %q has invalid field selector path %q: %w",
						failureMark(), crdName, targetName, path, err,
					)
				}

				// Validate value is not empty
				if strings.TrimSpace(value) == "" {
					return fmt.Errorf(
						"%s CRD %q: target %q has empty field selector value for path %q",
						failureMark(), crdName, targetName, path,
					)
				}

				// TODO: Validate path exists in CRD schema (best-effort)

				// Check uniqueness across targets (path:value must be unique)
				key := path + ":" + value
				if existingTarget, ok := allSelectors[key]; ok {
					return fmt.Errorf(
						"%s CRD %q: field selector %q=%q is used by both targets %q and %q — "+
							"field selectors must be unique across targets",
						failureMark(), crdName, path, value, existingTarget, targetName,
					)
				}
				allSelectors[key] = targetName
			}
		}

		// 3. Warn if a target has fieldSelector but CR mode is disabled
		for targetName, cfg := range crd.Serve.Target.Entries {
			if cfg.HasServeTargetFieldSelector() && !crd.FullCRModeEnabledFor(targetName) {
				crd.Warnings.AddWarning(fmt.Sprintf(
					"%s CRD %q: target %q has fieldSelector but CR mode is disabled — "+
						"fieldSelector will have no effect",
					warningMark(), crdName, targetName,
				))
			}
		}

		// Store the modified CRD back in the map
		e.k.EnabledCRDs()[crdName] = crd
	}
	return nil
}

// validateFieldSelectorPath checks that a field selector path is a valid dot-notation path.
func validateFieldSelectorPath(path string) error {
	if path == "" {
		return fmt.Errorf("field selector path cannot be empty")
	}

	if strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") {
		return fmt.Errorf("field selector path cannot start or end with a dot. Usage example: 'spec.mealPlan'")
	}

	parts := strings.Split(path, ".")
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("field selector path contains empty segment (double dot)")
		}
		if errs := validation.IsQualifiedName(part); len(errs) > 0 {
			return fmt.Errorf("field selector path segment %q is not a valid Kubernetes name: %s", part, strings.Join(errs, "; "))
		}
	}

	return nil
}
