package reconciler

import (
	"context"

	"slices"

	"github.com/orkspace/orkestra/domain"
	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// effectiveBoxAndTarget returns the operatorBox and resolved target name for
// this reconcile cycle. The target is read from the CR's serve-target annotation
// (alias resolved > raw target > Empty(). Falls back to the CRD-level box when
// the CR has no annotation (e.g. direct kubectl apply).
// The system CleanupFinalizer is always included in the returned box.
func (r *GenericReconciler[PTR]) effectiveBoxAndTarget(obj PTR) (orktypes.OperatorBoxConfig, string) {
	target := orktarget.ResolveTargetFromAnnotations(obj.GetAnnotations())
	box := *r.crd.EffectiveOperatorBox(target)
	if !slices.Contains(box.Finalizers, labels.CleanupFinalizer) {
		box.Finalizers = append(box.Finalizers, labels.CleanupFinalizer)
	}
	return box, target
}

// hooksFor returns the ObjectHooks for the given target name.
// If the target has a distinct hook binary (registered in TargetHookFactories),
// those hooks are returned. Otherwise falls back to the CRD-level hooks.
func (r *GenericReconciler[PTR]) hooksFor(target string) domain.ObjectHooks {
	if target != "" {
		if h, ok := r.targetHooks[target]; ok {
			return h
		}
	}
	return r.hooks
}

// withTargetArgs returns a context whose kube client carries the per-target
// merged hooks.args for this reconcile cycle. When the effective box has no
// args override, the context is returned unchanged.
func (r *GenericReconciler[PTR]) withTargetArgs(ctx context.Context, box orktypes.OperatorBoxConfig) context.Context {
	args := box.Reconciler.HooksArgs()
	if len(args) == 0 {
		return ctx
	}
	return kubeclient.WithKubeclient(ctx, r.kube.WithArgs(kubeclient.Args(args)))
}
