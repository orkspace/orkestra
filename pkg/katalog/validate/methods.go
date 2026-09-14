package validate

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/orkspace/orkestra/pkg/children"
	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// -----------------------------------------------------------------------------
// Validation: Pretty error reporting
// -----------------------------------------------------------------------------

func (e *executor) handleValidationErrors(err error) {
	logger.Error().Msg("Validation failed")

	// Case 1: struct field validation errors (validator.v10)
	if errs, ok := err.(validator.ValidationErrors); ok {
		for _, e := range errs {
			fmt.Printf(
				"  • Field '%s' failed validation rule '%s'\n",
				e.Field(), e.Tag(),
			)
		}
		return
	}

	// Case 2: custom validation errors (like duration parsing)
	fmt.Printf("  • %s\n", err.Error())
}

// -----------------------------------------------------------------------------
// Validate uniqueness
// -----------------------------------------------------------------------------
func (e *executor) validateUniqueness() error {
	// Name uniqueness is guaranteed by map keys — only check GVK uniqueness.
	return e.validateGVKUniqueness()
}

// -----------------------------------------------------------------------------
// Validation: GVK uniqueness
// -----------------------------------------------------------------------------
func (e *executor) validateGVKUniqueness() error {
	seen := make(map[string]string) // key -> name

	for name, crd := range e.k.EnabledCRDs() {
		key := fmt.Sprintf("%s/%s/%s", crd.APITypes.Group, crd.APITypes.Version, crd.APITypes.Kind)

		if existing, ok := seen[key]; ok {
			return fmt.Errorf(
				"duplicate GVK detected: %s/%s, Kind=%s (CRDs: %s and %s)",
				crd.APITypes.Group, crd.APITypes.Version, crd.APITypes.Kind, existing, name,
			)
		}

		seen[key] = name
	}

	return nil
}

// -----------------------------------------------------------------------------
// Validation: dependsOn existence + cycle detection
// -----------------------------------------------------------------------------

func (e *executor) validateDependsOn() error {
	exists := make(map[string]bool, e.k.Len())
	for name := range e.k.EnabledCRDs() {
		exists[name] = true
	}

	for name, crd := range e.k.EnabledCRDs() {
		for dep := range crd.DependsOn {
			if dep == name {
				return fmt.Errorf("CRD '%s' cannot depend on itself", name)
			}
			if !exists[dep] {
				return fmt.Errorf("CRD '%s' depends on unknown or disabled CRD '%s'", name, dep)
			}
		}
	}

	return e.detectDependencyCycles()
}

// -----------------------------------------------------------------------------
// Cycle detection (DFS)
// -----------------------------------------------------------------------------

func (e *executor) detectDependencyCycles() error {
	graph := make(map[string][]string, e.k.Len())
	for name, crd := range e.k.EnabledCRDs() {
		graph[name] = crd.DependsOn.Names()
	}

	visited := make(map[string]bool)
	stack := make(map[string]bool)

	var dfs func(node string) error
	dfs = func(node string) error {
		if stack[node] {
			return fmt.Errorf("dependency cycle detected involving '%s'", node)
		}
		if visited[node] {
			return nil
		}

		visited[node] = true
		stack[node] = true

		for _, dep := range graph[node] {
			if err := dfs(dep); err != nil {
				return err
			}
		}

		stack[node] = false
		return nil
	}

	for name := range graph {
		if err := dfs(name); err != nil {
			return err
		}
	}

	return nil
}

// validateStatus sets IgnoreStatusPatch and IgnoreObservedGeneration on each
// enabled CRD entry based on the built-in resource registry.
//
// Called once during Katalog loading — flags are set once, checked cheaply
// in the hot reconcile path.
func (e *executor) validateStatus() {
	for name, crd := range e.k.EnabledCRDs() {
		// Look up by Kind directly — avoids GVK string format mismatch.
		// children.BuiltInMeta returns zero value for unknown kinds (safe).
		meta := children.BuiltInMeta(crd.APITypes.Kind)

		if meta.SkipStatusSubresource {
			crd.IgnoreStatusPatch = true
		}

		if meta.SkipObservedGeneration {
			crd.IgnoreObservedGeneration = true
		}

		e.k.EnabledCRDs()[name] = crd
	}
}

// validateAutoscalerMetrics ensures only supported metrics.* fields are used.
// This is a fail-fast mechanism to avoid runtime errors.
func (e *executor) validateAutoscalerMetrics() error {
	for _, crd := range e.k.EnabledCRDs() {
		if !crd.AutoscaleEnabled() {
			continue
		}

		conds := crd.OperatorBox.Autoscale.Conditions

		for _, c := range conds.Or {
			if strings.HasPrefix(c.Field, "metrics.") {
				if err := crd.ValidateMetricField(c.Field); err != nil {
					e.handleValidationErrors(err)
					return fmt.Errorf(failureMark(), err)
				}
			}
		}

		for _, c := range conds.When {
			if strings.HasPrefix(c.Field, "metrics.") {
				if err := crd.ValidateMetricField(c.Field); err != nil {
					e.handleValidationErrors(err)
					return fmt.Errorf(failureMark(), err)
				}
			}
		}
	}
	return nil
}

// validateNamespaceProtection enforces that each CRD declares either
// allowedNamespaces or restrictedNamespaces, but not both.
func (e *executor) validateNamespaceProtection() error {
	for name, crd := range e.k.EnabledCRDs() {
		if !crd.IsNamespaced() || !crd.HasNamespaceRules() {
			continue
		}
		if crd.AllowedNamespacesOnly() || crd.RestrictedNamespacesOnly() {
			continue
		}
		if crd.HasAllowedNamespaces() && crd.HasRestrictedNamespaces() {
			return fmt.Errorf(
				"%s CRD %q cannot define both allowedNamespaces and restrictedNamespaces — choose one",
				failureMark(), name,
			)
		}
	}
	return nil
}

// validateTimeDuration validates all rotation-related duration fields
// across all enabled CRDs.
func (e *executor) validateTimeDuration() error {
	for name, crd := range e.k.EnabledCRDs() {
		if !crd.HasAnyHookTemplates() {
			continue
		}

		for _, e := range crd.CollectSleepEntries() {
			if orktypes.IsTemplate(e.Duration) {
				continue
			}
			if _, err := parseTimeDuration(e.Duration); err != nil {
				return durationError(name, e.ResourceName, "sleep", e.Duration, err)
			}
		}

		if !crd.HasAnySecrets() {
			continue
		}
		if crd.HasOnCreate() {
			for _, s := range crd.OperatorBox.OnCreate.Secrets {
				if s.RotateAfter != "" {
					if _, err := parseTimeDuration(s.RotateAfter); err != nil {
						return durationError(name, s.Name, "rotateAfter", s.RotateAfter, err)
					}
				}
				if s.TLS != nil && s.TLS.ValidFor != "" {
					if _, err := parseTimeDuration(s.TLS.ValidFor); err != nil {
						return durationError(name, s.Name, "validFor", s.TLS.ValidFor, err)
					}
				}
			}
		}

		if crd.HasOnReconcile() {
			for _, s := range crd.OperatorBox.OnReconcile.Secrets {
				if s.RotateAfter != "" {
					if _, err := parseTimeDuration(s.RotateAfter); err != nil {
						return durationError(name, s.Name, "rotateAfter", s.RotateAfter, err)
					}
				}
				if s.TLS != nil && s.TLS.ValidFor != "" {
					if _, err := parseTimeDuration(s.TLS.ValidFor); err != nil {
						return durationError(name, s.Name, "validFor", s.TLS.ValidFor, err)
					}
				}
			}
		}
	}
	return nil
}

func durationError(crdName, secretName, field, value string, err error) error {
	return fmt.Errorf(
		"%s invalid duration %q in CRD %q (secret %q, field %q): %v\n\n"+
			"Allowed units:\n"+
			"  d   = days (24h)\n"+
			"  w   = weeks (7d)\n"+
			"  mo  = months (30d)\n"+
			"  y   = years (365d)\n\n"+
			"Examples: 30d, 2w, 3mo, 1y",
		failureMark(), value, crdName, secretName, field, err,
	)
}

// validateHPAReference ensures that every HPA declaration has a valid ScaleTargetRef.
func (e *executor) validateHPAReference() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		if !crd.HasAnyHookTemplates() || !crd.HasAnyHPA() {
			continue
		}

		if crd.HasOnCreate() {
			for _, h := range crd.OperatorBox.OnCreate.HorizontalPodAutoscalers {
				if err := validateOneHPARef(crdName, h.Name, h.ScaleTargetRef); err != nil {
					return fmt.Errorf(failureMark(), err)
				}
			}
		}

		if crd.HasOnReconcile() {
			for _, h := range crd.OperatorBox.OnReconcile.HorizontalPodAutoscalers {
				if err := validateOneHPARef(crdName, h.Name, h.ScaleTargetRef); err != nil {
					return fmt.Errorf(failureMark(), err)
				}
			}
		}
	}
	return nil
}

func validateOneHPARef(crdName, hpaName string, ref orktypes.ScaleTargetRef) error {
	appsV1Kinds := map[string]bool{"Deployment": true, "StatefulSet": true, "ReplicaSet": true}
	if ref.APIVersion == "" && appsV1Kinds[ref.Kind] {
		ref.APIVersion = "apps/v1"
	}
	if ref.APIVersion == "" {
		return fmt.Errorf(
			"%s invalid HPA ScaleTargetRef in CRD %q (hpa %q): missing apiVersion\n\n"+
				"Example:\n"+
				"  ScaleTargetRef:\n"+
				"    apiVersion: apps/v1\n"+
				"    kind: Deployment\n"+
				"    name: my-app",
			failureMark(), crdName, hpaName,
		)
	}
	if ref.Kind == "" {
		return fmt.Errorf(
			"%s invalid HPA ScaleTargetRef in CRD %q (hpa %q): missing kind\n\n"+
				"Example:\n"+
				"  ScaleTargetRef:\n"+
				"    apiVersion: apps/v1\n"+
				"    kind: ReplicaSet\n"+
				"    name: my-app",
			failureMark(), crdName, hpaName,
		)
	}
	if ref.Name == "" {
		return fmt.Errorf(
			"invalid HPA ScaleTargetRef in CRD %q (hpa %q): missing name\n\n"+
				"Example:\n"+
				"  ScaleTargetRef:\n"+
				"    apiVersion: apps/v1\n"+
				"    kind: StatefulSet\n"+
				"    name: my-app",
			crdName, hpaName,
		)
	}
	return nil
}

// validateStatusTypes ensures all declarative status fields declare a valid type.
func (e *executor) validateStatusTypes() error {
	for name, crd := range e.k.EnabledCRDs() {
		if crd.OperatorBox.Status == nil {
			continue
		}

		if crd.OperatorBox.Status.HasFields() {
			for _, f := range crd.OperatorBox.Status.Fields {
				switch strings.ToLower(f.Type) {
				case "", "string", "str", "default":
				case "int", "integer":
				case "bool", "boolean":
				case "float", "auto":
					// valid
				default:
					return fmt.Errorf(
						"%s invalid status field type %q in CRD %q (path: %q):\n"+
							"  must be one of: string, str, int, integer, bool, boolean, float, auto\n",
						failureMark(), f.Type, name, f.Path,
					)
				}
			}
		}
	}
	return nil
}

// validateTeams ensures that a team referenced in a notify: block was declared
// in notification.teams within this Katalog.
func (e *executor) validateTeams() error {
	if !e.k.HasNotification() {
		return nil
	}
	if !e.k.HasTeams() {
		return nil
	}

	for name := range e.k.EnabledCRDs() {
		if _, ok := e.k.Notification.Teams[name]; !ok {
			return fmt.Errorf("%s  %s team not found", failureMark(), name)
		}
	}
	return nil
}
