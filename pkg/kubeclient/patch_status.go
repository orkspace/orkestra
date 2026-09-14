// pkg/kubeclient/patch_status.go
package kubeclient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// PatchStatus applies a merge patch to the /status subresource of a CR.
//
// statusFields is the map of status fields to set — it is merged into the
// existing status, not replaced entirely. Fields not present in statusFields
// are left untouched.
//
// The patch is applied to the /status subresource, which requires the CRD to
// declare:
//
//	spec:
//	  versions:
//	    - name: v1
//	      subresources:
//	        status: {}   # ← this enables the status subresource
//
// If the CRD does not declare a status subresource, the API server returns a
// 404 "the server could not find the requested resource". Callers should treat
// this as a non-fatal condition — the CRD simply does not support status updates.
//
// PatchStatus uses merge patch (application/merge-patch+json) rather than
// strategic merge patch. Merge patch is simpler and sufficient for status
// updates — we are setting top-level status fields, not merging lists.
//
// Example statusFields:
//
//	{
//	  "conditions": [{"type": "Ready", "status": "True", ...}],
//	  "observedGeneration": 3,
//	  "phase": "Running",
//	  "endpoint": "my-site.default.svc.cluster.local"
//	}
//
// The API server wraps this in {"status": <statusFields>} before applying.
// Callers pass only the status contents — not the "status" wrapper key.
func (k *Kubeclient) PatchStatus(
	ctx context.Context,
	obj domain.Object,
	statusFields map[string]interface{},
	opts metav1.PatchOptions,
) error {
	if len(statusFields) == 0 {
		return nil
	}

	mapping, err := k.gvrFor(obj)
	if err != nil {
		return err
	}

	patch := map[string]interface{}{
		"status": statusFields,
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshalling status patch: %w", err)
	}

	namespace := obj.GetNamespace()

	_, err = k.dynamic.
		Resource(mapping.Resource).
		Namespace(namespace).
		Patch(
			ctx,
			obj.GetName(),
			types.MergePatchType,
			patchBytes,
			opts,
			"status",
		)

	return err
}

// ── Why merge patch and not strategic merge patch ─────────────────────────
//
// Strategic merge patch understands the structure of specific Kubernetes types —
// it knows that a conditions list should be merged by the "type" key rather than
// replaced entirely. This is the correct patch type for objects like Deployments
// where the API server has built-in knowledge of the schema.
//
// For CRD status subresources, the API server does not have built-in schema
// knowledge. Strategic merge patch falls back to replace semantics for unknown
// types. Merge patch is explicit about what it does: it merges the top-level
// keys of the patch into the existing object. This is what we want.
//
// The conditions list is always written in full — buildReadyCondition produces
// a complete conditions entry. For a simple "one condition" status model this
// is equivalent to a replace. For CRDs that manage multiple conditions via hooks,
// only the Ready condition is written by Orkestra; others are left untouched
// because they are not present in the patch at all.
//
// If a future Layer requires strategic merge patch semantics (e.g. merging
// conditions by type rather than replacing the list), the patch type can be
// changed per-field using a separate apply patch call.
