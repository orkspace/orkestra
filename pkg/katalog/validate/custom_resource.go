package validate

import (
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/children"
	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateCustomResources validates any Custom resources declared under the
// OperatorBox hooks (onCreate/onReconcile/onDelete).
// This function delegates per-item structural checks to types.validateCustomResource
// which enforces apiVersion/kind/metadata/name/namespace/labels/annotations/template
// rules and the namespaced defaulting semantics.
func (e *executor) validateCustomResources() error {
	for name, crd := range e.k.EnabledCRDs() {
		// Fast path: nothing to validate if constructor is declared (reconciler.default: false).
		if crd.WithConstructorDecl() {
			logger.Debug().
				Str("crd", name).
				Msg("skipping custom resource validation: constructor declared")
			continue
		}

		if !crd.HasAnyHookTemplates() {
			logger.Debug().
				Str("crd", name).
				Msg("skipping custom resource validation: no hook templates")
			continue
		}

		// Fast path: if the CRD doesn't declare any custom resources, skip.
		if !crd.HasAnyCustomResources() {
			logger.Debug().
				Str("crd", name).
				Msg("no custom resources declared in onCreate/onReconcile; skipping validation")
			continue
		}

		// Validate OnCreate custom resources
		if crd.HasOnCreate() && crd.OperatorBox.OnCreate.CustomResource != nil {
			for i, cr := range crd.OperatorBox.OnCreate.CustomResource {
				path := fmt.Sprintf("%s.onCreate.custom[%d]", name, i)
				// Delegate structural validation to the types package.
				if err := ValidateCustomResource(&crd, &cr, path); err != nil {
					return err
				}

				// Additional katalog-level checks (defensive):
				// - If the declaration explicitly marks cluster-scoped but provided a namespace, error.
				if cr.Metadata.Namespaced != nil && !*cr.Metadata.Namespaced && cr.Metadata.Namespace != "" {
					return fmt.Errorf("%s %s: metadata.namespaced=false but metadata.namespace is set (cluster-scoped resources must not set namespace)", failureMark(), path)
				}

				// Log a debug line for observability
				logger.Debug().
					Str("crd", name).
					Str("path", path).
					Msg("validated onCreate custom resource declaration")
			}
		}

		// Validate OnReconcile custom resources
		if crd.HasOnReconcile() && crd.OperatorBox.OnReconcile.CustomResource != nil {
			for i, cr := range crd.OperatorBox.OnReconcile.CustomResource {
				path := fmt.Sprintf("%s.onReconcile.custom[%d]", name, i)
				if err := ValidateCustomResource(&crd, &cr, path); err != nil {
					return err
				}

				if cr.Metadata.Namespaced != nil && !*cr.Metadata.Namespaced && cr.Metadata.Namespace != "" {
					return fmt.Errorf("%s %s: metadata.namespaced=false but metadata.namespace is set (cluster-scoped resources must not set namespace)", failureMark(), path)
				}

				logger.Debug().
					Str("crd", name).
					Str("path", path).
					Msg("validated onReconcile custom resource declaration")
			}
		}
	}

	return nil
}

// Notes:
// ValidateCustomResource validates a single CustomResource declaration. It
// enforces the structural and semantic rules required by Orkestra while
// intentionally avoiding CRD schema validation (the API server owns that).
//
// Validation responsibilities:
//   - Ensure required top-level fields (apiVersion, kind) are present and well-formed.
//   - Ensure metadata.name is present after templating.
//   - Enforce namespaced vs cluster-scoped semantics using Metadata.Namespaced
//     with a defensive default of namespaced=true.
//   - Validate labels and annotations.
//   - Validate template syntax for spec/status/other fields. (TODO)
//   - Validate hasStatus semantics and warn when user-provided status will be ignored.
//   - Return path-aware errors so callers can point users to the exact declaration.
//

func ValidateCustomResource(crd *orktypes.CRDEntry, cr *orktypes.CustomResourceTemplateSource, path string) error {
	if cr == nil {
		return fmt.Errorf("%s %s: custom resource declaration is nil", failureMark(), path)
	}

	// --- apiVersion ---------------------------------------------------------
	if strings.TrimSpace(cr.APIVersion) == "" {
		return fmt.Errorf("%s %s: missing required field 'apiVersion'", failureMark(), path)
	}
	if !strings.Contains(cr.APIVersion, "/") {
		return fmt.Errorf("%s %s: apiVersion %q is invalid — must be group/version (e.g. foo.io/v1)", failureMark(), path, cr.APIVersion)
	}

	// --- native type guard --------------------------------------------------
	// Reject built-in Kubernetes types that Orkestra manages natively via
	// HookTemplates. Using custom: for these panics during simulate (scheme
	// double-registration) and bypasses native features like drift correction
	// and profiles. The builtInRegistry is the single source of truth.
	parts := strings.SplitN(cr.APIVersion, "/", 2)
	group, version := parts[0], parts[1]
	if b, isNative := children.LookupBuiltInByGVK(group, version, cr.Kind); isNative && b.IsChild && b.HookKey != "" {
		return fmt.Errorf(
			"%s %s: %s %s is a native Orkestra resource — use %s: instead of custom:",
			failureMark(), path, cr.APIVersion, cr.Kind, b.HookKey,
		)
	}

	// --- kind ---------------------------------------------------------------
	if strings.TrimSpace(cr.Kind) == "" {
		return fmt.Errorf("%s %s: missing required field 'kind'", failureMark(), path)
	}

	// --- metadata -----------------------------------------------------------
	// Defensive: metadata must be present and name required after templating.
	name := cr.Metadata.Name
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s %s: metadata.name is required", failureMark(), path)
	}
	if !isTemplate(name) {
		if err := isValidK8sName(name); err != nil {
			return fmt.Errorf("%s metadata.name: %w", failureMark(), err)
		}
	}

	// --- metadata.labels/annotations -----------------------------------------------------------
	// Validate that the key is a valid Kubernetes label/annotation key
	labels := cr.Metadata.Labels
	annotations := cr.Metadata.Annotations
	for k, v := range labels {
		if errs := isValidLabelKey(k); len(errs) > 0 {
			return fmt.Errorf("%s %s: metadata.labels is not a valid label key", failureMark(), k)
		}
		if isTemplate(v) {
			continue
		}
		if errs := isValidLabelValue(v); len(errs) > 0 {
			return fmt.Errorf("%s %s: metadata.labels is not a valid label value", failureMark(), k)
		}
	}
	for k := range annotations {
		if errs := isValidLabelKey(k); len(errs) > 0 {
			return fmt.Errorf("%s %s: metadata.annotations is not a valid annotation key", failureMark(), k)
		}
	}
	// Namespaced defaulting: nil => true (namespaced by default)
	namespaced := true
	if cr.Metadata.Namespaced != nil {
		namespaced = *cr.Metadata.Namespaced
	}

	// If the declaration explicitly marks cluster-scoped but provided a namespace, error.
	if !namespaced && cr.Metadata.Namespace != "" {
		return fmt.Errorf("%s %s: metadata.namespaced=false but metadata.namespace is set (cluster-scoped resources must not set namespace)", failureMark(), path)
	}

	// If namespaced (default) then namespace must be present (non-Empty().
	// Note: We allow empty namespace when the reconciler will template it to a value,
	// but at validation time we require the field to be present (non-Empty() to avoid
	// accidental cluster-scoped creations. If you prefer to allow templated empty
	// namespaces, relax this check accordingly.
	if namespaced && strings.TrimSpace(cr.Metadata.Namespace) == "" {
		return fmt.Errorf("%s %s: metadata.namespace is required for namespaced custom resources (metadata.namespaced is true or unspecified)", failureMark(), path)
	}

	// --- hasStatus ----------------------------------------------------------
	// Only type/semantic check: YAML/JSON parsing guarantees boolean type.
	// Warn if user provided status but explicitly disabled status writes.
	if cr.HasStatus != nil && !*cr.HasStatus && len(cr.Status) > 0 {
		warning := fmt.Sprintf("%s: status provided but hasStatus=false — status will be ignored", path)
		crd.Warnings.AddWarning(warning)
	}

	return nil
}
