// pkg/utils/gvk.go
package utils

import (
	"fmt"

	"github.com/orkspace/orkestra/pkg/logger"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersionKind is a consistent string key for GVK-keyed maps and logs.
type GroupVersionKind string

// SetGroupVersionKind formats a GVK as the canonical string used internally.
// Mirrors runtime.Object.GetObjectKind().GroupVersionKind().String() output.
func SetGroupVersionKind(group, version, kind string) GroupVersionKind {
	return GroupVersionKind(fmt.Sprintf("%s/%s, Kind=%s", group, version, kind))
}

func SetGroupVersionKindObj(gvk schema.GroupVersionKind) string {
	return SetGroupVersionKind(gvk.Group, gvk.Version, gvk.Kind).String()
}

func (g GroupVersionKind) String() string {
	return string(g)
}

func GvkFromObject(obj interface{}, scheme *runtime.Scheme) (*schema.GroupVersionKind, error) {
	runtimeObj, ok := obj.(runtime.Object)
	if !ok {
		logger.Error().Str("type", fmt.Sprintf("%T", obj)).Msg("object is not a runtime.Object — event dropped")
		return nil, fmt.Errorf("object is not a runtime.Object: %T", obj)
	}

	// Resolve GVK from scheme — cached objects have TypeMeta stripped,
	// GetObjectKind().GroupVersionKind() returns empty.
	gvks, _, err := scheme.ObjectKinds(runtimeObj)
	if err != nil || len(gvks) == 0 {
		logger.Error().Err(err).Str("type", fmt.Sprintf("%T", obj)).Msg("failed to resolve GVK — event dropped")
		return nil, fmt.Errorf("failed to resolve GVK for %T — event dropped", obj)
	}

	return &gvks[0], nil
}
