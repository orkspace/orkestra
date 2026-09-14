package reconciler

import (
	"context"

	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/runners"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// cleanupPreviousSurface deletes all resources belonging to the surface the CR
// was on before the current reconcile cycle, then stamps AnnotationLastSurface
// with the new target so subsequent reconciles skip this step.
//
// Detection: compares AnnotationLastSurface (previous target) with the current
// effective target derived from serve-alias / serve-target annotations. A mismatch
// means the CR was re-routed via the gateway and the old surface must be cleaned up.
//
// Deletion: label-selector sweep over all known resource types for the previous
// owner key ("<crName>.<prevTarget>"). Template-based deletion is not used because
// forEach spec fields may have been cleared before this runs (e.g. spec.regions
// removed when switching away from a regional target).
func (r *GenericReconciler[PTR]) cleanupPreviousSurface(
	ctx context.Context,
	rawObj PTR,
) error {
	target := orktarget.ResolveTargetFromAnnotations(rawObj.GetAnnotations())
	if target == "" || r.crd.KeepPreviousSurface(target) {
		return nil
	}

	prevTarget := rawObj.GetAnnotations()[labels.AnnotationLastSurface]
	if prevTarget != "" && prevTarget != target {
		prevOwnerKey := labels.EffectiveOwnerKey(rawObj.GetName(), map[string]string{
			labels.AnnotationServeAlias: prevTarget,
		})
		ns := rawObj.GetNamespace()
		if err := runners.SweepOwnedNamespacedResources(ctx, r.kube, prevOwnerKey, ns); err != nil {
			return err
		}
		if err := runners.SweepOwnedClusterScopedResources(ctx, r.kube, prevOwnerKey); err != nil {
			return err
		}
	}

	if err := r.kube.PatchAnnotations(ctx, rawObj, map[string]string{
		labels.AnnotationLastSurface: target,
	}, metav1.PatchOptions{}); err != nil {
		logger.FromContext(ctx).Warn().Err(err).
			Str("name", rawObj.GetName()).
			Msg("surface cleanup: failed to update last-surface annotation")
	}

	return nil
}
