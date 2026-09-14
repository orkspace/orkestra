package kubeclient

import (
	"context"
	"encoding/json"
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	sigs "sigs.k8s.io/controller-runtime/pkg/client"
)

// GVKFor resolves the object's GroupVersionKind from the client's scheme.
func GVKFor(c Interface, obj runtime.Object) (schema.GroupVersionKind, error) {
	gvks, _, err := c.Scheme().ObjectKinds(obj)
	if err != nil {
		return schema.GroupVersionKind{}, fmt.Errorf("unknown type %T: %w", obj, err)
	}
	if len(gvks) == 0 {
		return schema.GroupVersionKind{}, fmt.Errorf("no GVK registered for %T", obj)
	}
	return gvks[0], nil
}

// GVRFor resolves the REST mapping used to address the object's resource.
func GVRFor(c Interface, obj runtime.Object) (*apimeta.RESTMapping, error) {
	gvk, err := GVKFor(c, obj)
	if err != nil {
		return nil, err
	}

	mapping, err := c.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("no REST mapping for %s: %w", gvk, err)
	}
	return mapping, nil
}

// ResourceFor returns the dynamic resource interface at the object's scope.
func ResourceFor(c Interface, obj sigs.Object) (dynamic.ResourceInterface, error) {
	mapping, err := GVRFor(c, obj)
	if err != nil {
		return nil, err
	}

	resource := c.DynamicClient().Resource(mapping.Resource)

	if mapping.Scope.Name() == apimeta.RESTScopeNameNamespace {
		return resource.Namespace(obj.GetNamespace()), nil
	}
	return resource, nil
}

// ResourceForGVK resolves a dynamic resource interface for a GVK and
// applies the namespace when the mapped resource is namespaced.
func ResourceForGVK(c Interface, gvk schema.GroupVersionKind, namespace string) (dynamic.ResourceInterface, error) {
	mapping, err := c.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("no REST mapping for %s: %w", gvk, err)
	}

	resource := c.DynamicClient().Resource(mapping.Resource)

	if mapping.Scope.Name() == apimeta.RESTScopeNameNamespace {
		return resource.Namespace(namespace), nil
	}
	return resource, nil
}

// ToUnstructured converts a controller-runtime object to an unstructured object.
func ToUnstructured(obj runtime.Object) (*unstructured.Unstructured, error) {
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("convert to unstructured: %w", err)
	}
	return &unstructured.Unstructured{Object: raw}, nil
}

// GroupVersionKindFor resolves the object's GVK from the client's scheme.
func GroupVersionKindFor(c Interface, obj runtime.Object) (schema.GroupVersionKind, error) {
	gvks, _, err := c.Scheme().ObjectKinds(obj)
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	if len(gvks) == 0 {
		return schema.GroupVersionKind{}, fmt.Errorf(
			"ctrlclient: no GVK registered for %T",
			obj,
		)
	}
	return gvks[0], nil
}

// IsObjectNamespaced reports whether the object's resource is namespace-scoped.
func IsObjectNamespaced(c Interface, obj runtime.Object) (bool, error) {
	if c == nil {
		return false, nil
	}
	mapping, err := GVRFor(c, obj)
	if err != nil {
		return false, err
	}
	return mapping.Scope.Name() == apimeta.RESTScopeNameNamespace, nil
}

// RunApply executes a server-side apply against the target resource,
// optionally addressing a subresource such as "status".
func RunApply(
	ctx context.Context,
	c Interface,
	cfg runtime.ApplyConfiguration,
	opts metav1.ApplyOptions,
	subresource ...string,
) error {
	if cfg == nil {
		return fmt.Errorf("apply configuration must not be nil")
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal apply configuration: %w", err)
	}

	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(data, &u.Object); err != nil {
		return fmt.Errorf("decode apply configuration: %w", err)
	}

	gvk := u.GroupVersionKind()
	if gvk.Empty() {
		return fmt.Errorf("apply configuration %T has no apiVersion/kind", cfg)
	}

	name := u.GetName()
	ns := u.GetNamespace()

	if name == "" {
		return fmt.Errorf("apply configuration %T has no metadata.name", cfg)
	}

	resource, err := ResourceForGVK(c, gvk, ns)
	if err != nil {
		return err
	}

	result, err := resource.Apply(
		ctx,
		name,
		u,
		opts,
		subresource...,
	)
	if err != nil {
		return err
	}

	resultData, err := json.Marshal(result.Object)
	if err != nil {
		return fmt.Errorf("marshal applied object: %w", err)
	}

	return json.Unmarshal(resultData, cfg)
}
