package reconciler

import (
	"context"

	orkexternal "github.com/orkspace/orkestra/pkg/external"
	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
)

// applyReconcileTimeValidation evaluates validation rules against the live CR.
// Returns the (possibly enriched) resolver so external call results from
// validation.external are available to the caller for status and templates.
// Warn violations are logged as advisory — reconcile continues.
// Deny violations halt reconcile — the caller patches status and returns the error.
func (r *GenericReconciler[PTR]) applyReconcileTimeValidation(ctx context.Context, resolver *orktmpl.Resolver, obj PTR) (*orktmpl.Resolver, *ValidationResult, error) {
	if r.crd.Validation == nil || len(r.crd.Validation.Rules) == 0 {
		return resolver, nil, nil
	}

	var err error
	if calls := r.crd.Validation.ReconcileExternal(); len(calls) > 0 {
		resolver, err = orkexternal.Run(ctx, r.crd.GVKString(), resolver, calls, r.kube.Clientset())
		if err != nil {
			return resolver, nil, err
		}
	}

	result := runValidation(resolver.Data(), resolver, r.crd.Validation, r.crd.APITypes.Kind)

	for _, w := range result.Warnings {
		logger.FromContext(ctx).Warn().
			Str("name", obj.GetName()).
			Str("crd", r.crd.GVKString()).
			Str("field", w.Field).
			Str("message", w.Message).
			Msg("reconcile validation: warn")
	}

	if result.Deny {
		return resolver, result, result.DenialError()
	}

	return resolver, result, nil
}

// applyReconcileTimeMutation applies mutation defaults to the CR and patches
// the spec subresource when changes are needed.
// Returns the (possibly enriched) resolver so external call results from
// mutation.external are available to the caller for subsequent steps.
// Mutation failures are non-fatal — the caller logs and continues.
func (r *GenericReconciler[PTR]) applyReconcileTimeMutation(ctx context.Context, resolver *orktmpl.Resolver, obj PTR) (*orktmpl.Resolver, error) {
	if r.crd.Mutation == nil || len(r.crd.Mutation.Rules) == 0 {
		return resolver, nil
	}

	if calls := r.crd.Mutation.ReconcileExternal(); len(calls) > 0 {
		var err error
		resolver, err = orkexternal.Run(ctx, r.crd.GVKString(), resolver, calls, r.kube.Clientset())
		if err != nil {
			return resolver, err
		}
	}

	_, err := runMutation(ctx, r.kube, obj, resolver, r.crd.Mutation, r.crd.GVR(), r.crd.APITypes.Kind)
	return resolver, err
}
