package utils

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// MatchesFieldRequirements checks that u satisfies all requirements using
// simple string equality on unstructured field values. Used as a post-filter
// after an index lookup to handle residual requirements the index did not cover.
func MatchesFieldRequirements(u *unstructured.Unstructured, reqs fields.Requirements) bool {
	for _, req := range reqs {
		parts := SplitField(req.Field)
		if len(parts) == 0 {
			continue
		}
		val, _, _ := unstructured.NestedString(u.Object, parts...)
		if val != req.Value {
			return false
		}
	}
	return true
}

// Check if a CRD exists
func CheckCRDExists(gvk *schema.GroupVersionKind, c *rest.Config) bool {
	disco, err := discovery.NewDiscoveryClientForConfig(c)
	if err != nil {
		return false
	}
	resources, err := disco.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		return false
	}
	for _, r := range resources.APIResources {
		if r.Kind == gvk.Kind {
			return true
		}
	}
	return false
}
