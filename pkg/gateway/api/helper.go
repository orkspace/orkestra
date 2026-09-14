package api

import (
	"fmt"
	"net/http"
	"strings"

	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ─── Package-level aliases ──────────────────────────────────────────────────
// These aliases reduce import repetition across files and provide a single
// place to reference commonly used utilities in this package.

var (
	// writeJSON writes a JSON response with the given status code and data.
	writeJSON = utils.WriteJSON

	// writeJSONError writes a structured JSON error response.
	writeJSONError = utils.WriteJSONError

	// parsePagination extracts limit and offset from request query parameters.
	parsePagination = utils.ParsePagination

	// resolveScalarField resolves a dot-notation path (e.g., "status.phase")
	// against a map and returns the value as a string.
	resolveScalarField = orktypes.ResolveScalarField

	// validateK8sName tests that a string is a valid Kubernetes name.
	validateK8sName = utils.ValidKubernetesName

	// Others
	resourceChecker  = utils.NewResourceChecker
	nestedSlice      = utils.NestedSlice
	nestedMap        = utils.NestedMap
	deleteNestedPath = utils.DeleteNestedPath
	isTargetRequest  = orktarget.IsTargetRequest
)

// resolvePollURL builds the poll URL for the Gateway API response.
//
// It follows this resolution order:
//  1. Start with the default resource path: /api/v1/resources/{kind}/{namespace}/{name}
//  2. If poll.url is configured and resolves to a non-empty value, replace the default
//  3. If poll.field is configured and resolves to a non-empty value, append ?field=<value>
//  4. Return the final URL
//
// The resolver is used to evaluate templates in poll.url and poll.field.
func resolvePollURL(
	kind, namespace, name string,
	config *orktypes.ServePollingConfig,
	resolver *orktmpl.Resolver,
) string {
	// 1. Start with default
	pollURL := resourcePath(kind, namespace, name)

	// 2. Override with custom URL if configured
	if config != nil && config.URL != "" {
		if resolved, err := resolver.Resolve(config.URL); err == nil && resolved != "" {
			pollURL = resolved
		}
	}

	// 3. Append field if configured
	if config != nil && config.Field != "" {
		if resolved, err := resolver.Resolve(config.Field); err == nil && resolved != "" {
			pollURL = fmt.Sprintf("%s?field=%s", pollURL, resolved)
		}
	}

	return pollURL
}

// resourcePath builds the canonical /api/v1/resources/{kind}/{namespace}/{name}
// path for a CR. namespace is "" for cluster-scoped kinds.
func resourcePath(kind, namespace, name string) string {
	if namespace == "" {
		return fmt.Sprintf("/api/v1/resources/%s//%s", kind, name)
	}
	return fmt.Sprintf("/api/v1/resources/%s/%s/%s", kind, namespace, name)
}

// parsePath extracts kind, namespace, and optional name from
// /api/v1/resources/{kind}/{namespace}[/{name}]
func parsePath(path string) (kind, ns, name string, err error) {
	// Strip prefix — the handler is registered at /api/v1/resources/
	path = strings.TrimPrefix(path, "/api/v1/resources/")
	path = strings.TrimPrefix(path, "/") // normalise double-slash

	parts := strings.SplitN(path, "/", 3)
	switch len(parts) {
	case 2:
		return parts[0], parts[1], "", nil
	case 3:
		return parts[0], parts[1], parts[2], nil
	default:
		return "", "", "", fmt.Errorf("path must be /{kind}/{namespace}[/{name}], got %q", path)
	}
}

// writeKubeError maps common Kubernetes API errors to HTTP status codes.
func writeKubeError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found") || strings.Contains(msg, "NotFound"):
		http.Error(w, msg, http.StatusNotFound)
	case strings.Contains(msg, "forbidden") || strings.Contains(msg, "Forbidden"):
		http.Error(w, msg, http.StatusForbidden)
	default:
		http.Error(w, msg, http.StatusInternalServerError)
	}
}

// debugResourceExistenceOnSSAError checks resource existence and returns structured logs and response
// if the resource does not exist. Used by SSA.
func debugResourceExistenceOnSSAError(obj *unstructured.Unstructured, gvr schema.GroupVersionResource, w http.ResponseWriter, r *http.Request, kube kubeclient.Interface) bool {
	// ─── Debug: Log the GVR and check if the resource exists ──────────────────
	logger.FromContext(r.Context()).Debug().
		Str("gvr", gvr.String()).
		Str("namespace", obj.GetNamespace()).
		Str("name", obj.GetName()).
		Str("apiVersion", obj.GetAPIVersion()).
		Str("kind", obj.GetKind()).
		Msg("Gateway API: debugging SSA patch")

	// Check if the CRD exists
	_, crdErr := kube.DynamicClient().Resource(gvr).Namespace(obj.GetNamespace()).
		Get(r.Context(), obj.GetName(), metav1.GetOptions{})
	if crdErr != nil {
		logger.FromContext(r.Context()).Warn().
			Str("gvr", gvr.String()).
			Err(crdErr).
			Msg("Gateway API: resource not found or CRD not registered")
		writeJSON(w, http.StatusUnprocessableEntity, ApplyResponse{
			Message: "resource not found",
			Violations: []ApplyViolation{{
				Field:    "metadata",
				Message:  "resource not found",
				Severity: "error",
			}},
		})
		return false
	}
	return true
}
