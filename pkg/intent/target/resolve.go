package target

import (
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/utils/common"
)

// ResolveTargetFromAnnotations extracts the effective target from a CR's annotations.
// Resolution order:
//  1. serve-alias annotation (most specific)
//  2. serve-target annotation (primary target)
//  3. Empty string (no target found)
func ResolveTargetFromAnnotations(annotations map[string]string) string {
	if annotations == nil {
		return ""
	}

	// 1. Check alias first (most specific)
	if alias, ok := annotations[labels.AnnotationServeAlias]; ok && alias != "" {
		return alias
	}

	// 2. Fall back to target
	if target, ok := annotations[labels.AnnotationServeTarget]; ok && target != "" {
		return target
	}

	return ""
}

// ResolveIntentFromObject extracts the raw intent payload from the
// orkestra.orkspace.io/serve-intent annotation on a CR object map.
// Returns nil when the annotation is absent or unparseable.
// Used by both the webhook and the reconciler to inject .request into
// the resolver so validation rules can reference intent-vocabulary fields.
func ResolveIntentFromObject(obj map[string]interface{}) map[string]interface{} {
	return common.ResolveAnnotationFromObject(obj, labels.AnnotationServeIntent)
}
