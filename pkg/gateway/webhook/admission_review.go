// webhook/admission_review.go — Kubernetes AdmissionReview types.
//
// These mirror the Kubernetes admissionregistration.k8s.io/v1 types.
// Declared here to keep the webhook package self-contained without importing
// the full k8s.io/api module where it isn't needed.
//
// The Kubernetes API server sends an AdmissionReview to /validate and /mutate.
// Orkestra reads the Request, evaluates rules from the Katalog, and writes
// the Response. The API server reads the Response and either stores the object
// (allowed) or rejects it (denied).
package webhook

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AdmissionReview is the top-level wrapper sent by the API server.
// The same struct is used for both request and response directions.
type AdmissionReview struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Request    *AdmissionRequest  `json:"request,omitempty"`
	Response   *AdmissionResponse `json:"response,omitempty"`
}

// AdmissionRequest contains the object the API server is asking about.
type AdmissionRequest struct {
	UID       string                      `json:"uid"`
	Kind      metav1.GroupVersionKind     `json:"kind"`
	Resource  metav1.GroupVersionResource `json:"resource"`
	Name      string                      `json:"name,omitempty"`
	Namespace string                      `json:"namespace,omitempty"`
	Operation string                      `json:"operation"`
	Object    json.RawMessage             `json:"object,omitempty"`
	OldObject json.RawMessage             `json:"oldObject,omitempty"`
	DryRun    *bool                       `json:"dryRun,omitempty"`
}

// AdmissionResponse is what Orkestra writes in reply to an AdmissionRequest.
type AdmissionResponse struct {
	UID       string           `json:"uid"`
	Allowed   bool             `json:"allowed"`
	Status    *AdmissionStatus `json:"status,omitempty"`
	Patch     []byte           `json:"patch,omitempty"`
	PatchType *string          `json:"patchType,omitempty"`
	Warnings  []string         `json:"warnings,omitempty"`
}

// AdmissionStatus is the rejection reason returned when Allowed is false.
//
// Message stays a single verbose string — field path, rule text, and the
// received value all concatenated — because that's what shows up in
// `kubectl apply`'s error output and needs to be self-contained there.
// Details.Causes carries the same denials as clean, structured per-field
// entries instead: this is what the real Kubernetes API server relays into
// any client's error response (metav1.Status.Details), which is how
// pkg/gateway/api's ApplyResponse.Violations gets populated for the
// Gateway API — used by the Control Center form and any other client that
// wants to show a field-focused error instead of the raw kubectl-style string.
type AdmissionStatus struct {
	Message string                `json:"message"`
	Code    int32                 `json:"code"`
	Details *metav1.StatusDetails `json:"details,omitempty"`
}

const jsonPatchType = "JSONPatch"
