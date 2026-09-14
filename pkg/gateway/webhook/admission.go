// webhook/admission.go — /validate and /mutate HTTP handlers.
//
// The Kubernetes API server POSTs an AdmissionReview to /validate for every
// CREATE and UPDATE on a resource covered by the ValidatingWebhookConfiguration,
// and to /mutate for every CREATE and UPDATE covered by the MutatingWebhookConfiguration.
//
// Mutation fires before validation in the Kubernetes admission chain, so defaults
// are applied before validation sees the object.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/metrics"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (ws *WebhookServer) validationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()

	var review AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		logger.Error().Err(err).Msg("admission/validate: failed to decode AdmissionReview")
		http.Error(w, "invalid AdmissionReview", http.StatusBadRequest)
		return
	}

	if review.Request == nil {
		http.Error(w, "missing request in AdmissionReview", http.StatusBadRequest)
		return
	}

	req := review.Request

	logger.Debug().
		Str("uid", req.UID).
		Str("kind", req.Kind.Kind).
		Str("name", req.Name).
		Str("namespace", req.Namespace).
		Str("operation", req.Operation).
		Msg("admission/validate: received request")

	resp := &AdmissionResponse{UID: req.UID, Allowed: true}

	gvrKey := gvrToKey(req.Resource)
	rules := ws.admissionRegistry.GetValidationRules(gvrKey)

	if rules == nil || len(rules.Rules) == 0 {
		ws.writeAdmissionResponse(w, review.APIVersion, review.Kind, resp)
		return
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(req.Object, &obj); err != nil {
		logger.Error().Err(err).
			Str("uid", req.UID).
			Str("kind", req.Kind.Kind).
			Msg("admission/validate: failed to unmarshal object")
		resp.Allowed = false
		resp.Status = &AdmissionStatus{
			Message: fmt.Sprintf("internal error: could not parse object: %v", err),
			Code:    500,
		}
		ws.writeAdmissionResponse(w, review.APIVersion, review.Kind, resp)
		return
	}

	denials, warnings := ws.evaluateValidationRules(r.Context(), obj, rules, req.Kind.Kind)

	for _, w := range warnings {
		resp.Warnings = append(resp.Warnings,
			fmt.Sprintf("orkestra: field %q: %s", w.Field, w.Message))
	}

	duration := time.Since(start)
	crdName := req.Kind.Kind

	admStats := ws.admissionStatsFor(gvrKey)
	if len(denials) > 0 {
		admStats.RecordValidationDenied(duration)
		metrics.RecordValidationOutcome(crdName, "denied", metrics.MetricSourceAdmission)
		for _, d := range denials {
			metrics.RecordValidationViolation(crdName, d.Field, d.RuleType, "deny", metrics.MetricSourceAdmission)
		}
		for _, w := range warnings {
			metrics.RecordValidationViolation(crdName, w.Field, w.RuleType, "warn", metrics.MetricSourceAdmission)
		}
	} else if len(warnings) > 0 {
		admStats.RecordValidationWarned(duration)
		metrics.RecordValidationOutcome(crdName, "warned", metrics.MetricSourceAdmission)
		for _, w := range warnings {
			metrics.RecordValidationViolation(crdName, w.Field, w.RuleType, "warn", metrics.MetricSourceAdmission)
		}
	} else {
		admStats.RecordValidationAllowed(duration)
		metrics.RecordValidationOutcome(crdName, "allowed", metrics.MetricSourceAdmission)
	}
	metrics.RecordValidationDurationSeconds(crdName, duration.Seconds())

	if len(denials) > 0 {
		resp.Allowed = false
		msgs := make([]string, 0, len(denials))
		causes := make([]metav1.StatusCause, 0, len(denials))
		for _, d := range denials {
			msgs = append(msgs, fmt.Sprintf("field %q: %s (got: %q)", d.Field, d.Message, d.Got))
			causes = append(causes, metav1.StatusCause{
				Type:    metav1.CauseTypeFieldValueInvalid,
				Field:   d.Field,
				Message: d.Message,
			})
		}
		resp.Status = &AdmissionStatus{
			Message: fmt.Sprintf(
				"\n\n[Orkestra Validation] validation failed\n\n"+
					"%s/%s/%s was blocked due to the following policies:\n"+
					" %s\n\n", req.Kind.Kind, req.Name, req.Namespace, strings.Join(msgs, "; ")),
			Code:    400,
			Details: &metav1.StatusDetails{Causes: causes},
		}
		logger.Info().
			Str("kind", req.Kind.Kind).
			Str("name", req.Name).
			Str("namespace", req.Namespace).
			Str("uid", req.UID).
			Int("denials", len(denials)).
			Msg("admission/validate: rejected")
	} else if len(warnings) > 0 {
		logger.Info().
			Str("kind", req.Kind.Kind).
			Str("name", req.Name).
			Int("warnings", len(warnings)).
			Msg("admission/validate: allowed with warnings")
	} else {
		logger.Debug().
			Str("kind", req.Kind.Kind).
			Str("name", req.Name).
			Msg("admission/validate: allowed")
	}

	ws.writeAdmissionResponse(w, review.APIVersion, review.Kind, resp)
}

func (ws *WebhookServer) mutationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()

	var review AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		logger.Error().Err(err).Msg("admission/mutate: failed to decode AdmissionReview")
		http.Error(w, "invalid AdmissionReview", http.StatusBadRequest)
		return
	}

	if review.Request == nil {
		http.Error(w, "missing request in AdmissionReview", http.StatusBadRequest)
		return
	}

	req := review.Request

	logger.Debug().
		Str("uid", req.UID).
		Str("kind", req.Kind.Kind).
		Str("name", req.Name).
		Str("namespace", req.Namespace).
		Str("operation", req.Operation).
		Msg("admission/mutate: received request")

	resp := &AdmissionResponse{UID: req.UID, Allowed: true}

	gvrKey := gvrToKey(req.Resource)
	rules := ws.admissionRegistry.GetMutationRules(gvrKey)

	if rules == nil || len(rules.Rules) == 0 {
		ws.writeAdmissionResponse(w, review.APIVersion, review.Kind, resp)
		return
	}

	var original map[string]interface{}
	if err := json.Unmarshal(req.Object, &original); err != nil {
		logger.Error().Err(err).
			Str("uid", req.UID).
			Str("kind", req.Kind.Kind).
			Msg("admission/mutate: failed to unmarshal object")
		ws.writeAdmissionResponse(w, review.APIVersion, review.Kind, resp)
		return
	}

	mutated := deepCopyMap(original)
	changes, err := ws.applyMutationRules(context.Background(), mutated, rules, req.Kind.Kind)
	if err != nil {
		logger.Error().Err(err).
			Str("kind", req.Kind.Kind).
			Str("name", req.Name).
			Msg("admission/mutate: error applying rules — allowing without mutation")
		ws.writeAdmissionResponse(w, review.APIVersion, review.Kind, resp)
		return
	}

	crdName := req.Kind.Kind

	mutStats := ws.admissionStatsFor(gvrKey)
	if len(changes) == 0 {
		duration := time.Since(start)
		mutStats.RecordMutationSkipped(duration)
		metrics.RecordMutationOutcome(crdName, "skipped", metrics.MetricSourceAdmission)
		metrics.RecordMutationDurationSeconds(crdName, duration.Seconds())
		logger.Debug().
			Str("kind", crdName).
			Str("name", req.Name).
			Msg("admission/mutate: no changes — allowed without patch")
		ws.writeAdmissionResponse(w, review.APIVersion, review.Kind, resp)
		return
	}

	patch, err := buildJSONPatch(changes)
	if err != nil {
		logger.Error().Err(err).
			Str("kind", crdName).
			Str("name", req.Name).
			Msg("admission/mutate: failed to build patch — allowing without mutation")
		ws.writeAdmissionResponse(w, review.APIVersion, review.Kind, resp)
		return
	}

	resp.Patch = patch
	resp.PatchType = stringPtr(jsonPatchType)

	duration := time.Since(start)
	mutStats.RecordMutationApplied(duration)
	metrics.RecordMutationOutcome(crdName, "applied", metrics.MetricSourceAdmission)
	for _, c := range changes {
		metrics.RecordMutationFieldApplied(crdName, c.Field, c.ChangeType, metrics.MetricSourceAdmission)
	}
	metrics.RecordMutationDurationSeconds(crdName, duration.Seconds())

	logger.Info().
		Str("kind", crdName).
		Str("name", req.Name).
		Int("changes", len(changes)).
		Msg("admission/mutate: defaults applied")

	ws.writeAdmissionResponse(w, review.APIVersion, review.Kind, resp)
}

// ── Shared helpers ─────────────────────────────────────────────────────────────

func (ws *WebhookServer) writeAdmissionResponse(
	w http.ResponseWriter,
	apiVersion, kind string,
	resp *AdmissionResponse,
) {
	review := AdmissionReview{
		APIVersion: apiVersion,
		Kind:       kind,
		Response:   resp,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(review); err != nil {
		logger.Error().Err(err).Str("uid", resp.UID).Msg("admission: failed to encode response")
	}
}

func gvrToKey(gvr metav1.GroupVersionResource) string {
	if gvr.Group == "" {
		return fmt.Sprintf("%s/%s", gvr.Version, gvr.Resource)
	}
	return fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Version, gvr.Resource)
}

func deepCopyMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	b, _ := json.Marshal(src)
	var dst map[string]interface{}
	_ = json.Unmarshal(b, &dst)
	return dst
}

// JSONPatchOp is one operation in a JSON Patch document (RFC 6902).
type JSONPatchOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func buildJSONPatch(changes []fieldChange) ([]byte, error) {
	ops := make([]JSONPatchOp, 0, len(changes))
	for _, change := range changes {
		ptr := "/" + strings.ReplaceAll(change.Field, ".", "/")
		op := "replace"
		if change.OldValue == "" {
			op = "add"
		}
		ops = append(ops, JSONPatchOp{Op: op, Path: ptr, Value: change.TypedValue})
	}
	return json.Marshal(ops)
}
