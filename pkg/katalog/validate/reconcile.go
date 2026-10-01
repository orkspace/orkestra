package validate

import (
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/children"
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

		// Remote reconciler validation
		if err := e.validateRemote(name, &crd); err != nil {
			return err
		}

		// Managed resources validation
		if err := e.validateManagedResources(name, &crd); err != nil {
			return err
		}

		// onDelete group name uniqueness
		if err := validateOnDeleteGroupNames(name, &crd); err != nil {
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
	if crd.Box().Reconcile.Hooks.Location == "" {
		return fmt.Errorf("%s CRD %q: reconciler.hooks.location is required", failureMark(), name)
	}
	if crd.Box().Reconcile.Hooks.Function == "" {
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
	if crd.Box().Reconcile.ConstructorDecl.Location == "" {
		return fmt.Errorf("%s CRD %q: reconciler.constructor.location is required", failureMark(), name)
	}
	if crd.Box().Reconcile.ConstructorDecl.Function == "" {
		return fmt.Errorf("%s CRD %q: reconciler.constructor.function is required", failureMark(), name)
	}

	return nil
}

// -----------------------------------------------------------------------------
// validateRemote — structural checks for remote reconciler declarations
// -----------------------------------------------------------------------------

func (e *executor) validateRemote(name string, crd *orktypes.CRDEntry) error {
	if !crd.WithRemoteDecl() {
		return nil
	}

	// Remote and hooks cannot both be declared
	if crd.WithHooksDecl() {
		return fmt.Errorf(
			"%s CRD %q: cannot declare both 'hooks' and 'remote' – choose one",
			failureMark(), name,
		)
	}

	// Remote and constructor cannot both be declared
	if crd.WithConstructorDecl() {
		return fmt.Errorf(
			"%s CRD %q: cannot declare both 'constructor' and 'remote' – choose one",
			failureMark(), name,
		)
	}

	remote := crd.Box().Reconcile.Remote

	// Endpoint is required (HasRemoteDecl guards this, but be explicit)
	if remote.Endpoint == "" {
		return fmt.Errorf("%s CRD %q: reconcile.remote.endpoint is required", failureMark(), name)
	}

	funcMap := buildFuncMapForValidation(e.k.Notes)
	// If the endpoint contains template expressions, validate them.
	if isTemplate(remote.Endpoint) {
		if err := validateTemplate("reconcile.remote", name, name, "endpoint", remote.Endpoint, funcMap); err != nil {
			return err
		}
	}

	// Args — validate template expressions in values
	if len(remote.Args) > 0 {
		for k, v := range remote.Args {
			s, ok := v.(string)
			if !ok || !isTemplate(s) {
				continue
			}
			field := fmt.Sprintf("reconcile.remote.args.%s", k)
			if err := validateTemplate("reconcile.remote.args", name, name, field, s, funcMap); err != nil {
				return err
			}
		}
	}

	// Auth secretRef — name and key required; namespace warning if empty
	if remote.Auth != nil && remote.Auth.SecretRef != nil {
		context := fmt.Sprintf("CRD %q: reconcile.remote.auth", name)
		if err := validateSecretRefWithCRDWarning(remote.Auth.SecretRef, context, &crd.Warnings); err != nil {
			return err
		}
	}

	// Protocol must be valid when set
	if remote.Protocol != "" && !orktypes.IsValidRemoteReconcileProtocol(remote.Protocol.String()) {
		return fmt.Errorf(
			"%s CRD %q: reconcile.remote.protocol %q is not valid — valid values: %s",
			failureMark(), name, remote.Protocol,
			strings.Join(orktypes.ValidRemoteReconcileProtocols(), ", "),
		)
	}

	if err := e.validateRemotePayload(name, crd, remote); err != nil {
		return err
	}

	return nil
}

// validateRemotePayload validates payload.object.exclude and payload.children.
func (e *executor) validateRemotePayload(name string, crd *orktypes.CRDEntry, remote *orktypes.RemoteReconcilerDeclaration) error {
	if remote.Payload == nil {
		return nil
	}

	// payload.object.exclude — dot-notation paths
	if remote.Payload.Object != nil {
		for i, p := range remote.Payload.Object.Exclude {
			if err := validatePathFormat(p); err != nil {
				return fmt.Errorf(
					"%s CRD %q: reconcile.remote.payload.object.exclude[%d] %q: %w",
					failureMark(), name, i, p, err,
				)
			}
		}
	}

	ch := remote.Payload.EffectiveChildren()
	if ch == nil {
		return nil // children injection explicitly disabled
	}

	// Build the set of declared managed resource type names for membership checks.
	// Built-ins are resolved through the registry; custom resources use mr.Kind directly.
	managedTypes := make(map[string]bool, len(crd.RemoteManagedResources()))
	for _, mr := range crd.RemoteManagedResources() {
		res := children.LookupBuiltIn(mr.Kind)
		if res.Found() {
			managedTypes[strings.ToLower(res.Kind())] = true
		} else if mr.Kind != "" {
			managedTypes[strings.ToLower(mr.Kind)] = true
		}
	}

	// payload.children.resources — keys must match a declared managed resource type.
	for typeName, resCfg := range ch.Resources {
		if len(managedTypes) > 0 && !managedTypes[typeName] {
			return fmt.Errorf(
				"%s CRD %q: reconcile.remote.payload.children.resources: %q is not declared in reconcile.remote.resources — declare it there first",
				failureMark(), name, typeName,
			)
		}
		// Per-resource exclude — dot-notation paths
		if resCfg == nil {
			continue
		}
		for i, p := range resCfg.Exclude {
			if err := validatePathFormat(p); err != nil {
				return fmt.Errorf(
					"%s CRD %q: reconcile.remote.payload.children.resources[%q].exclude[%d] %q: %w",
					failureMark(), name, typeName, i, p, err,
				)
			}
		}
	}

	// payload.children.exclude (root) — dot-notation paths
	for i, p := range ch.Exclude {
		if err := validatePathFormat(p); err != nil {
			return fmt.Errorf(
				"%s CRD %q: reconcile.remote.payload.children.exclude[%d] %q: %w",
				failureMark(), name, i, p, err,
			)
		}
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
			crd.Box().Reconcile.Hooks.Location,
			crd.Box().Reconcile.Hooks.Function,
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
			crd.Box().Reconcile.ConstructorDecl.Location,
			crd.Box().Reconcile.ConstructorDecl.Function,
		)
	}

	// Remote with no managedResources is valid — the service may have no Kubernetes footprint.
	// Surface as info so ork validate makes it visible without blocking.
	if crd.WithRemoteDecl() && !crd.WithRemoteManagedResources() {
		crd.Info.AddInfo(fmt.Sprintf(
			"CRD %q: reconcile.remote has no managedResources — RBAC will not be generated for this reconciler",
			name,
		))
	}

	// Custom managed resources (not in the built-in registry) must declare kind
	// so GVK resolution and children injection work correctly.
	for _, mr := range crd.AllManagedResources() {
		if children.IsBuiltIn(mr.Kind) || children.IsBuiltIn(mr.Plural) {
			continue
		}
		if mr.Kind == "" {
			return fmt.Errorf(
				"%s CRD %q: custom managed resource %q must declare 'kind' — "+
					"built-in types are resolved automatically but custom CRDs require an explicit kind",
				failureMark(), name, mr.Plural,
			)
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// validateOnDeleteGroupNames checks that named groups within onDelete.groups are unique.
func validateOnDeleteGroupNames(crdName string, crd *orktypes.CRDEntry) error {
	t := crd.Box().EffectiveOnDelete()
	if t == nil || len(t.Groups) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(t.Groups))
	for i, g := range t.Groups {
		if g.Name == "" {
			continue
		}
		if _, exists := seen[g.Name]; exists {
			return fmt.Errorf(
				"%s CRD %q: onDelete.groups[%d].name %q is not unique — group names must be distinct",
				failureMark(), crdName, i, g.Name,
			)
		}
		seen[g.Name] = struct{}{}
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
