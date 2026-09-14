package domain

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

// Object represents a Kubernetes API object with metadata and runtime type information.
type Object interface {
	metav1.Object
	runtime.Object
}

// ObjectList represents a Kubernetes API list object containing metadata
// and runtime type information.
type ObjectList interface {
	metav1.ListInterface
	runtime.Object
}

// ObjectKey identifies a Kubernetes object by namespace and name.
type ObjectKey = types.NamespacedName

// ToDomainObject unwraps a cache tombstone and asserts to domain.Object.
func ToDomainObject(obj interface{}) (Object, bool) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	d, ok := obj.(Object)
	return d, ok
}

// UnwrapCacheTombstone unwraps a cache tombstone and returns the object.
func UnwrapCacheTombstone(obj interface{}) interface{} {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	return obj
}

// ToUnstructured unwraps a cache tombstone and asserts to *unstructured.Unstructured.
func ToUnstructured(obj interface{}) (*unstructured.Unstructured, bool) {
	if ts, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = ts.Obj
	}
	u, ok := obj.(*unstructured.Unstructured)
	return u, ok
}
