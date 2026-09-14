// pkg/kubeclient/helper.go
package kubeclient

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	sigs "sigs.k8s.io/controller-runtime/pkg/client"
)

func (k *Kubeclient) gvkFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return GVKFor(k, obj)
}

func (k *Kubeclient) gvrFor(obj runtime.Object) (*apimeta.RESTMapping, error) {
	return GVRFor(k, obj)
}

func (k *Kubeclient) resourceFor(obj sigs.Object) (dynamic.ResourceInterface, error) {
	return ResourceFor(k, obj)
}

func (k *Kubeclient) runApply(ctx context.Context, cfg runtime.ApplyConfiguration, opts metav1.ApplyOptions, subresource ...string) error {
	return RunApply(ctx, k, cfg, opts, subresource...)
}

func (k *Kubeclient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return GroupVersionKindFor(k, obj)

}

func (k *Kubeclient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return IsObjectNamespaced(k, obj)
}

// getFromStore attempts a cache-backed read for the requested object.
func (k *Kubeclient) getFromStore(
	obj runtime.Object,
	key domain.ObjectKey,
) (*unstructured.Unstructured, bool, string) {
	fn := k.GetStoreFor()
	if fn == nil {
		return nil, false, "storeFor not wired"
	}

	gvks, _, err := k.Scheme().ObjectKinds(obj)
	if err != nil || len(gvks) == 0 {
		return nil, false, fmt.Sprintf(
			"scheme cannot resolve GVK: %v",
			err,
		)
	}

	gvk := gvks[0]
	store := fn(gvk)
	if store == nil {
		return nil, false, "no informer store for " + gvk.String()
	}

	storeKey := key.Name
	if key.Namespace != "" {
		storeKey = key.Namespace + "/" + key.Name
	}

	raw, exists, err := store.GetByKey(storeKey)
	if err != nil {
		return nil, false, "store.GetByKey error: " + err.Error()
	}
	if !exists || raw == nil {
		return nil, false, "key not in store"
	}

	if u, ok := raw.(*unstructured.Unstructured); ok {
		return u, true, ""
	}
	// Typed informers store the concrete Go type. Convert to unstructured so the
	// caller can FromUnstructured it into the target — same roundtrip, avoids a
	// direct type assertion that would only work for one specific type.
	rObj, ok := raw.(runtime.Object)
	if !ok {
		return nil, false, fmt.Sprintf(
			"store item is %T, not runtime.Object",
			raw,
		)
	}

	rawMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rObj)
	if err != nil {
		return nil, false, "ToUnstructured: " + err.Error()
	}

	return &unstructured.Unstructured{Object: rawMap}, true, ""
}
