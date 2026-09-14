// pkg/kubeclient/crud.go
package kubeclient

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// ── Reader ────────────────────────────────────────────────────────────────────

// Get fetches the object identified by namespace/name into into.
// Derives the API group and resource from the Go type via the scheme and mapper.
func (k *Kubeclient) Get(ctx context.Context, key domain.ObjectKey, obj domain.Object, opts metav1.GetOptions) error {
	namespace := key.Namespace
	name := key.Name

	if u, ok, reason := k.getFromStore(obj, key); ok {
		return runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj)
	} else {
		logger.Debug().
			Str("type", fmt.Sprintf("%T", obj)).
			Str("key", key.Namespace+"/"+key.Name).
			Str("reason", reason).
			Msg("ctrlclient.Get: cache miss — live API call")
	}

	mapping, err := k.gvrFor(obj)
	if err != nil {
		return err
	}

	var u *unstructured.Unstructured
	if namespace == "" {
		u, err = k.dynamic.Resource(mapping.Resource).Get(ctx, name, opts)
	} else {
		u, err = k.dynamic.Resource(mapping.Resource).Namespace(namespace).Get(ctx, name, opts)
	}
	if err != nil {
		return err
	}

	return runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj)
}

// ── Writer ────────────────────────────────────────────────────────────────────

// Apply performs a Kubernetes server-side apply.
func (k *Kubeclient) Apply(ctx context.Context, cfg runtime.ApplyConfiguration, opts metav1.ApplyOptions) error {
	return k.runApply(ctx, cfg, opts)
}

// Create creates obj in the cluster.
// Derives the API group and resource from the Go type via the scheme and mapper.
func (k *Kubeclient) Create(ctx context.Context, obj domain.Object, opts metav1.CreateOptions) error {
	mapping, err := k.gvrFor(obj)
	if err != nil {
		return err
	}

	u, err := ToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("convert to unstructured: %w", err)
	}
	ns := obj.GetNamespace()

	if ns == "" {
		_, err = k.dynamic.Resource(mapping.Resource).Create(ctx, u, opts)
	} else {
		_, err = k.dynamic.Resource(mapping.Resource).Namespace(ns).Create(ctx, u, opts)
	}
	return err
}

// Update updates obj in the cluster.
// Derives the API group and resource from the Go type via the scheme and mapper.
func (k *Kubeclient) Update(ctx context.Context, obj domain.Object, opts metav1.UpdateOptions) error {
	mapping, err := k.gvrFor(obj)
	if err != nil {
		return err
	}

	u, err := ToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("convert to unstructured: %w", err)
	}
	ns := obj.GetNamespace()

	if ns == "" {
		_, err = k.dynamic.Resource(mapping.Resource).Update(ctx, u, opts)
	} else {
		_, err = k.dynamic.Resource(mapping.Resource).Namespace(ns).Update(ctx, u, opts)
	}
	return err
}

// Patch applies patch to obj in the cluster.
// The patch body and type are computed by calling patch.Data(obj).
// Use kubeclient.MergeFrom or kubeclient.StrategicMergeFrom to build the patch,
// or pass sigs.MergeFrom / sigs.StrategicMergeFrom directly — they satisfy Patch.
func (k *Kubeclient) Patch(ctx context.Context, obj domain.Object, patch Patch, opts metav1.PatchOptions) error {
	mapping, err := k.gvrFor(obj)
	if err != nil {
		return err
	}

	data, err := patch.Data(obj)
	if err != nil {
		return fmt.Errorf("compute patch: %w", err)
	}

	ns := obj.GetNamespace()
	name := obj.GetName()

	if ns == "" {
		_, err = k.dynamic.Resource(mapping.Resource).Patch(ctx, name, patch.Type(), data, opts)
	} else {
		_, err = k.dynamic.Resource(mapping.Resource).Namespace(ns).Patch(ctx, name, patch.Type(), data, opts)
	}
	return err
}

// Delete removes obj from the cluster.
func (k *Kubeclient) Delete(ctx context.Context, obj domain.Object, opts metav1.DeleteOptions) error {
	mapping, err := k.gvrFor(obj)
	if err != nil {
		return err
	}
	ns := obj.GetNamespace()
	name := obj.GetName()

	if ns == "" {
		err = k.dynamic.Resource(mapping.Resource).Delete(ctx, name, opts)
	} else {
		err = k.dynamic.Resource(mapping.Resource).Namespace(ns).Delete(ctx, name, opts)
	}
	return err
}

// DeleteAllOf deletes all objects of the given type matching the supplied
// controller-runtime delete-all options.
func (k *Kubeclient) DeleteAllOf(ctx context.Context, obj domain.Object, deleteOpts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	resource, err := k.resourceFor(obj)
	if err != nil {
		return err
	}
	return resource.DeleteCollection(ctx, deleteOpts, listOpts)
}
