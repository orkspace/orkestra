// pkg/gateway/apply/handler.go
//
// POST /api/v1/apply
//
// Pipeline: auth (handled by middleware) → decode body → SSA → translate response.
//
// The Gateway API does not duplicate admission logic. Webhooks enforce admission
// rules when enabled; the reconciler enforces them otherwise. This handler's
// only job is to accept a CR body, apply it via server-side apply, and return
// a structured result.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils/common"
)

// ApplyResponse is returned for every POST /api/v1/apply request.
type ApplyResponse struct {
	// Accepted is true when the CR was applied (or would be applied, for dry runs) without error.
	Accepted bool `json:"accepted"`
	// DryRun is true when the request was a dry-run preview (?dryRun=true).
	DryRun bool `json:"dryRun,omitempty"`
	// Name is the CR name as stored in Kubernetes.
	Name string `json:"name,omitempty"`
	// Namespace is the CR namespace.
	Namespace string `json:"namespace,omitempty"`
	// Kind is the CR kind.
	Kind string `json:"kind,omitempty"`
	// APIVersion is the CR apiVersion.
	APIVersion string `json:"apiVersion,omitempty"`
	// PollURL is the URL where the caller can GET the full state of this resource.
	// By default, it points to /api/v1/resources/{kind}/{namespace}/{name}.
	//
	// The platform team can override this via serve.config.response.poll:
	//   - poll.field:   appends ?field=<value> for lightweight polling
	//   - poll.url:     replaces the URL entirely with a custom template
	//
	// Callers should use this URL for subsequent GET requests instead of
	// assembling it themselves from Kind/Namespace/Name.
	PollURL string `json:"pollUrl,omitempty"`
	// Message carries the rejection reason on Accepted=false.
	Message string `json:"message,omitempty"`
	// Warnings is a list of advisory messages returned by the admission webhook
	// even when the request was accepted. Mirrors the kubectl warning experience.
	Warnings []string `json:"warnings,omitempty"`
	// Violations is a structured list of field-level errors from admission or
	// validation. Populated when Accepted=false and kubernetes returns Status details.
	Violations []ApplyViolation `json:"violations,omitempty"`

	// Payload carries the platform team's curated view of the submitted CR,
	// evaluated from serve.config.response at apply time.
	//
	// At apply time .status is not yet available — payload fields that
	// reference status resolve to "". The caller should poll
	// GET /api/v1/resources/{kind}/{ns}/{name} for live status values.
	//
	// Omitted when the CRD has no serve.config.response declared.
	Payload map[string]interface{} `json:"payload,omitempty"`

	// Clusters carries per-cluster results for fan-out applies.
	// Populated when serve.clusters is declared; absent for local-only applies.
	Clusters []ClusterApplyResult `json:"clusters,omitempty"`

	// PartialSuccess is true when at least one cluster accepted but not all.
	PartialSuccess bool `json:"partialSuccess,omitempty"`
}

// ClusterApplyResult is the per-cluster outcome for a fan-out apply.
type ClusterApplyResult struct {
	Cluster    string           `json:"cluster"`
	Accepted   bool             `json:"accepted"`
	Name       string           `json:"name,omitempty"`
	Namespace  string           `json:"namespace,omitempty"`
	PollURL    string           `json:"pollUrl,omitempty"`
	Message    string           `json:"message,omitempty"`
	Warnings   []string         `json:"warnings,omitempty"`
	Violations []ApplyViolation `json:"violations,omitempty"`
}

// ApplyViolation is one field-level error returned from Kubernetes on a failed apply.
type ApplyViolation struct {
	// Field is the dot-notation path to the offending field (e.g. "spec.environment").
	Field string `json:"field,omitempty"`
	// Message is the human-readable error from Kubernetes or the admission webhook.
	Message string `json:"message"`
	// Severity is "error" (blocks apply) or "warning" (informational).
	Severity string `json:"severity,omitempty"`
}

// warningCapture collects Kubernetes API server warnings from a single request.
// It implements rest.WarningHandler and is safe for concurrent use.
type warningCapture struct {
	mu       sync.Mutex
	warnings []string
}

func (c *warningCapture) HandleWarningHeader(_ int, _ string, message string) {
	c.mu.Lock()
	c.warnings = append(c.warnings, message)
	c.mu.Unlock()
}

func (c *warningCapture) collect() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.warnings...)
}

// scopedDynamic returns a dynamic client that routes warnings to capture
// while reusing the existing HTTP transport from kube.
func scopedDynamic(kube kubeclient.Interface, capture *warningCapture) dynamic.Interface {
	cfg := kube.RestConfig()
	if cfg == nil {
		return kube.DynamicClient()
	}
	copy := *cfg
	copy.WarningHandler = capture
	d, err := dynamic.NewForConfig(&copy)
	if err != nil {
		return kube.DynamicClient()
	}
	return d
}

// applyHandler returns the http.HandlerFunc for POST /api/v1/apply.
// The auth middleware must wrap this handler before registration.
func applyHandler(
	kube kubeclient.Interface,
	clusters *ClusterRegistry,
	kat *katalog.Katalog,
) http.HandlerFunc {
	var notes orktypes.NoteRegistry
	if !kat.Empty() {
		notes = kat.UserNotes()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "only POST requests are supported")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ApplyResponse{
				Message: "failed to read request body",
			})
			return
		}

		// Decode into a raw map first to detect target vs full CR mode.
		var raw map[string]interface{}
		if err := json.Unmarshal(body, &raw); err != nil {
			writeJSON(w, http.StatusBadRequest, ApplyResponse{
				Message: fmt.Sprintf("invalid JSON: %v", err),
			})
			return
		}

		dryRun := r.URL.Query().Get("dryRun") == "true"
		overwrite := r.URL.Query().Get("overwrite") == "true" // SSA forceConflict
		override := r.URL.Query().Get("override") == "true"   // TargetConflict

		var (
			obj       *unstructured.Unstructured
			crd       *orktypes.CRDEntry
			alias     string // non-empty when caller used an alias
			gvr       schema.GroupVersionResource
			patchBody []byte
		)

		// ── Format detection ──────────────────────────────────────────────────
		if isTargetRequest(raw) {
			// ── Target mode ───────────────────────────────────────────────────
			// Caller submitted flat fields alongside a target identifier.
			// The gateway builds the full CR from the serve field declaration.
			target, _ := raw["target"].(string)
			if strings.TrimSpace(target) == "" {
				writeJSON(w, http.StatusBadRequest, ApplyResponse{
					Message: `"target" must be a non-empty string`,
				})
				return
			}

			resolution := kat.LookupByTargetOrAlias(target)
			if resolution == nil {
				writeJSON(w, http.StatusBadRequest, ApplyResponse{
					Message: fmt.Sprintf(
						"unknown target %q — available: %s",
						target,
						strings.Join(kat.AvailableTargets(), ", "),
					),
				})
				return
			}
			crd = resolution.CRD
			alias = resolution.Alias

			if !crd.TargetModeEnabledFor(alias) {
				writeJSONError(w, http.StatusBadRequest, "target mode not enabled",
					fmt.Sprintf("Target mode is not enabled for '%q'", target),
				)
				return
			}

			built, err := orktarget.BuildCRFromTarget(raw, crd, notes)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, ApplyResponse{
					Message: err.Error(),
				})
				return
			}
			obj = built
			InjectProvenanceAnnotations(obj, crd.ServeTarget(), alias, OIDCSubFromContext(r.Context()))
			InjectServeIntentAnnotation(obj, raw)
			gvr = crd.GVR()

			// ─── Marshal built CR for the patch body ──────────────────────
			patchBody, err = json.Marshal(obj.Object)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, ApplyResponse{
					Message: "failed to marshal built CR",
				})
				return
			}

		} else {
			// ── Full CR mode ──────────────────────────────────────────────────
			// Built from the already-decoded raw map.
			full := unstructured.Unstructured{Object: raw}

			if full.GetKind() == "" || full.GetAPIVersion() == "" {
				writeJSON(w, http.StatusBadRequest, ApplyResponse{
					Message: `request must include either "target" (target mode) or "apiVersion" and "kind" (full CR mode)`,
				})
				return
			}

			crd = kat.LookupByKind(full.GetKind()).Entry()
			if crd == nil {
				writeJSON(w, http.StatusBadRequest, ApplyResponse{
					Message: fmt.Sprintf("unknown kind %q", full.GetKind()),
				})
				return
			}

			if !crd.ServeEnabled() {
				writeJSONError(w, http.StatusBadRequest, "serve not enabled",
					fmt.Sprintf("Serve is not enabled for kind %q", full.GetKind()),
				)
				return
			}

			// If the CRD has matchFields, try to find a matching target
			matchedTarget := ""
			if target := crd.EffectiveServeTargetForMap(full.Object); target != "" {
				matchedTarget = target
			}
			if matchedTarget != "" {
				logger.FromContext(r.Context()).Debug().
					Str("kind", full.GetKind()).
					Str("name", full.GetName()).
					Str("namespace", full.GetNamespace()).
					Str("target", matchedTarget).
					Msg("matched target")
			}

			// Determine the effective target for mode checking and provenance
			effectiveTarget := matchedTarget
			if effectiveTarget == "" {
				effectiveTarget = crd.ServeTarget()
			}

			if !crd.FullCRModeEnabledFor(effectiveTarget) {
				writeJSONError(w, http.StatusBadRequest, "CR mode not enabled",
					fmt.Sprintf("Full CR mode is not enabled for %q", effectiveTarget),
				)
				return
			}

			obj = &full

			// ─── Inject Provenance Annotations ────────────────────────────────────────
			InjectProvenanceAnnotations(obj, crd.ServeTarget(), matchedTarget, OIDCSubFromContext(r.Context()))
			if matchedTarget != "" {
				InjectFieldSelectorAnnotations(obj, matchedTarget, crd.FieldSelectorForTarget(matchedTarget))
			}

			// Resolve serve.name and serve.namespace when declared on the CRD.
			if err := resolveServeMeta(obj, crd, notes); err != nil {
				writeJSON(w, http.StatusBadRequest, ApplyResponse{
					Message: err.Error(),
				})
				return
			}
		}

		// ─── Marshal the modified CR ───────────────────────────────────────────────
		patchBody, err = json.Marshal(obj.Object)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ApplyResponse{
				Message: fmt.Sprintf("failed to marshal CR with annotations: %v", err),
			})
			return
		}
		gvr = crd.GVR()

		// ─── Resolve final effective target ──────────────────────────────────────────
		// Only override alias when a fieldSelector explicitly matched — never fall
		// back to the primary target here, since that would mask an explicitly
		// named target (e.g. "web") with the primary (e.g. "apifixture") and
		// bypass the routing surface conflict check.
		if effectiveTarget := crd.ServeTargetForFieldSelector(obj.Object); effectiveTarget != "" {
			alias = effectiveTarget
		}

		// ── Cluster routing ───────────────────────────────────────────────────────
		// Resolve apply targets for this CRD+alias. Cascade: target.clusters →
		// serve.clusters → local. Templates resolved against raw.
		targets, clusterErr := resolveClusterTargets(crd, alias, raw, notes, clusters, kube)
		if clusterErr != nil {
			writeJSON(w, http.StatusBadRequest, ApplyResponse{Message: clusterErr.Error()})
			return
		}

		// ── Force conflict ───────────────────────────────────────────────────────
		// Resolve force conflict: enabled if CRD/target permits it and request requests it
		forceConflictEnabled := crd.ServeForceConflictEnabledFor(alias)
		if overwrite && !forceConflictEnabled {
			writeJSONError(w, http.StatusBadRequest, "forceConflict not enabled",
				fmt.Sprintf("Force conflict is not enabled for %q", alias),
			)
			return
		}

		// If CRD/target enables forceConflict, treat as force even without ?overwrite=true
		if !overwrite && forceConflictEnabled {
			overwrite = true
		}

		if obj.GetName() == "" {
			writeJSON(w, http.StatusUnprocessableEntity, ApplyResponse{
				DryRun:  dryRun,
				Message: "name is required",
				Violations: []ApplyViolation{{
					Field:    "metadata.name",
					Message:  "name is required",
					Severity: "error",
				}},
			})
			return
		}

		if obj.GetNamespace() == "" {
			writeJSON(w, http.StatusUnprocessableEntity, ApplyResponse{
				DryRun:  dryRun,
				Message: "namespace is required",
				Violations: []ApplyViolation{{
					Field:    "metadata.namespace",
					Message:  "namespace is required",
					Severity: "error",
				}},
			})
			return
		}

		patchOpts := metav1.PatchOptions{
			FieldManager: konfig.FieldManagerGateway,
			Force:        boolPtr(overwrite),
		}
		if dryRun {
			patchOpts.DryRun = []string{metav1.DryRunAll}
		}

		tokenName := TokenNameFromContext(r.Context())
		payload := EvaluatePayload(obj.Object, crd, alias, notes)

		// ── Single local cluster — original response shape (no Clusters field) ──
		if len(targets) == 1 && targets[0].name == "" {
			effectiveKube := targets[0].kube

			if crd != nil {
				op := orktypes.ServeOpCreate
				existing, probeErr := effectiveKube.DynamicClient().
					Resource(gvr).
					Namespace(obj.GetNamespace()).
					Get(r.Context(), obj.GetName(), metav1.GetOptions{})
				if probeErr == nil {
					op = orktypes.ServeOpUpdate
					ann := existing.GetAnnotations()
					storedSurface := ann[labels.AnnotationServeAlias]
					if storedSurface == "" {
						storedSurface = ann[labels.AnnotationServeTarget]
					}
					incomingSurface := alias
					if incomingSurface == "" {
						incomingSurface = crd.ServeTarget()
					}
					if storedSurface != "" && storedSurface != incomingSurface {
						if !override {
							msg := fmt.Sprintf(
								"routing surface conflict: resource was created via %q, cannot update via %q without ?override=true",
								storedSurface, incomingSurface,
							)
							writeJSON(w, http.StatusConflict, ApplyResponse{
								Message: msg,
								Violations: []ApplyViolation{{
									Field:    "metadata.annotations",
									Message:  msg,
									Severity: "error",
								}},
							})
							return
						}

						// ── Resolve Targe Override ───────────────────────────────────────────────────────
						if override {
							if !crd.ServeTargetOverrideEnabledFor(storedSurface) {
								writeJSONError(w, http.StatusBadRequest, "target override not enabled",
									fmt.Sprintf("Target override is not enabled for %q", storedSurface),
								)
								return
							}
						}

						logger.FromContext(r.Context()).Warn().
							Str("from", storedSurface).
							Str("to", incomingSurface).
							Str("name", obj.GetName()).
							Str("namespace", obj.GetNamespace()).
							Msg("routing surface override")
					}
				}

				allowed, reason := crd.TokenAllowedFor(alias, tokenName, op, obj.GetNamespace(), orktypes.ServeClassResources)
				if !allowed {
					msg := reason.Message(tokenName, op, obj.GetKind(), obj.GetNamespace())
					writeJSON(w, http.StatusForbidden, ApplyResponse{
						Message: msg,
						Violations: []ApplyViolation{{
							Field:    "metadata",
							Message:  msg,
							Severity: "error",
						}},
					})
					return
				}
			}

			ns := obj.GetNamespace()
			capture := &warningCapture{}
			result, err := scopedDynamic(effectiveKube, capture).
				Resource(gvr).
				Namespace(ns).
				Patch(r.Context(), obj.GetName(), k8stypes.ApplyPatchType, patchBody, patchOpts)
			if err != nil {
				violations := extractViolations(err)
				message := err.Error()
				if len(violations) > 0 && violations[0].Message != "" {
					message = violations[0].Message
				}
				logger.FromContext(r.Context()).Warn().
					Str("kind", obj.GetKind()).
					Str("name", obj.GetName()).
					Bool("dryRun", dryRun).
					Err(err).
					Msg("gateway API: SSA rejected")
				writeJSON(w, http.StatusUnprocessableEntity, ApplyResponse{
					DryRun:     dryRun,
					Message:    message,
					Warnings:   capture.collect(),
					Violations: violations,
				})
				return
			}

			resolver := orktmpl.NewResolverFromMap(raw).WithUserNotes(notes)
			pollURL := resolvePollURL(result.GetKind(), result.GetNamespace(), result.GetName(), crd.GetServePollingConfig(), resolver)
			writeJSON(w, http.StatusOK, ApplyResponse{
				Accepted:   true,
				DryRun:     dryRun,
				Name:       result.GetName(),
				Namespace:  result.GetNamespace(),
				Kind:       result.GetKind(),
				APIVersion: result.GetAPIVersion(),
				PollURL:    pollURL,
				Warnings:   capture.collect(),
				Payload:    payload,
			})
			return
		}

		// ── Fan-out — apply to each resolved cluster target ───────────────────────
		incomingSurface := alias
		if incomingSurface == "" && crd != nil {
			incomingSurface = crd.ServeTarget()
		}
		resolver := orktmpl.NewResolverFromMap(raw).WithUserNotes(notes)

		var clusterResults []ClusterApplyResult
		for _, t := range targets {
			cr := ClusterApplyResult{Cluster: t.name}

			if crd != nil {
				op := orktypes.ServeOpCreate
				existing, probeErr := t.kube.DynamicClient().
					Resource(gvr).
					Namespace(obj.GetNamespace()).
					Get(r.Context(), obj.GetName(), metav1.GetOptions{})
				if probeErr == nil {
					op = orktypes.ServeOpUpdate
					ann := existing.GetAnnotations()
					storedSurface := ann[labels.AnnotationServeAlias]
					if storedSurface == "" {
						storedSurface = ann[labels.AnnotationServeTarget]
					}
					if storedSurface != "" && storedSurface != incomingSurface && !override {
						cr.Message = fmt.Sprintf(
							"routing surface conflict: resource was created via %q, cannot update via %q without ?override=true",
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
			}

			capture := &warningCapture{}
			result, err := scopedDynamic(t.kube, capture).
				Resource(gvr).
				Namespace(obj.GetNamespace()).
				Patch(r.Context(), obj.GetName(), k8stypes.ApplyPatchType, patchBody, patchOpts)
			if err != nil {
				violations := extractViolations(err)
				message := err.Error()
				if len(violations) > 0 && violations[0].Message != "" {
					message = violations[0].Message
				}
				logger.FromContext(r.Context()).Warn().
					Str("cluster", t.name).
					Str("kind", obj.GetKind()).
					Str("name", obj.GetName()).
					Err(err).
					Msg("gateway API: SSA rejected (fan-out)")
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

		writeJSON(w, http.StatusOK, ApplyResponse{
			Accepted:       anyAccepted,
			DryRun:         dryRun,
			Kind:           obj.GetKind(),
			APIVersion:     obj.GetAPIVersion(),
			PartialSuccess: anyAccepted && !allAccepted,
			Clusters:       clusterResults,
			Payload:        payload,
		})
	}
}

// resolveServeMeta resolves serve.name and serve.namespace on a full CR in-place.
// Called in full CR mode when the platform team has set these expressions on
// the CRD — the submitted spec fields are the resolver data source.
// resolveServeMeta resolves serve.name and serve.namespace on a full CR in-place.
// Called in full CR mode when the platform team has set these expressions on
// the CRD — the submitted spec fields are the resolver data source.
func resolveServeMeta(
	obj *unstructured.Unstructured,
	crd *orktypes.CRDEntry,
	notes orktypes.NoteRegistry,
) error {
	if crd.Serve == nil {
		return nil
	}

	// Build resolver from the submitted spec so expressions like
	// `{{ .repository }}` resolve against spec fields.
	data := map[string]interface{}{
		"metadata": obj.Object["metadata"],
		"spec":     obj.Object["spec"],
	}

	// Add labels and annotations as top-level fields for convenience
	if meta, ok := obj.Object["metadata"].(map[string]interface{}); ok {
		if labels, ok := meta["labels"].(map[string]interface{}); ok {
			data["labels"] = labels
		}
		if annotations, ok := meta["annotations"].(map[string]interface{}); ok {
			data["annotations"] = annotations
		}
	}
	resolver := orktmpl.NewResolverFromMap(data).WithUserNotes(notes)

	if crd.HasServeName() {
		name, err := resolver.Resolve(crd.Serve.Name)
		if err != nil || strings.TrimSpace(name) == "" {
			logger.Error().
				Str("serve.name", crd.Serve.Name).
				Err(err).
				Msg("serve.name could not be resolved")
			return fmt.Errorf(
				"serve.name %q could not be resolved: %w",
				crd.Serve.Name, err,
			)
		}
		name = strings.TrimSpace(name)
		if err := validateK8sName(name); err != nil {
			logger.Error().
				Str("serve.name", name).
				Err(err).
				Msg("serve.name is not a valid kubernetes name")
			return fmt.Errorf(
				"serve.name %q is not a valid kubernetes name: %w",
				name, err,
			)
		}
		obj.SetName(name)
	}

	if crd.HasServeNamespace() {
		ns, err := resolver.Resolve(crd.Serve.Namespace)
		if err != nil || strings.TrimSpace(ns) == "" {
			logger.Error().
				Str("serve.namespace", crd.Serve.Namespace).
				Err(err).
				Msg("serve.namespace could not be resolved")
			return fmt.Errorf(
				"serve.namespace %q could not be resolved: %w",
				crd.Serve.Namespace, err,
			)
		}
		ns = strings.TrimSpace(ns)
		if err := validateK8sName(ns); err != nil {
			logger.Error().
				Str("serve.namespace", ns).
				Err(err).
				Msg("serve.namespace is not a valid kubernetes namespace")
			return fmt.Errorf(
				"serve.namespace %q is not a valid kubernetes namespace: %w",
				ns, err,
			)
		}
		obj.SetNamespace(ns)
	}

	return nil
}

func boolPtr(b bool) *bool { return &b }

// InjectProvenanceAnnotations stamps the three serve provenance annotations onto
// obj before SSA. Empty strings are skipped — callers pass "" for fields that
// don't apply to the current request path (e.g. alias is "" on primary targets).
func InjectProvenanceAnnotations(obj *unstructured.Unstructured, target, alias, source string) {
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = make(map[string]string, 3)
	}
	if target != "" {
		ann[labels.AnnotationServeTarget] = target
	}
	if alias != "" {
		ann[labels.AnnotationServeAlias] = alias
	}
	if source != "" {
		ann[labels.AnnotationServeSource] = source
	}
	obj.SetAnnotations(ann)
}

// InjectServeIntentAnnotation stores the raw intent payload as a JSON-encoded
// annotation so the admission webhook can bind it as .request in validation
// rules, enabling intent-level gates before field translation.
func InjectServeIntentAnnotation(obj *unstructured.Unstructured, raw map[string]interface{}) {
	common.InjectAnnotationToObject(obj, raw, labels.AnnotationServeIntent)
}

// InjectFieldSelectorAnnotations adds the field selector target and selectors to the CR.
func InjectFieldSelectorAnnotations(obj *unstructured.Unstructured, target string, selector map[string]string) {
	if target == "" || len(selector) == 0 {
		return
	}
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = make(map[string]string)
	}
	ann[labels.AnnotationServeSelectorTarget] = target

	b, err := json.Marshal(selector)
	if err == nil {
		ann[labels.AnnotationServeSelector] = string(b)
	}
	obj.SetAnnotations(ann)
}

// extractViolations pulls field-level causes out of a Kubernetes Status error.
func extractViolations(err error) []ApplyViolation {
	var statusErr *k8serrors.StatusError
	if !errors.As(err, &statusErr) || statusErr.ErrStatus.Details == nil {
		return nil
	}
	causes := statusErr.ErrStatus.Details.Causes
	if len(causes) == 0 {
		return nil
	}
	vs := make([]ApplyViolation, 0, len(causes))
	for _, c := range causes {
		v := ApplyViolation{
			Field:   c.Field,
			Message: c.Message,
		}
		if c.Type == metav1.CauseTypeFieldValueInvalid ||
			c.Type == metav1.CauseTypeFieldValueRequired ||
			c.Type == metav1.CauseTypeFieldValueDuplicate ||
			c.Type == metav1.CauseTypeFieldValueNotSupported {
			v.Severity = "error"
		} else {
			v.Severity = "warning"
		}
		vs = append(vs, v)
	}
	return vs
}
