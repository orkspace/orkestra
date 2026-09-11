package domain

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ManagedResource describes a Kubernetes resource type that a typed extension
// (either a hook or a constructor) will manage.
//
// Orkestra uses this information for two purposes:
//
//  1. RBAC generation — each declared resource results in permissions to
//     get/list/watch/create/update/patch/delete that resource type.
//
//  2. Implicit watch informer — Orkestra automatically starts a watch informer
//     for each declared resource, identical to declaring a watch: entry with
//     all events and owner-reference key resolution. This means:
//
//     - r.client.Get / r.client.List for that type are served from cache
//     - when an owned resource changes, Orkestra enqueues the primary CR
//
//     If you need finer control (custom on:, enqueueGate:, keyFrom:, or index:),
//     declare a watch: entry for that type — it takes priority over the
//     implicit informer from resources:.
//
// For built-in Kubernetes resources, Kind alone is sufficient because Orkestra
// resolves the full GroupVersionResource from its internal registry.
//
// For custom resources or non-core API groups, APIVersion and/or explicit
// group/version/plural may be provided.
type ManagedResource struct {
	Kind       string `json:"kind,omitempty" yaml:"kind,omitempty"`
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Group      string `json:"group,omitempty" yaml:"group,omitempty"`
	Version    string `json:"version,omitempty" yaml:"version,omitempty"`
	Plural     string `json:"plural,omitempty" yaml:"plural,omitempty"`
	Name       string `yaml:"name,omitempty" json:"name,omitempty"`
	Namespace  string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

// Matches reports whether the resource reference at field matches this
// ManagedResource.
//
// The field identifies a nested Kubernetes resource reference, such as
// "regarding" or "related". Each non-empty field in the ManagedResource is
// matched against the corresponding value in the reference; empty fields
// act as wildcards.
//
// A nil receiver matches any reference.
func (r *ManagedResource) Matches(obj *unstructured.Unstructured, field string) bool {
	if r == nil {
		return true
	}

	path := strings.Split(field, ".")
	apiVersion, _, _ := unstructured.NestedString(obj.Object, append(path, "apiVersion")...)
	kind, _, _ := unstructured.NestedString(obj.Object, append(path, "kind")...)
	name, _, _ := unstructured.NestedString(obj.Object, append(path, "name")...)
	namespace, _, _ := unstructured.NestedString(obj.Object, append(path, "namespace")...)

	if r.APIVersion != "" && r.APIVersion != apiVersion {
		return false
	}
	if r.Kind != "" && r.Kind != kind {
		return false
	}
	if r.Name != "" && r.Name != name {
		return false
	}
	if r.Namespace != "" && r.Namespace != namespace {
		return false
	}

	return true
}
