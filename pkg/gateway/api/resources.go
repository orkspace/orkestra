// pkg/gateway/api/resources.go
//
// GET  /api/v1/resources/{kind}/{namespace}/{name}  — read one CR
// GET  /api/v1/resources/{kind}/{namespace}         — list CRs in a namespace
// DELETE /api/v1/resources/{kind}/{namespace}/{name} — delete a CR
//
// These endpoints are for external callers (CI, Terraform, Slack bots) that
// need raw Kubernetes CR state without kubeconfig. The Control Center uses the
// runtime's /katalog/{crd}/cr/... endpoints (richer, informer-cached); these
// endpoints call the Kubernetes API directly via the dynamic client.
//
// Deletion protection is enforced by the admission webhook when enabled.
// This handler does not duplicate that check — it issues the DELETE and lets
// Kubernetes reject it if the webhook blocks.
//
// {kind} is the lowercased kind string or, when the CRD has serve.target set,
// the target value. The handler resolves the CRD via the kind lookup.
package api

import (
	"fmt"
	"net/http"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
)

// resourcesHandler returns the http.HandlerFunc for /api/v1/resources/... routes.
// URL pattern: /api/v1/resources/{kind}/{namespace}[/{name}]
// The auth middleware must wrap this handler before registration.
//
// Two lookup modes are supported:
//   - Kind lookup: resolves the raw kind string to a CRD entry
//   - Target lookup: resolves the target identifier to a CRD entry (when serve.target is set)
func resourcesHandler(
	kube kubeclient.Interface,
	clusters *ClusterRegistry,
	kat *katalog.Katalog,
) http.HandlerFunc {
	var notes orktypes.NoteRegistry
	if !kat.Empty() {
		notes = kat.UserNotes()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		kind, ns, name, err := parsePath(r.URL.Path)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid path",
				fmt.Sprintf("could not parse path: %v", err),
			)
			return
		}

		// ─── Debug: Log available kinds ──────────────────────────────────────────────
		logger.FromContext(r.Context()).Debug().
			Str("kind", kind).
			Str("availableKinds", strings.Join(kat.ListKinds(), ", ")).
			Msg("resourcesHandler: lookup")

		// Resolve CRD entry — accept Kubernetes Kind, serve target, or alias.
		resolution := kat.LookupByKindOrAlias(kind)
		if resolution == nil {
			writeJSONError(w, http.StatusNotFound, "kind not found",
				fmt.Sprintf("unknown kind %q", kind),
			)
			return
		}
		crd := resolution.CRD
		alias := resolution.Alias

		if !crd.ServeEnabled() {
			writeJSONError(w, http.StatusBadRequest, "serve not enabled",
				fmt.Sprintf("Serve is not enabled for kind %q", kind),
			)
			return
		}

		// ── Cluster routing ───────────────────────────────────────────────────────
		// ?cluster=<name> explicitly targets a registered remote cluster.
		effectiveKube := kube
		if clusterName := r.URL.Query().Get("cluster"); clusterName != "" {
			c, ok := clusters.ClientFor(clusterName)
			if !ok {
				writeJSONError(w, http.StatusBadRequest, "cluster not registered",
					fmt.Sprintf("cluster %q is not registered in gateway.clusters", clusterName),
				)
				return
			}
			effectiveKube = c
		} else {
			derived, clusterErr := resolveReadCluster(crd, alias, notes, clusters, kube)
			if clusterErr != nil {
				writeJSONError(w, http.StatusBadRequest, "cluster routing error", clusterErr.Error())
				return
			}
			effectiveKube = derived
		}

		switch r.Method {
		case http.MethodGet:
			if name == "" {
				listResources(w, r, effectiveKube, ns, crd, alias, notes)
			} else {
				getResource(w, r, effectiveKube, ns, name, crd, alias, notes)
			}
		case http.MethodDelete:
			if name == "" {
				writeJSONError(w, http.StatusBadRequest, "name required",
					"DELETE requires a resource name",
				)
				return
			}
			deleteResource(w, r, effectiveKube, ns, name, crd, alias)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed",
				fmt.Sprintf("method %q is not supported for /api/v1/resources", r.Method),
			)
		}
	}
}

// getResource handles GET /api/v1/resources/{kind}/{ns}/{name}.
// Returns the full stored CR with the serve.config.response payload evaluated
// against the live CR — status fields written by the runtime are available.
func getResource(
	w http.ResponseWriter,
	r *http.Request,
	kube kubeclient.Interface,
	ns, name string,
	crd *orktypes.CRDEntry,
	alias string,
	notes orktypes.NoteRegistry,
) {
	// When the CRD declares serve.tokens, the authenticated token must
	// have permission to perform the operation it is attempting.
	if !checkServePermission(w, r, crd, orktypes.ServeClassResources, orktypes.ServeOpGet, ns, alias) {
		return
	}

	gvr := crd.GVR()
	obj, err := kube.DynamicClient().Resource(gvr).Namespace(ns).
		Get(r.Context(), name, metav1.GetOptions{})
	if err != nil {
		writeKubeError(w, err)
		return
	}

	response := obj.Object

	// ── Resolve alias from provenance annotation ──────────────────────────
	// The CR carries the surface it was created through. Use it to drive
	// alias-specific response config without requiring the caller to pass ?target=.
	// serve-alias annotation wins; fall back to serve-target, then CRD-level.
	if alias == "" {
		ann := obj.GetAnnotations()
		if a := ann[labels.AnnotationServeAlias]; a != "" {
			alias = a
		}
	}

	// ── Step 1: Check for ?field= query param (lightweight polling) ──────
	field := r.URL.Query().Get("field")
	if field != "" {
		value, ok := resolveScalarField(response, field)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid field path",
				fmt.Sprintf("field %q not found", field),
			)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"name":  name,
			"field": field,
			"value": value,
		})
		return
	}

	// ── Step 2: Apply exclusions ──────────────────────────────────────────
	ApplyExclusions(response, crd, alias, notes)

	// ── Step 3: Evaluate payload ──────────────────────────────────────────
	payload := EvaluatePayload(response, crd, alias, notes)

	// ── Step 4: Return response ──────────────────────────────────────────
	if payload != nil {
		cfg := crd.ServeResponseConfigFor(alias)
		if cfg != nil && !cfg.UseDefault() {
			writeJSON(w, http.StatusOK, payload)
			return
		}
		response["payload"] = payload
	}

	writeJSON(w, http.StatusOK, response)
}

// listResources handles GET /api/v1/resources/{kind}/{ns}.
// Returns a paginated list of CRs. Supports Kubernetes continuation tokens
// via the ?continue= query param for server-side pagination.
func listResources(
	w http.ResponseWriter,
	r *http.Request,
	kube kubeclient.Interface,
	ns string,
	crd *orktypes.CRDEntry,
	alias string,
	notes orktypes.NoteRegistry,
) {
	if !checkServePermission(w, r, crd, orktypes.ServeClassResources, orktypes.ServeOpList, ns, alias) {
		return
	}

	p := parsePagination(r)

	listOpts := metav1.ListOptions{
		Limit:    int64(p.Limit),
		Continue: p.Continue,
	}

	gvr := crd.GVR()
	list, err := kube.DynamicClient().
		Resource(gvr).
		Namespace(ns).
		List(r.Context(), listOpts)
	if err != nil {
		writeKubeError(w, err)
		return
	}

	field := r.URL.Query().Get("field")
	cfg := crd.ServeResponseConfigFor(alias)

	// ── Build items ──────────────────────────────────────────────────────
	type responseItem struct {
		Object  map[string]interface{} `json:"object,omitempty"`
		Payload map[string]interface{} `json:"payload,omitempty"`
	}

	items := make([]responseItem, 0, len(list.Items))
	for _, item := range list.Items {
		obj := item.Object

		ApplyExclusions(obj, crd, alias, notes)

		var payload map[string]interface{}
		if cfg != nil && cfg.HasPayload() {
			payload = EvaluatePayload(obj, crd, alias, notes)
		}

		entry := responseItem{Object: obj}
		if cfg != nil && !cfg.UseDefault() {
			entry.Object = nil
		}
		if payload != nil {
			entry.Payload = payload
		}

		items = append(items, entry)

		// ── ?field= query param ──────────────────────────────────────────
		if field != "" {
			value, ok := resolveScalarField(obj, field)
			if !ok {
				writeJSONError(w, http.StatusBadRequest, "invalid field path",
					fmt.Sprintf("field %q not found", field),
				)
				return
			}
			name, _ := obj["metadata"].(map[string]interface{})["name"].(string)
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"name":  name,
				"field": field,
				"value": value,
			})
			return
		}
	}

	// ── Return paginated response ───────────────────────────────────────
	writeJSON(w, http.StatusOK, utils.PaginatedResponse[responseItem]{
		Total:    len(items),
		Limit:    p.Limit,
		Offset:   p.Offset,
		Continue: list.GetContinue(),
		Items:    items,
	})
}

// deleteResource handles DELETE /api/v1/resources/{kind}/{ns}/{name}.
// Respects deletion protection — the Kubernetes webhook enforces it; the
// gateway surfaces the structured rejection rather than a raw API server error.
func deleteResource(
	w http.ResponseWriter,
	r *http.Request,
	kube kubeclient.Interface,
	ns, name string,
	crd *orktypes.CRDEntry,
	alias string,
) {
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "name required",
			"DELETE requires a resource name",
		)
		return
	}

	if !checkServePermission(w, r, crd, orktypes.ServeClassResources, orktypes.ServeOpDelete, ns, alias) {
		return
	}

	gvr := crd.GVR()
	err := kube.DynamicClient().Resource(gvr).Namespace(ns).
		Delete(r.Context(), name, metav1.DeleteOptions{})
	if err != nil {
		writeKubeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkServePermission is a single function used by all resource handlers
// (get, list, delete, schema). alias is the serve alias from the request
// path — resource endpoints that carry no alias context pass "".
// TokenAllowedFor resolves alias-specific tokens before falling back to
// CRD-level tokens, then delegates to ServeConfig.TokenAllowed.
//
// Returns true when the request should proceed. Writes the 403 response and
// returns false when it should not — callers must return immediately.
func checkServePermission(
	w http.ResponseWriter,
	r *http.Request,
	crd *orktypes.CRDEntry,
	class orktypes.ServeEndpointClass,
	op, ns, alias string,
) bool {
	if crd == nil {
		return true
	}

	tokenName := TokenNameFromContext(r.Context())
	allowed, reason := crd.TokenAllowedFor(alias, tokenName, op, ns, class)
	if !allowed {
		writeJSONError(w, http.StatusForbidden, "permission denied", reason.Message(tokenName, op, crd.APITypes.Kind, ns))
		return false
	}
	return true
}
