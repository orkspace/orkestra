package shared

import (
	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ResolveNamespace — priority: spec.Namespace → owner namespace → "default"
func ResolveNamespace(owner domain.Object, namespace string) string {
	if namespace != "" {
		return namespace
	}
	if owner.GetNamespace() != "" {
		return owner.GetNamespace()
	}
	return "default"
}

// ToPullSecrets converts a slice of string to a []corev1.LocalObjectReference
// Acceptable as Pull secrets
func ToPullSecrets(names []string) []corev1.LocalObjectReference {
	out := make([]corev1.LocalObjectReference, len(names))
	for i, n := range names {
		out[i] = corev1.LocalObjectReference{Name: n}
	}
	return out
}

// ResolveOwnerReferences returns the common ownership reference for orkestra-managed resources.
func ResolveOwnerReferences(owner domain.Object) []metav1.OwnerReference {
	apiVersion := ""
	kind := ""
	if u, ok := owner.(*unstructured.Unstructured); ok {
		apiVersion = u.GetAPIVersion()
		kind = u.GetKind()
	} else {
		gvk := owner.GetObjectKind().GroupVersionKind()
		apiVersion = gvk.GroupVersion().String()
		kind = gvk.Kind
	}

	return []metav1.OwnerReference{
		{
			APIVersion:         apiVersion,
			Kind:               kind,
			Name:               owner.GetName(),
			UID:                owner.GetUID(),
			Controller:         utils.BoolPtr(true),
			BlockOwnerDeletion: utils.BoolPtr(true),
		},
	}
}

// ResolveForceConflict returns the effective force-conflict setting for a resource.
// The resource-level setting takes precedence over the CRD-level setting.
// When neither is configured, ForceConflict defaults to true.
func ResolveForceConflict(kube kubeclient.Interface, resourceForceConflict *bool) *bool {
	if resourceForceConflict != nil {
		return resourceForceConflict
	}

	if kube != nil {
		if forceConflict := kube.ForceConflict(); forceConflict != nil {
			return forceConflict
		}
	}

	defaultForceConflict := true
	return &defaultForceConflict
}
