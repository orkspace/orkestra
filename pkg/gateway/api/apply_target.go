package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ApplyTargetFields runs the full target-mode apply pipeline — target/alias
// resolution, CR construction, provenance stamping, token permission check,
// SSA patch, and response shaping — for callers that already have a flat
// field map and no *http.Request to hand to applyHandler. Used by gateway
// intake sources (GitHub, GitLab, Slack, generic webhook), whose delivery
// mechanism is a push event rather than a caller-initiated HTTP apply.
//
// tokenName identifies the caller for both the serve.tokens permission check
// and the stamped serve-source provenance annotation — for intake sources,
// this is the webhook entry's own Name, the same identity serve.tokens
// authorizes against.
//
// Intake sources never carry an ?override=true equivalent, so a routing
// surface conflict here is always rejected rather than offering an override
// path — that decision belongs to whoever calls POST /api/v1/apply directly.
func ApplyTargetFields(
	ctx context.Context,
	kube kubeclient.Interface,
	clusters *ClusterRegistry,
	kat *katalog.Katalog,
	notes orktypes.NoteRegistry,
	tokenName string,
	fields map[string]interface{},
	dryRun bool,
) (*ApplyResponse, int) {
	target, _ := fields["target"].(string)
	if strings.TrimSpace(target) == "" {
		return &ApplyResponse{Message: `"target" must be a non-empty string`}, http.StatusBadRequest
	}

	resolution := kat.LookupByTargetOrAlias(target)
	if resolution == nil {
		return &ApplyResponse{
			Message: fmt.Sprintf(
				"unknown target %q — available: %s",
				target, strings.Join(kat.AvailableTargets(), ", "),
			),
		}, http.StatusBadRequest
	}
	crd := resolution.CRD
	alias := resolution.Alias

	targets, clusterErr := resolveClusterTargets(crd, alias, fields, notes, clusters, kube)
	if clusterErr != nil {
		return &ApplyResponse{Message: clusterErr.Error()}, http.StatusBadRequest
	}

	obj, err := orktarget.BuildCRFromTarget(fields, crd, notes)
	if err != nil {
		return &ApplyResponse{Message: err.Error()}, http.StatusBadRequest
	}
	InjectProvenanceAnnotations(obj, crd.ServeTarget(), alias, tokenName)
	InjectServeIntentAnnotation(obj, fields)
	gvr := crd.GVR()

	patchBody, err := json.Marshal(obj.Object)
	if err != nil {
		return &ApplyResponse{Message: "failed to marshal built CR"}, http.StatusInternalServerError
	}

	if obj.GetName() == "" {
		return &ApplyResponse{
			DryRun:  dryRun,
			Message: "name is required",
			Violations: []ApplyViolation{{
				Field:    "metadata.name",
				Message:  "name is required",
				Severity: "error",
			}},
		}, http.StatusUnprocessableEntity
	}
	if obj.GetNamespace() == "" {
		return &ApplyResponse{
			DryRun:  dryRun,
			Message: "namespace is required",
			Violations: []ApplyViolation{{
				Field:    "metadata.namespace",
				Message:  "namespace is required",
				Severity: "error",
			}},
		}, http.StatusUnprocessableEntity
	}

	overwrite := crd.ServeForceConflictEnabledFor(alias)
	patchOpts := metav1.PatchOptions{
		FieldManager: konfig.FieldManagerGateway,
		Force:        boolPtr(overwrite),
	}
	if dryRun {
		patchOpts.DryRun = []string{metav1.DryRunAll}
	}

	payload := EvaluatePayload(obj.Object, crd, alias, notes)
	resolver := orktmpl.NewResolverFromMap(fields).WithUserNotes(notes)

	incomingSurface := alias
	if incomingSurface == "" {
		incomingSurface = crd.ServeTarget()
	}

	// ── Single local cluster — original response shape ──────────────────
	if len(targets) == 1 && targets[0].name == "" {
		effectiveKube := targets[0].kube

		op := orktypes.ServeOpCreate
		existing, probeErr := effectiveKube.DynamicClient().
			Resource(gvr).Namespace(obj.GetNamespace()).
			Get(ctx, obj.GetName(), metav1.GetOptions{})
		if probeErr == nil {
			op = orktypes.ServeOpUpdate
			ann := existing.GetAnnotations()
			storedSurface := ann[labels.AnnotationServeAlias]
			if storedSurface == "" {
				storedSurface = ann[labels.AnnotationServeTarget]
			}
			if storedSurface != "" && storedSurface != incomingSurface {
				msg := fmt.Sprintf(
					"routing surface conflict: resource was created via %q, cannot update via %q",
					storedSurface, incomingSurface,
				)
				return &ApplyResponse{
					Message: msg,
					Violations: []ApplyViolation{{
						Field:    "metadata.annotations",
						Message:  msg,
						Severity: "error",
					}},
				}, http.StatusConflict
			}
		}

		allowed, reason := crd.TokenAllowedFor(alias, tokenName, op, obj.GetNamespace(), orktypes.ServeClassResources)
		if !allowed {
			msg := reason.Message(tokenName, op, obj.GetKind(), obj.GetNamespace())
			return &ApplyResponse{
				Message:    msg,
				Violations: []ApplyViolation{{Field: "metadata", Message: msg, Severity: "error"}},
			}, http.StatusForbidden
		}

		capture := &warningCapture{}
		result, err := scopedDynamic(effectiveKube, capture).
			Resource(gvr).Namespace(obj.GetNamespace()).
			Patch(ctx, obj.GetName(), k8stypes.ApplyPatchType, patchBody, patchOpts)
		if err != nil {
			logger.FromContext(ctx).Warn().
				Str("kind", obj.GetKind()).Str("name", obj.GetName()).
				Str("tokenName", tokenName).Err(err).
				Msg("gateway API: SSA rejected (intake)")
			violations := extractViolations(err)
			message := err.Error()
			if len(violations) > 0 && violations[0].Message != "" {
				message = violations[0].Message
			}
			return &ApplyResponse{
				DryRun: dryRun, Message: message,
				Warnings: capture.collect(), Violations: violations,
			}, http.StatusUnprocessableEntity
		}

		pollURL := resolvePollURL(result.GetKind(), result.GetNamespace(), result.GetName(), crd.GetServePollingConfig(), resolver)
		return &ApplyResponse{
			Accepted: true, DryRun: dryRun,
			Name: result.GetName(), Namespace: result.GetNamespace(),
			Kind: result.GetKind(), APIVersion: result.GetAPIVersion(),
			PollURL: pollURL, Warnings: capture.collect(), Payload: payload,
		}, http.StatusOK
	}

	// ── Fan-out — apply to each resolved cluster target ─────────────────
	var clusterResults []ClusterApplyResult
	for _, t := range targets {
		cr := ClusterApplyResult{Cluster: t.name}

		op := orktypes.ServeOpCreate
		existing, probeErr := t.kube.DynamicClient().
			Resource(gvr).Namespace(obj.GetNamespace()).
			Get(ctx, obj.GetName(), metav1.GetOptions{})
		if probeErr == nil {
			op = orktypes.ServeOpUpdate
			ann := existing.GetAnnotations()
			storedSurface := ann[labels.AnnotationServeAlias]
			if storedSurface == "" {
				storedSurface = ann[labels.AnnotationServeTarget]
			}
			if storedSurface != "" && storedSurface != incomingSurface {
				cr.Message = fmt.Sprintf(
					"routing surface conflict: resource was created via %q, cannot update via %q",
					storedSurface, incomingSurface,
				)
				clusterResults = append(clusterResults, cr)
				continue
			}
		}

		allowed, reason := crd.TokenAllowedFor(alias, tokenName, op, obj.GetNamespace(), orktypes.ServeClassResources)
		if !allowed {
			cr.Message = reason.Message(tokenName, op, obj.GetKind(), obj.GetNamespace())
			clusterResults = append(clusterResults, cr)
			continue
		}

		capture := &warningCapture{}
		result, err := scopedDynamic(t.kube, capture).
			Resource(gvr).Namespace(obj.GetNamespace()).
			Patch(ctx, obj.GetName(), k8stypes.ApplyPatchType, patchBody, patchOpts)
		if err != nil {
			logger.FromContext(ctx).Warn().
				Str("cluster", t.name).Str("kind", obj.GetKind()).
				Str("name", obj.GetName()).Err(err).
				Msg("gateway API: SSA rejected (intake fan-out)")
			violations := extractViolations(err)
			message := err.Error()
			if len(violations) > 0 && violations[0].Message != "" {
				message = violations[0].Message
			}
			cr.Message = message
			cr.Warnings = capture.collect()
			cr.Violations = violations
			clusterResults = append(clusterResults, cr)
			continue
		}

		cr.Accepted = true
		cr.Name = result.GetName()
		cr.Namespace = result.GetNamespace()
		cr.Warnings = capture.collect()
		cr.PollURL = resolvePollURL(result.GetKind(), result.GetNamespace(), result.GetName(), crd.GetServePollingConfig(), resolver)
		clusterResults = append(clusterResults, cr)
	}

	var anyAccepted, allAccepted bool
	allAccepted = true
	for _, cr := range clusterResults {
		if cr.Accepted {
			anyAccepted = true
		} else {
			allAccepted = false
		}
	}

	return &ApplyResponse{
		Accepted:       anyAccepted,
		DryRun:         dryRun,
		Kind:           obj.GetKind(),
		APIVersion:     obj.GetAPIVersion(),
		PartialSuccess: anyAccepted && !allAccepted,
		Clusters:       clusterResults,
		Payload:        payload,
	}, http.StatusOK
}
