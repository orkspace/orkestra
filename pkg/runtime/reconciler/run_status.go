// pkg/reconciler/run_status.go
//
// Status management — three layers:
//
// Layer 1 — Automatic Ready condition.
//
//	Written after every reconcile regardless of Katalog declarations.
//	reason: ReconcileSucceeded on success, ReconcileError on failure.
//	No Katalog declaration required. Requires subresources.status in the CRD.
//
// Layer 2 — Declarative status fields.
//
//	Written from status.fields declarations in the Katalog.
//	Template expressions resolved against the live CR object map.
//	Optional when: conditions gate individual field writes — this is what
//	makes declarative state machines possible.
//	Written only on successful reconcile.
//
// Layer 3 — Child resource propagation.
//
//	Children are read after runTemplateReconcile and injected into the
//	resolver context via WithChildren. The returned resolver is used for
//	all subsequent status resolution.
//	Status field expressions reference children as:
//	  {{ .children.deployment.status.readyReplicas }}
//	  {{ .children.cronjob.status.lastScheduleTime }}
//	Children are keyed by lowercase Kind — matches kubectl conventions.
package reconciler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/children"
	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// patchStatusWithChildren is the top-level status entry point called from
// reconcileImpl after the reconcile work completes.
//
// Always called — even when reconcile returned an error — so that
// Ready=False is written on failure. The error argument controls Layer 1.
func (r *GenericReconciler[PTR]) patchStatusWithChildren(
	ctx context.Context,
	obj PTR,
	resolver *orktmpl.Resolver,
	reconcileErr error,
	box orktypes.OperatorBoxConfig,
	valResult ...*ValidationResult,
) {
	// ── Layer 3: extend resolver with child resource state ─────────────────
	// WithChildren returns a new Resolver — the original is not modified.
	// The new resolver's data map includes a "children" key so that
	// status field expressions can reference child status:
	//   {{ .children.cronjob.status.lastScheduleTime }}
	if reconcileErr == nil && (box.OnCreate != nil || box.OnReconcile != nil) {
		children := children.ReadChildren(ctx, r.kube, obj, resolver, r.crd)
		resolver = resolver.WithChildren(children) // ← reassign — WithChildren returns new resolver
	}

	// ── Layer 1 + 2: patch status ──────────────────────────────────────────
	var vr *ValidationResult
	if len(valResult) > 0 {
		vr = valResult[0]
	}
	if err := runStatusPatch(ctx, r, obj, resolver, reconcileErr, vr, box); err != nil {
		logger.FromContext(ctx).Warn().Err(err).
			Str("name", obj.GetName()).
			Msg("status: patch failed — continuing")
	}
}

// runStatusPatch writes Layer 1 (Ready condition) and Layer 2 (declared fields).
//
// Works for both unstructured and typed CRDs. Previously, typed objects hit an
// early return here because PatchStatus was assumed to require *unstructured.Unstructured.
// PatchStatus only needs domain.Object (for GetName/GetNamespace), so obj is passed
// directly — objectToMap in the template package already handles the JSON round-trip
// for typed spec access, so there is no longer any structural gap between the two modes.
func runStatusPatch[PTR domain.Object](
	ctx context.Context,
	r *GenericReconciler[PTR],
	obj PTR,
	resolver *orktmpl.Resolver,
	reconcileErr error,
	valResult *ValidationResult,
	box orktypes.OperatorBoxConfig,
) error {
	// ── Layer 1: Ready condition ───────────────────────────────────────────
	// Always written — on success and failure — so operators can monitor
	// the Ready condition without knowing anything else about the CRD.
	// Except for some builtins or explicitly required
	if r.crd.SkipStatusSubresource() {
		return nil
	}

	patch := map[string]interface{}{}

	skipObservedGen := r.crd.SkipObservedGeneration() // true for Namespace etc
	cond := buildReadyCondition(reconcileErr, obj.GetGeneration(), skipObservedGen)
	conditions := []interface{}{cond}
	conditions = append(conditions, buildValidationCondition(valResult))
	conditions = append(conditions, buildValidationWarningCondition(valResult))
	patch["conditions"] = conditions

	// Only patch if necessary
	if !skipObservedGen {
		patch["observedGeneration"] = obj.GetGeneration()
	}

	// ── Layer 2: Declared status fields ───────────────────────────────────
	// Conditional fields (with when:/or:) are always evaluated so that
	// status can reflect why reconcile failed (e.g. external health check result).
	// Unconditional fields are only written on success — on error they would
	// write stale or misleading values (e.g. phase: Active while denied).
	// Errors in field resolution are logged as warnings and do not fail the reconcile.
	logger.FromContext(ctx).Debug().
		Str("name", obj.GetName()).
		Bool("has_status_config", box.Status != nil && box.Status.HasFields()).
		Bool("reconcile_error", reconcileErr != nil).
		AnErr("reconcile_err", reconcileErr).
		Msg("status: layer2 evaluation")

	if box.Status != nil && box.Status.HasFields() {
		fields := box.Status.Fields
		if reconcileErr != nil {
			var conditional []orktypes.StatusFieldSpec
			for _, f := range fields {
				if len(f.When) > 0 || len(f.Or) > 0 {
					conditional = append(conditional, f)
				}
			}
			fields = conditional
		}
		resolved, err := resolver.ResolveStatusFields(fields)
		if err != nil {
			logger.FromContext(ctx).Warn().Err(err).
				Str("name", obj.GetName()).
				Msg("status: some fields failed to resolve")
		}
		logger.FromContext(ctx).Debug().
			Str("name", obj.GetName()).
			Interface("resolved_fields", resolved).
			Msg("status: layer2 resolved fields")
		for k, v := range resolved {
			patch[k] = v
		}
	}

	// Skip the API call entirely when nothing has semantically changed.
	// PatchStatus always increments resourceVersion, which generates a watch
	// event on the CR — immediately re-queuing a reconcile and defeating the
	// configured resync interval.
	if statusPatchNeeded(obj, patch) {
		return r.kube.PatchStatus(ctx, obj, patch, metav1.PatchOptions{})
	}
	return nil
}

// statusPatchNeeded returns true when the patch carries at least one
// meaningful change vs. the object's current status. lastTransitionTime is
// excluded from the comparison — only type/status/reason/message are checked
// for conditions, and the scalar fields (observedGeneration, any layer-2
// values) are compared directly.
func statusPatchNeeded(obj domain.Object, patch map[string]interface{}) bool {
	u, ok := any(obj).(*unstructured.Unstructured)
	if !ok {
		// Can't read existing status — patch to be safe.
		return true
	}

	// observedGeneration
	if desiredGen, ok := patch["observedGeneration"].(int64); ok {
		existingGen, _, _ := unstructured.NestedInt64(u.Object, "status", "observedGeneration")
		if existingGen != desiredGen {
			return true
		}
	}

	// conditions — compare by type, ignoring lastTransitionTime
	existing, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	byType := make(map[string]map[string]interface{}, len(existing))
	for _, c := range existing {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := cm["type"].(string); t != "" {
			byType[t] = cm
		}
	}

	desired, _ := patch["conditions"].([]interface{})
	if len(desired) != len(byType) {
		return true
	}
	for _, d := range desired {
		dm, ok := d.(map[string]interface{})
		if !ok {
			return true
		}
		t, _ := dm["type"].(string)
		ex, found := byType[t]
		if !found {
			return true
		}
		if ex["status"] != dm["status"] || ex["reason"] != dm["reason"] || ex["message"] != dm["message"] {
			return true
		}
	}

	// layer-2 scalar fields (any key beyond conditions/observedGeneration)
	for k, v := range patch {
		if k == "conditions" || k == "observedGeneration" {
			continue
		}
		existing, ok, _ := unstructured.NestedFieldNoCopy(u.Object, "status", k)
		if !ok || existing != v {
			return true
		}
	}

	return false
}

// buildReadyCondition constructs the standard Kubernetes Ready condition map.
//
// Signature: (reconcileErr error, generation int64)
// — err nil   → Ready=True,  reason=ReconcileSucceeded
// — err non-nil → Ready=False, reason=ReconcileError, message=err.Error() truncated
//
// The condition is returned as map[string]interface{} for direct inclusion
// in the status patch — avoids an extra metav1.Condition → unstructured conversion.
func buildValidationCondition(valResult *ValidationResult) map[string]interface{} {
	now := time.Now().UTC().Format(time.RFC3339)
	if valResult != nil && valResult.Deny {
		msg := valResult.Error().Error()
		if len(msg) > 256 {
			msg = msg[:253] + "..."
		}
		return map[string]interface{}{
			"type":               "ValidationFailed",
			"status":             "True",
			"reason":             "DenyRuleViolation",
			"message":            msg,
			"lastTransitionTime": now,
		}
	}
	return map[string]interface{}{
		"type":               "ValidationFailed",
		"status":             "False",
		"reason":             "ValidationPassed",
		"message":            "",
		"lastTransitionTime": now,
	}
}

func buildValidationWarningCondition(valResult *ValidationResult) map[string]interface{} {
	now := time.Now().UTC().Format(time.RFC3339)
	if valResult != nil && len(valResult.Warnings) > 0 {
		msgs := make([]string, 0, len(valResult.Warnings))
		for _, w := range valResult.Warnings {
			msgs = append(msgs, fmt.Sprintf("field %q: %s", w.Field, w.Message))
		}
		msg := strings.Join(msgs, "; ")
		if len(msg) > 256 {
			msg = msg[:253] + "..."
		}
		return map[string]interface{}{
			"type":               "ValidationWarning",
			"status":             "True",
			"reason":             "WarnRuleViolation",
			"message":            msg,
			"lastTransitionTime": now,
		}
	}
	return map[string]interface{}{
		"type":               "ValidationWarning",
		"status":             "False",
		"reason":             "NoWarnings",
		"message":            "",
		"lastTransitionTime": now,
	}
}

func buildReadyCondition(reconcileErr error, generation int64, skipObservedGeneration bool) map[string]interface{} {
	now := time.Now().UTC().Format(time.RFC3339)

	cond := map[string]interface{}{
		"type":               "Ready",
		"status":             "True",
		"reason":             "ReconcileSucceeded",
		"message":            "",
		"lastTransitionTime": now,
	}

	if reconcileErr != nil {
		cond["status"] = "False"
		cond["reason"] = "ReconcileError"
		msg := reconcileErr.Error()
		if len(msg) > 256 {
			msg = msg[:253] + "..."
		}
		cond["message"] = msg
	}

	// Only add observedGeneration if the resource supports it
	if !skipObservedGeneration {
		cond["observedGeneration"] = generation
	}

	return cond
}
