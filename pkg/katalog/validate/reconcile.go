package validate

import (
	"fmt"

	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateReconciler validates all reconciler configuration
func (e *executor) validateReconciler() error {
	if err := e.validateReconcilerMode(); err != nil {
		return err
	}
	if err := e.validateQueue(); err != nil {
		return err
	}
	return nil
}

// -----------------------------------------------------------------------------
// Validation: Reconciler Mode (entrypoint)
// -----------------------------------------------------------------------------

// validateReconcilerMode determines the reconciler mode (dynamic/typed) for each
// CRD and performs consistency checks for hooks, constructors, and managed
// resources. Delegates to helper functions for clarity.
func (e *executor) validateReconcilerMode() error {
	for name, crd := range e.k.EnabledCRDs() {

		// Mode defaulting + basic validation
		if err := e.validateMode(name, &crd); err != nil {
			return err
		}

		// Hooks validation
		if err := e.validateHooks(name, &crd); err != nil {
			return err
		}

		// Constructor validation
		if err := e.validateConstructor(name, &crd); err != nil {
			return err
		}

		// Managed resources validation
		if err := e.validateManagedResources(name, &crd); err != nil {
			return err
		}

		// Save updated CRD entry
		e.k.EnabledCRDs()[name] = crd
	}

	return nil
}

// -----------------------------------------------------------------------------
// validateMode — defaulting + mode-level checks
// -----------------------------------------------------------------------------

func (e *executor) validateMode(name string, crd *orktypes.CRDEntry) error {
	mode := crd.Mode

	switch mode {
	case "":
		// Default mode
		if crd.APITypes.Location != "" {
			crd.Mode = orktypes.CRDModeTyped
			logger.Debug().
				Str("crd", name).
				Msg("reconciler mode defaulted to 'typed' because apiTypes.location is set")
		} else {
			crd.Mode = orktypes.CRDModeDynamic
			logger.Debug().
				Str("crd", name).
				Msg("reconciler mode defaulted to 'dynamic'")
		}

	case orktypes.CRDModeDynamic, orktypes.CRDModeTyped:
		// Valid modes

	default:
		return fmt.Errorf(
			"%s CRD %q: reconciler mode %q is not supported — use %q or %q",
			failureMark(), name, mode,
			orktypes.CRDModeDynamic,
			orktypes.CRDModeTyped,
		)
	}

	// Typed mode requires apiTypes.location
	if crd.Mode == orktypes.CRDModeTyped && crd.APITypes.Location == "" {
		return fmt.Errorf(
			"%s CRD %q: mode is 'typed' but apiTypes.location is missing",
			failureMark(), name,
		)
	}

	return nil
}

// -----------------------------------------------------------------------------
// validateHooks — structural + typed-mode requirements
// -----------------------------------------------------------------------------

func (e *executor) validateHooks(name string, crd *orktypes.CRDEntry) error {
	if !crd.WithHooksDecl() {
		return nil
	}

	// Hooks and constructor cannot both be declared
	if crd.WithConstructorDecl() {
		return fmt.Errorf(
			"%s CRD %q: cannot declare both 'hooks' and 'constructor' – choose one",
			failureMark(), name,
		)
	}

	// Typed CRD required
	if crd.APITypes.Location == "" {
		return fmt.Errorf(
			"%s CRD %q: hooks declared but apiTypes.location is missing (typed hooks require typed CRD)",
			failureMark(), name,
		)
	}

	// Required fields
	if crd.OperatorBox.Reconciler.Hooks.Location == "" {
		return fmt.Errorf("%s CRD %q: reconciler.hooks.location is required", failureMark(), name)
	}
	if crd.OperatorBox.Reconciler.Hooks.Function == "" {
		return fmt.Errorf("%s CRD %q: reconciler.hooks.function is required", failureMark(), name)
	}

	return nil
}

// -----------------------------------------------------------------------------
// validateConstructor — structural + typed-mode requirements
// -----------------------------------------------------------------------------

func (e *executor) validateConstructor(name string, crd *orktypes.CRDEntry) error {
	if !crd.WithConstructorDecl() {
		return nil
	}

	// Constructor and hooks cannot both be declared
	if crd.WithHooksDecl() {
		return fmt.Errorf(
			"%s CRD %q: cannot declare both 'hooks' and 'constructor' – choose one",
			failureMark(), name,
		)
	}

	// Typed CRD required
	if crd.APITypes.Location == "" {
		return fmt.Errorf(
			"%s CRD %q: constructor declared but apiTypes.location is missing (typed constructor requires typed CRD)",
			failureMark(), name,
		)
	}

	// Required fields
	if crd.OperatorBox.Reconciler.ConstructorDecl.Location == "" {
		return fmt.Errorf("%s CRD %q: reconciler.constructor.location is required", failureMark(), name)
	}
	if crd.OperatorBox.Reconciler.ConstructorDecl.Function == "" {
		return fmt.Errorf("%s CRD %q: reconciler.constructor.function is required", failureMark(), name)
	}

	return nil
}

// -----------------------------------------------------------------------------
// validateManagedResources — RBAC requirements for typed mode
// -----------------------------------------------------------------------------

func (e *executor) validateManagedResources(name string, crd *orktypes.CRDEntry) error {
	// Hooks declared but no resources
	if crd.WithHooksDecl() && !crd.WithHookManagedResources() {
		return fmt.Errorf(
			"%s CRD %q: hooks declared but no managed resources defined.\n\n"+
				"Typed hooks require RBAC permissions, which are generated from the\n"+
				"'resources' list under the hooks block.\n\n"+
				"Example:\n"+
				"  hooks:\n"+
				"    location: %s\n"+
				"    function: %s\n"+
				"    resources:\n"+
				"      - kind: Pod\n"+
				"      - kind: Deployment\n",
			failureMark(), name,
			crd.OperatorBox.Reconciler.Hooks.Location,
			crd.OperatorBox.Reconciler.Hooks.Function,
		)
	}

	// Constructor declared but no resources
	if crd.WithConstructorDecl() && !crd.WithConstructorManagedResources() {
		return fmt.Errorf(
			"%s CRD %q: constructor declared but no managed resources defined.\n\n"+
				"Typed constructors take full ownership of reconciliation and must\n"+
				"declare the Kubernetes resources they manage so RBAC can be generated.\n\n"+
				"Example:\n"+
				"  constructor:\n"+
				"    location: %s\n"+
				"    function: %s\n"+
				"    resources:\n"+
				"      - kind: StatefulSet\n"+
				"      - kind: Service\n",
			failureMark(), name,
			crd.OperatorBox.Reconciler.ConstructorDecl.Location,
			crd.OperatorBox.Reconciler.ConstructorDecl.Function,
		)
	}

	return nil
}

// -----------------------------------------------------------------------------
// validateQueue
// -----------------------------------------------------------------------------

// queue.behaviour has 2 knobs: onLimit and onThreshold
// Rules:
//   - No Queue behaviour allowed if queue is unlimited
//   - No value for onLimit declaration
//   - onThreshold without value is a hard error
//   - onThreshold.value must be between 1 and 100
//   - Warn if onThreshold is declared and drop is false (always true)
func (e *executor) validateQueue() error {
	if e.k.Empty() {
		return nil
	}
	for name, crd := range e.k.EnabledCRDs() {
		q := crd.QueueConfig()
		if q.Empty() {
			continue
		}

		if q.HasBehaviour() {
			if q.IsUnlimited() {
				return fmt.Errorf("%s CRD %q: (unlimited queue - maxDepth = 0): 'queue.behaviour' configuration is only valid when 'queue.maxDepth' is greater than 0. Consider increasing maxDepth",
					failureMark(), name)
			}
			cfg := q.Behaviour()
			if cfg.HasOnLimit() {
				if cfg.OnLimit.Value > 0 {
					return fmt.Errorf("%s CRD %q: 'value' is not allowed in onLimit configuration 'behaviour.onLimit.value' - %v",
						failureMark(), name, cfg.OnLimit.Value)
				}
			}

			if cfg.HasOnThreshold() {
				if !cfg.OnThreshold.HasValue() {
					return fmt.Errorf("%s CRD %q: 'value' is required in onThreshold configuration - (eg: 70)",
						failureMark(), name)
				}
				if !cfg.OnThreshold.ShouldDrop() {
					crd.Warnings.AddWarning("disabling drop when onThreshold is declared is redundant. Drop will be ignored")
				}

				cfg.OnThreshold.Drop = boolPtr(true)
			}
		}

		e.k.EnabledCRDs()[name] = crd
	}

	return nil
}
