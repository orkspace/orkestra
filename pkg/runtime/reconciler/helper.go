package reconciler

import (
	"context"
	"fmt"
	"slices"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// getLatestObject fetches the current state of the object directly from the
// Kubernetes API server, bypassing any local informer cache.
//
// It is used when label or annotation decisions depend on the latest cluster
// state and cannot tolerate cache staleness (e.g., after a Katalog reload or
// when reconciler configuration changes). The fresh object is returned as the
// same concrete type PTR (e.g., *unstructured.Unstructured or a typed client).
//
// Parameters:
//   - ctx:        context for the API call
//   - namespace:  namespace of the object (empty for cluster-scoped resources)
//   - name:       name of the object
//
// Returns:
//   - The freshly fetched object, already converted to PTR
//   - An error if the API Get fails (e.g., resource not found, network issue)
func (r *GenericReconciler[PTR]) getLatestObject(ctx context.Context, namespace, name string) (PTR, error) {
	gvr := r.crd.GVR()
	var unstructuredObj *unstructured.Unstructured
	var err error

	if namespace == "" {
		unstructuredObj, err = r.kube.DynamicClient().Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	} else {
		unstructuredObj, err = r.kube.DynamicClient().Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return any(unstructuredObj).(PTR), err
	}
	// Convert back to PTR
	return any(unstructuredObj).(PTR), nil
}

// ── Finalizer management ──────────────────────────────────────────────────────

// ensureFinalizers adds the finalizers declared in the CRD's OperatorBox to the
// given object if they are not already present.
//
// Finalizers block deletion of a resource until the controller has performed
// necessary cleanup (e.g., deleting child resources, releasing external resources).
// The list of finalizers is defined per CRD in the Katalog under spec.crds[].operatorBox.finalizers.
//
// This function is idempotent: it checks which finalizers from the configured
// list are missing and appends them. It never removes finalizers; removal is
// handled separately in the finalizer reconciliation logic (typically after
// successful cleanup).
//
// An event is emitted when finalizers are added, providing an audit trail.
//
// Edge cases:
//   - If the CRD has no finalizers configured, this function does nothing.
//   - If `crd.RemoveFinalizers` is true (a development flag), the function
//     currently only checks for existence but still adds missing finalizers.
//     This behaviour is marked for future review.
//
// Example:
//
//	Configured finalizers: ["protection.orkestra.io/finalizer"]
//	After calling ensureFinalizers, the resource's metadata.finalizers will include it.
func (r *GenericReconciler[PTR]) ensureFinalizers(ctx context.Context, obj PTR, box orktypes.OperatorBoxConfig) error {
	if len(box.Finalizers) == 0 {
		return nil
	}

	logger.Debug().
		Str("name", obj.GetName()).
		Any("crd finalizers", box.Finalizers).
		Msgf("checking finalizers: %v", obj.GetFinalizers())

	needsUpdate := false
	for _, f := range box.Finalizers {
		if !ContainsFinalizer(obj, f) {
			needsUpdate = true
			break
		}
	}
	if !needsUpdate {
		return nil
	}

	newFinalizers := obj.GetFinalizers()
	for _, f := range box.Finalizers {
		if !ContainsFinalizer(obj, f) {
			newFinalizers = append(newFinalizers, f)
		}
	}

	logger.Debug().
		Str("name", obj.GetName()).
		Msgf("adding finalizers: %v → %v", obj.GetFinalizers(), newFinalizers)

	r.event.Eventf(obj, corev1.EventTypeNormal, r.crd.APITypes.Kind+"FinalizerAdded",
		fmt.Sprintf("Added finalizers to %s/%s", obj.GetNamespace(), obj.GetName()))

	return r.kube.PatchFinalizers(ctx, obj, newFinalizers, metav1.PatchOptions{})
}

func (r *GenericReconciler[PTR]) removeFinalizers(ctx context.Context, obj PTR, box orktypes.OperatorBoxConfig) error {
	if len(obj.GetFinalizers()) == 0 {
		return nil
	}

	newFinalizers := make([]string, 0, len(obj.GetFinalizers()))
	for _, f := range obj.GetFinalizers() {
		if !slices.Contains(box.Finalizers, f) {
			newFinalizers = append(newFinalizers, f)
		}
	}

	if len(newFinalizers) == len(obj.GetFinalizers()) {
		return nil
	}

	logger.Debug().
		Str("name", obj.GetName()).
		Msgf("removing finalizers: %v → %v", obj.GetFinalizers(), newFinalizers)

	return r.kube.PatchFinalizers(ctx, obj, newFinalizers, metav1.PatchOptions{})
}

// ── Finalizer helpers — exported for custom reconcilers ───────────────────────

func AddFinalizer(o domain.Object, finalizer string) (updated bool) {
	if ContainsFinalizer(o, finalizer) {
		return false
	}
	o.SetFinalizers(append(o.GetFinalizers(), finalizer))
	return true
}

func RemoveFinalizer(o domain.Object, finalizer string) (updated bool) {
	f := o.GetFinalizers()
	length := len(f)
	index := 0
	for i := range length {
		if f[i] == finalizer {
			continue
		}
		f[index] = f[i]
		index++
	}
	o.SetFinalizers(f[:index])
	return length != index
}

func ContainsFinalizer(o domain.Object, finalizer string) bool {
	return slices.Contains(o.GetFinalizers(), finalizer)
}

func copyStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func toUnstructured(obj domain.Object) *unstructured.Unstructured {
	res, ok := domain.ToUnstructured(obj)
	if !ok {
		return nil
	}
	return res
}

// Inject live runtime metrics/health also to the object as annotation so that the gateway
// Uses it for metrics-level and health-level gating in validation and mutation rules.
// This respects crossAccess and endpoint security declaration
func (r *GenericReconciler[PTR]) injectRuntimeHealthAndMetrics(obj domain.Object, metricsMap, healthMap map[string]interface{}, healthOk bool) {
	unObj := toUnstructured(obj)

	if r.crd.CrossAccessEnabled() {
		if unObj != nil {
			common.InjectMetricsAnnotation(unObj, metricsMap)
		}
	}
	if r.crd.Endpoints.IsHealthEnabled() {
		if unObj != nil && healthOk {
			common.InjectHealthAnnotation(unObj, healthMap)
		}

	}
}
