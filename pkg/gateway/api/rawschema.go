package api

import (
	"fmt"
	"net/http"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RawSchemaResponse is returned by GET /api/v1/raw-schema?kind=<k>.
//
// It exposes the raw Kubernetes OpenAPI v3 schema for a CRD alongside the serve
// metadata destinations (labels, annotations). The caller receives the full
// structural picture and decides how to map their fields — no Orkestra
// abstraction is applied.
//
// Intended for:
//   - Callers who want direct access to the CRD spec without serve field config.
//   - Developers building tooling that understands Kubernetes CR structure.
//   - Advanced use cases where serve.fields is not declared or is deliberately
//     omitted in favour of letting the caller construct the full CR.
//
// Companion: POST /api/v1/apply in full CR mode accepts a complete CR whose
// structure matches what this endpoint describes.
type RawSchemaResponse struct {
	// Kind is the resolved CRD kind string.
	Kind string `json:"kind"`

	// APIVersion is the CRD's group/version.
	APIVersion string `json:"apiVersion"`

	// Spec contains the OpenAPI v3 schema for the CRD's spec field.
	// Properties describes each spec field; Required lists the mandatory ones.
	Spec RawSchemaSection `json:"spec"`

	// Labels describes fields the serve expects in metadata.labels, when declared
	// in serve.labels. Empty when not configured.
	Labels map[string]orktypes.ServeFieldConfig `json:"labels,omitempty"`

	// Annotations describes fields the serve expects in metadata.annotations,
	// when declared in serve.annotations. Empty when not configured.
	Annotations map[string]orktypes.ServeFieldConfig `json:"annotations,omitempty"`
}

// RawSchemaSection is the spec portion of the raw schema.
type RawSchemaSection struct {
	// Properties is the raw OpenAPI v3 properties map for the spec.
	// Values are the schema objects as returned by the Kubernetes API.
	Properties map[string]interface{} `json:"properties,omitempty"`

	// Required lists the spec field names that are required.
	Required []string `json:"required,omitempty"`
}

// rawSchemaHandler serves GET /api/v1/raw-schema?kind=<k>.
//
// It fetches the CRD definition from the Kubernetes API server and returns the
// spec schema alongside any serve-declared label and annotation fields. The
// caller sees all four CR destinations (spec, labels, annotations, and implicit
// metadata like name/namespace) and is responsible for mapping their data.
//
// By default, it querries the API server in the gateway cluster
// Pass ?cluster=<name> to target a remote cluster managed by the gateway
// Unlike GET /api/v1/schema?target=<t>, this endpoint:
//   - Requires ?kind= (the Kubernetes kind string, not the serve target).
//   - Returns raw OpenAPI properties, not the curated ServeFieldConfig list.
//   - Works even when serve.fields is not declared on the CRD entry.
//   - Is intended for callers who will use full CR mode in POST /api/v1/apply.
func rawSchemaHandler(
	kube kubeclient.Interface,
	clusters *ClusterRegistry,
	kat *katalog.Katalog,
) http.HandlerFunc {
	var notes orktypes.NoteRegistry
	if !kat.Empty() {
		notes = kat.UserNotes()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "only GET requests are supported")
			return
		}

		kind := r.URL.Query().Get("kind")
		if kind == "" {
			writeJSONError(w, http.StatusBadRequest, "missing parameter", `"kind" query parameter is required`)
			return
		}

		apiVersion := r.URL.Query().Get("apiVersion")

		var crd *orktypes.CRDEntry
		if apiVersion != "" && kind != "" {
			crd = kat.LookupByAPIVersionAndKind(apiVersion, kind).Entry()
			if crd == nil {
				// Try kind-only as fallback, but note the mismatch
				crd = kat.LookupByKind(kind).Entry()
				if crd != nil {
					// ─── Return the warning as the response ──────────────
					writeJSON(w, http.StatusBadRequest, utils.H{
						"error":      "apiVersion mismatch",
						"message":    fmt.Sprintf("kind %q found, but apiVersion %q did not match the stored version %q", kind, apiVersion, crd.APIVersion()),
						"kind":       kind,
						"apiVersion": crd.APIVersion(),
					})
					return
				}
			}
		} else {
			crd = kat.LookupByKind(kind).Entry()
		}

		if crd == nil {
			writeJSONError(w, http.StatusNotFound, "kind not found",
				fmt.Sprintf("kind %q not found in the Katalog", kind),
			)
			return
		}

		// Permission check — schema class, get operation.
		tokenName := TokenNameFromContext(r.Context())
		if crd.Serve != nil && crd.Serve.HasTokenRestrictions() {
			allowed, reason := crd.Serve.TokenAllowed(
				tokenName, orktypes.ServeOpGet, "", orktypes.ServeClassSchema,
			)
			if !allowed {
				writeJSONError(w, http.StatusForbidden, "permission denied",
					reason.Message(tokenName, orktypes.ServeOpGet, kind, ""),
				)
				return
			}
		}

		// Fetch the CRD definition from the Kubernetes API.
		crdGVR := schema.GroupVersionResource{
			Group:    "apiextensions.k8s.io",
			Version:  "v1",
			Resource: "customresourcedefinitions",
		}

		// CRD name is <plural>.<group>.
		crdName := crd.APITypes.Plural + "." + crd.APITypes.Group

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
			derived, clusterErr := resolveReadCluster(crd, "", notes, clusters, kube)
			if clusterErr != nil {
				writeJSONError(w, http.StatusBadRequest, "cluster routing error", clusterErr.Error())
				return
			}
			effectiveKube = derived
		}

		dynamicClient := effectiveKube.DynamicClient()
		// Create a resource checker
		checker := resourceChecker(dynamicClient)
		defer checker.InvalidateAll()

		// Check if the CRD exists
		if !checker.Exists(r.Context(), crdGVR, "", crdName) {
			writeJSONError(w, http.StatusNotFound, "CRD not found",
				fmt.Sprintf("CRD %q not found", crdName),
			)
			return
		}

		// Fetch the CRD definition from the Kubernetes API.
		obj, err := dynamicClient.
			Resource(crdGVR).
			Get(r.Context(), crdName, metav1.GetOptions{})
		if err != nil {
			writeKubeError(w, err)
			return
		}

		// Navigate to the OpenAPI schema for the storage version.
		// The storage version is the one with storage: true in the CRD spec.
		props, required := extractStorageSpecSchema(obj.Object)

		resp := RawSchemaResponse{
			Kind:       crd.APITypes.Kind, // Use the stored kind for consistency
			APIVersion: crd.APIVersion(),
			Spec: RawSchemaSection{
				Properties: props,
				Required:   required,
			},
		}

		// Attach serve label and annotation fields when declared.
		if crd.Serve != nil {
			if labels := crd.ServeLabels(); len(labels) > 0 {
				resp.Labels = labels
			}
			if annotations := crd.ServeAnnotations(); len(annotations) > 0 {
				resp.Annotations = annotations
			}
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// extractStorageSpecSchema navigates the CRD unstructured object to find the
// OpenAPI v3 spec schema for the storage version (storage: true).
//
// Returns nil, nil when the storage version cannot be found — callers render
// an empty properties map rather than an error.
func extractStorageSpecSchema(
	crdObj map[string]interface{},
) (properties map[string]interface{}, required []string) {
	versions, ok := nestedSlice(crdObj, "spec", "versions")
	if !ok {
		return nil, nil
	}

	for _, v := range versions {
		ver, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		// Check if this is the storage version.
		storage, _ := ver["storage"].(bool)
		if !storage {
			continue
		}

		// Navigate to the spec schema.
		specSchema, ok := nestedMap(ver, "schema", "openAPIV3Schema", "properties", "spec")
		if !ok {
			return nil, nil
		}

		props, _ := specSchema["properties"].(map[string]interface{})
		if props == nil {
			return nil, nil
		}

		var req []string
		if reqRaw, ok := specSchema["required"].([]interface{}); ok {
			for _, r := range reqRaw {
				if s, ok := r.(string); ok {
					req = append(req, s)
				}
			}
		}
		return props, req
	}

	return nil, nil
}
