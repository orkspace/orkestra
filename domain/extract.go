package domain

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

// ExtractNamespace extracts the namespace from an object passed to handleEvent.
// Handles both regular objects and DeletedFinalStateUnknown (tombstone) wrappers.
// Returns "" for cluster-scoped resources — Allows("") returns true so they pass.
func ExtractNamespace(obj interface{}) string {
	// Handle tombstone (deleted objects)
	obj = UnwrapCacheTombstone(obj)

	if rObj, ok := obj.(runtime.Object); ok {
		if accessor, err := meta.Accessor(rObj); err == nil {
			return accessor.GetNamespace()
		}
	}

	return ""
}

// ExtractGeneration returns metadata.generation for obj, or 0 if unavailable.
func ExtractGeneration(obj interface{}) int64 {
	obj = UnwrapCacheTombstone(obj)
	if rObj, ok := obj.(runtime.Object); ok {
		if accessor, err := meta.Accessor(rObj); err == nil {
			return accessor.GetGeneration()
		}
	}
	return 0
}

// ExtractResourceVersion returns metadata.resourceVersion for obj, or "" if unavailable.
func ExtractResourceVersion(obj interface{}) string {
	obj = UnwrapCacheTombstone(obj)
	if rObj, ok := obj.(runtime.Object); ok {
		if accessor, err := meta.Accessor(rObj); err == nil {
			return accessor.GetResourceVersion()
		}
	}
	return ""
}
