package katalog

import (
	"fmt"
	"reflect"

	"github.com/orkspace/orkestra/pkg/logger"
	ork_runtime "github.com/orkspace/orkestra/pkg/typeregistry"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// -----------------------------------------------------------------------------
// Entry point
// -----------------------------------------------------------------------------

// NewSchemeRegistry returns a new scheme
func NewSchemeRegistry(k *Katalog) (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()

	// 1. Register built-in Kubernetes types
	metav1.AddToGroupVersion(scheme, metav1.SchemeGroupVersion)

	// 2. Register core Kubernetes types
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}

	// 3. Register CRDs
	var err error
	// Register dynamic scheme
	if scheme, err = k.registerDynamicScheme(scheme); err != nil {
		return nil, err
	}
	// Register typed scheme
	if scheme, err = ork_runtime.RegisterTypedScheme(scheme); err != nil {
		return nil, err
	}

	// 4. Register external GVKs from custom: blocks so the fake dynamic client's
	// object tracker can create/get them during simulate (in-memory cluster).
	// Without this, the tracker has no schema entry and Create calls fail silently.
	if scheme, err = k.registerCustomResourceScheme(scheme); err != nil {
		return nil, err
	}

	return scheme, nil
}

// Helpers
// UpdateResourceMapAndReturn maps each enabled CRD's dynamic object type into
// the package-level resourceTypeMap for O(1) GVK reverse-lookups, then returns k.
func (k *Katalog) UpdateResourceMapAndReturn() (*Katalog, error) {
	// Map the type of the object
	for _, c := range k.enabledCRDs {
		if k.EnabledCRDsEmpty() {
			return nil, fmt.Errorf("no enabled CRDs found")
		}

		// Map the type of the object
		logger.Debug().Msgf("updating resource map for %s", c.GroupVersionKind.String())
		resourceTypeMap[reflect.TypeOf(c.DynamicModeObject)] = c.GroupVersionKind.String()
	}

	return k, nil
}

// registerCustomResourceScheme registers external GVKs declared in custom: blocks
// (onCreate and onReconcile) into the scheme so the fake dynamic client's object
// tracker can store and retrieve them during simulate. Deduplicates by GVK string.
func (k *Katalog) registerCustomResourceScheme(scheme *runtime.Scheme) (*runtime.Scheme, error) {
	seen := make(map[string]bool)
	for _, crd := range k.enabledCRDs {
		if crd.HasOnCreate() && crd.OperatorBox.OnCreate.CustomResource != nil {
			for i := range crd.OperatorBox.OnCreate.CustomResource {
				cr := &crd.OperatorBox.OnCreate.CustomResource[i]
				key := cr.APIVersion + "/" + cr.Kind
				if seen[key] || cr.APIVersion == "" || cr.Kind == "" {
					continue
				}
				seen[key] = true
				gvk, err := cr.BuildGVK()
				if err != nil {
					continue
				}
				scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
				scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List"}, &unstructured.UnstructuredList{})
			}
		}

		if crd.HasOnReconcile() && crd.OperatorBox.OnReconcile.CustomResource != nil {
			for i := range crd.OperatorBox.OnReconcile.CustomResource {
				cr := &crd.OperatorBox.OnReconcile.CustomResource[i]
				key := cr.APIVersion + "/" + cr.Kind
				if seen[key] || cr.APIVersion == "" || cr.Kind == "" {
					continue
				}
				seen[key] = true
				gvk, err := cr.BuildGVK()
				if err != nil {
					continue
				}
				scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
				scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List"}, &unstructured.UnstructuredList{})
			}
		}

	}
	return scheme, nil
}

// Register dynamic CRDs — tells the watch stream to decode
// these GVKs as *unstructured.Unstructured instead of failing
func (k *Katalog) registerDynamicScheme(scheme *runtime.Scheme) (*runtime.Scheme, error) {
	for _, crd := range k.enabledCRDs {
		if crd.IsDynamic() && crd.APITypes.Location == "" && !crd.IsBuiltInType() {
			// Register Object
			scheme.AddKnownTypeWithName(
				schema.GroupVersionKind{
					Group:   crd.APITypes.Group,
					Version: crd.APITypes.Version,
					Kind:    crd.APITypes.Kind,
				},
				&unstructured.Unstructured{},
			)

			// Register List
			scheme.AddKnownTypeWithName(
				schema.GroupVersionKind{
					Group:   crd.APITypes.Group,
					Version: crd.APITypes.Version,
					Kind:    crd.APITypes.Kind + "List",
				},
				&unstructured.UnstructuredList{},
			)
		}
	}

	return scheme, nil
}
