// pkg/reconciler/rollback.go
//
// Rollback implementation for GenericReconciler.
//
// Three responsibilities:
//
//  1. snapshotSpec — writes the current spec to the previous-spec annotation
//     before any spec change is applied. Called when generation changes.
//
//  2. shouldRollback — reads the operatorbox health state and evaluates
//     the rollback trigger. Returns true when rollback should activate.
//
//  3. runRollback — applies the onRollback resource group using the previous
//     spec hydrated into the resolver as .previous.*.
//
// The rollback phase state lives in status.phase. The reconciler checks it
// at the top of reconcileImpl and blocks normal reconciliation while active.
// The only exit from rollback is a spec generation change.
package reconciler

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// ── Spec snapshotting ─────────────────────────────────────────────────────────

// snapshotSpec writes the current spec to the previous-spec annotation.
// Called when the reconciler detects a generation change (spec has been updated).
// The snapshot is taken BEFORE the new spec is applied — it captures the last
// known good state.
//
// The spec is gzip-compressed and base64-encoded to keep the annotation small.
// A typical CRD spec compresses to a few hundred bytes.
func (r *GenericReconciler[PTR]) snapshotSpec(ctx context.Context, obj PTR) error {
	u, ok := any(obj).(*unstructured.Unstructured)
	if !ok {
		return nil // only unstructured mode supports snapshotting
	}

	spec, found, err := unstructured.NestedMap(u.Object, "spec")
	if err != nil || !found {
		return nil // no spec to snapshot
	}

	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("snapshot: marshaling spec: %w", err)
	}

	// Gzip + base64 to keep annotation size manageable
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("snapshot: compressing spec: %w", err)
	}
	w.Close()

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	// Patch only the spec snapshot annotation — RollbackGenerationAnnotation
	// is written separately by markRollbackActive when rollback actually triggers.
	patchData := fmt.Sprintf(
		`{"metadata":{"annotations":{%q:%q}}}`,
		orktypes.PreviousSpecAnnotation, encoded,
	)

	_, err = r.kube.DynamicClient().
		Resource(r.crd.GVR()).
		Namespace(obj.GetNamespace()).
		Patch(ctx, obj.GetName(), k8stypes.MergePatchType, []byte(patchData), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("snapshot: patching annotation: %w", err)
	}

	logger.Debug().
		Str("crd", r.crd.GVKString()).
		Str("name", obj.GetName()).
		Int64("generation", obj.GetGeneration()).
		Msg("rollback: spec snapshot written")

	return nil
}

// readPreviousSpec reads and decompresses the previous spec from the annotation.
// Returns nil when no snapshot exists.
func readPreviousSpec(obj domain.Object) map[string]interface{} {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil
	}

	encoded, ok := annotations[orktypes.PreviousSpecAnnotation]
	if !ok || encoded == "" {
		return nil
	}

	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}

	r, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil
	}
	defer r.Close()

	var spec map[string]interface{}
	if err := json.NewDecoder(r).Decode(&spec); err != nil {
		return nil
	}

	return spec
}

// ── Trigger evaluation ────────────────────────────────────────────────────────

// rollbackFailureHistory tracks per-CR failure timestamps for window-based triggers.
// Key: "namespace/name", Value: timestamps newest-first.
// This is in-process state — reset on restart. The restart case is safe:
// the failure counter in status tracks consecutive failures persistently.
type rollbackFailureHistory struct {
	times []time.Time
}

// record adds a failure timestamp and trims the history to the required window.
func (h *rollbackFailureHistory) record(required int) {
	h.times = append([]time.Time{time.Now()}, h.times...)
	if len(h.times) > required*2 {
		h.times = h.times[:required*2] // keep a 2x buffer
	}
}

// shouldRollback returns true when the rollback trigger conditions are met.
// Uses the effective trigger from DerivedRollback so that rollBackOnError: true
// uses the correct default threshold even when no explicit rollback block is set.
func (r *GenericReconciler[PTR]) shouldRollback(
	consecutiveFailures int,
	history *rollbackFailureHistory,
) bool {
	derived := r.crd.OperatorBox.DerivedRollback()
	if derived == nil {
		return false
	}
	return derived.Trigger.ShouldTrigger(history.times)
}

// isRollbackActive returns true when the CR is currently in the rolled-back phase.
func isRollbackActive(obj domain.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	rollbackGen, ok := annotations[orktypes.RollbackGenerationAnnotation]
	if !ok {
		return false
	}

	// Rollback is active if the generation at which rollback triggered
	// matches the current generation. If the user has updated the spec
	// (new generation), rollback is no longer active.
	currentGen := fmt.Sprintf("%d", obj.GetGeneration())
	return rollbackGen == currentGen
}

// ── Rollback execution ────────────────────────────────────────────────────────

// runRollback applies the rollback resource group using either the explicit
// onRollback: templates or the derived templates from rollBackOnError: true.
//
// Derived path (rollBackOnError: true, no explicit onRollback):
//   - Templates are the reconcile:true declarations from onCreate/onReconcile.
//   - Resolver has .spec.* replaced with the previous spec — templates run unchanged.
//
// Explicit path (onRollback: block declared):
//   - Uses the declared templates as-is.
//   - Resolver has .previous.* injected — templates use .previous.spec.* references.
func (r *GenericReconciler[PTR]) runRollback(ctx context.Context, resolver *orktmpl.Resolver, obj PTR) error {
	rollback := r.crd.OperatorBox.DerivedRollback()
	if rollback == nil || rollback.OnRollback == nil {
		logger.Info().
			Str("crd", r.crd.GVKString()).
			Str("name", obj.GetName()).
			Msg("rollback: active — no onRollback templates declared, blocking normal reconcile")
		return nil
	}

	previousSpec := readPreviousSpec(obj)
	if previousSpec == nil {
		logger.Warn().
			Str("crd", r.crd.GVKString()).
			Str("name", obj.GetName()).
			Msg("rollback: no previous spec snapshot found — cannot apply rollback templates")
		return nil
	}

	kube, ok := kubeclient.FromContext(ctx)
	if !ok {
		return fmt.Errorf("rollback: kubeclient not found in context")
	}

	var rollbackResolver *orktmpl.Resolver
	usingDerived := r.crd.OperatorBox.RollBackOnError &&
		(r.crd.OperatorBox.Rollback == nil || r.crd.OperatorBox.Rollback.OnRollback == nil)

	if usingDerived {
		// Derived path: substitute .spec.* with previous spec so the same
		// template expressions work for both normal reconcile and rollback.
		rollbackResolver = resolver.WithSpecOverride(previousSpec)
	} else {
		// Explicit path: inject previous spec under .previous.* as documented.
		rollbackResolver = resolver.WithPrevious(previousSpec)
	}

	if err := r.runResourceGroup(ctx, kube, rollbackResolver, obj, rollback.OnRollback, true); err != nil {
		return fmt.Errorf("rollback: applying templates: %w", err)
	}

	logger.Info().
		Str("crd", r.crd.GVKString()).
		Str("name", obj.GetName()).
		Bool("derived", usingDerived).
		Msg("rollback: previous state re-applied")

	return nil
}

// markRollbackActive writes RollbackGenerationAnnotation = currentGen, signalling
// that rollback is active for this generation. isRollbackActive reads this annotation.
func (r *GenericReconciler[PTR]) markRollbackActive(ctx context.Context, obj PTR) error {
	patchData := fmt.Sprintf(
		`{"metadata":{"annotations":{%q:%q}}}`,
		orktypes.RollbackGenerationAnnotation, fmt.Sprintf("%d", obj.GetGeneration()),
	)

	_, err := r.kube.DynamicClient().
		Resource(r.crd.GVR()).
		Namespace(obj.GetNamespace()).
		Patch(ctx, obj.GetName(), k8stypes.MergePatchType, []byte(patchData), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("marking rollback active: %w", err)
	}

	logger.Warn().
		Str("crd", r.crd.GVKString()).
		Str("name", obj.GetName()).
		Int64("generation", obj.GetGeneration()).
		Msg("rollback: marked active")

	if r.rollbackTriggerFn != nil {
		r.rollbackTriggerFn()
	}
	return nil
}

// getFailureHistory returns the failure history for a CR key, creating it if absent.
// Caller must not hold rollbackMu.
func (r *GenericReconciler[PTR]) getFailureHistory(key string) *rollbackFailureHistory {
	r.rollbackMu.Lock()
	defer r.rollbackMu.Unlock()
	h, ok := r.rollbackHistory[key]
	if !ok {
		h = &rollbackFailureHistory{}
		r.rollbackHistory[key] = h
	}
	return h
}

// clearFailureHistory resets the failure history for a CR key after a successful reconcile.
func (r *GenericReconciler[PTR]) clearFailureHistory(key string) {
	r.rollbackMu.Lock()
	delete(r.rollbackHistory, key)
	r.rollbackMu.Unlock()
}

// clearRollback removes the rollback annotation, allowing normal reconciliation
// to resume after the spec has been corrected.
func (r *GenericReconciler[PTR]) clearRollback(ctx context.Context, obj PTR) error {
	patchData := fmt.Sprintf(
		`{"metadata":{"annotations":{%q:null,%q:null}}}`,
		orktypes.PreviousSpecAnnotation,
		orktypes.RollbackGenerationAnnotation,
	)

	_, err := r.kube.DynamicClient().
		Resource(r.crd.GVR()).
		Namespace(obj.GetNamespace()).
		Patch(ctx, obj.GetName(), k8stypes.MergePatchType, []byte(patchData), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("clearing rollback annotations: %w", err)
	}

	logger.Info().
		Str("crd", r.crd.GVKString()).
		Str("name", obj.GetName()).
		Msg("rollback: cleared — resuming normal reconciliation")

	if r.rollbackClearFn != nil {
		r.rollbackClearFn()
	}
	return nil
}
