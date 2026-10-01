// pkg/informer/namespace_filter_test.go
package informer

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ── NamespaceFilter.Allows ────────────────────────────────────────────────────

func TestNamespaceFilter_Allows_NoRestrictions_AllowsAll(t *testing.T) {
	f := &NamespaceFilter{}
	if !f.Allows("default") {
		t.Error("no restrictions must allow any namespace")
	}
}

func TestNamespaceFilter_Allows_AllowedList_MatchAllows(t *testing.T) {
	f := &NamespaceFilter{AllowedNamespaces: []string{"prod", "staging"}}
	if !f.Allows("prod") {
		t.Error("prod must be allowed")
	}
	if !f.Allows("staging") {
		t.Error("staging must be allowed")
	}
}

func TestNamespaceFilter_Allows_AllowedList_NonMatchBlocks(t *testing.T) {
	f := &NamespaceFilter{AllowedNamespaces: []string{"prod"}}
	if f.Allows("default") {
		t.Error("default must not be allowed when not in allowlist")
	}
}

func TestNamespaceFilter_Allows_RestrictedList_MatchBlocks(t *testing.T) {
	f := &NamespaceFilter{RestrictedNamespaces: []string{"kube-system"}}
	if f.Allows("kube-system") {
		t.Error("kube-system must be blocked when restricted")
	}
}

func TestNamespaceFilter_Allows_RestrictedList_NonMatchAllows(t *testing.T) {
	f := &NamespaceFilter{RestrictedNamespaces: []string{"kube-system"}}
	if !f.Allows("default") {
		t.Error("default must be allowed when not in restricted list")
	}
}

func TestNamespaceFilter_Allows_AllowedTakesPrecedenceOverRestricted(t *testing.T) {
	f := &NamespaceFilter{
		AllowedNamespaces:    []string{"prod"},
		RestrictedNamespaces: []string{"prod"}, // contradictory
	}
	// Allowed takes precedence — prod is in the allowlist, so it passes
	if !f.Allows("prod") {
		t.Error("allowed list takes precedence over restricted list")
	}
	// Non-listed namespace is blocked by allowed list
	if f.Allows("staging") {
		t.Error("staging not in allowlist must be blocked")
	}
}

func TestNamespaceFilter_Allows_EmptyNamespace_ClusterScoped(t *testing.T) {
	f := &NamespaceFilter{} // no restrictions
	if !f.Allows("") {
		t.Error("empty namespace (cluster-scoped) must pass empty filter")
	}
}

// ── NamespaceFilter.IsActive ──────────────────────────────────────────────────

func TestNamespaceFilter_IsActive_NilFilter_False(t *testing.T) {
	var f *NamespaceFilter
	if f.IsActive() {
		t.Error("nil filter must not be active")
	}
}

func TestNamespaceFilter_IsActive_EmptyFilter_False(t *testing.T) {
	f := &NamespaceFilter{}
	if f.IsActive() {
		t.Error("empty filter must not be active")
	}
}

func TestNamespaceFilter_IsActive_WithAllowed_True(t *testing.T) {
	f := &NamespaceFilter{AllowedNamespaces: []string{"prod"}}
	if !f.IsActive() {
		t.Error("filter with allowed namespaces must be active")
	}
}

func TestNamespaceFilter_IsActive_WithRestricted_True(t *testing.T) {
	f := &NamespaceFilter{RestrictedNamespaces: []string{"kube-system"}}
	if !f.IsActive() {
		t.Error("filter with restricted namespaces must be active")
	}
}

// ── NamespaceFilter.IsSingleNamespace ────────────────────────────────────────

func TestNamespaceFilter_IsSingleNamespace_OneAllowed_True(t *testing.T) {
	f := &NamespaceFilter{AllowedNamespaces: []string{"prod"}}
	if !f.IsSingleNamespace() {
		t.Error("single allowed namespace must return true")
	}
}

func TestNamespaceFilter_IsSingleNamespace_MultipleAllowed_False(t *testing.T) {
	f := &NamespaceFilter{AllowedNamespaces: []string{"prod", "staging"}}
	if f.IsSingleNamespace() {
		t.Error("multiple allowed namespaces must return false")
	}
}

func TestNamespaceFilter_IsSingleNamespace_OneAllowedWithRestricted_False(t *testing.T) {
	f := &NamespaceFilter{AllowedNamespaces: []string{"prod"}, RestrictedNamespaces: []string{"kube-system"}}
	if f.IsSingleNamespace() {
		t.Error("single allowed + restricted must not be single-namespace")
	}
}

func TestNamespaceFilter_IsSingleNamespace_Nil_False(t *testing.T) {
	var f *NamespaceFilter
	if f.IsSingleNamespace() {
		t.Error("nil filter must not be single-namespace")
	}
}

// ── NamespaceFilter.SingleNamespace ──────────────────────────────────────────

func TestNamespaceFilter_SingleNamespace_OneAllowed_ReturnsIt(t *testing.T) {
	f := &NamespaceFilter{AllowedNamespaces: []string{"prod"}}
	if got := f.SingleNamespace(); got != "prod" {
		t.Errorf("expected prod, got %q", got)
	}
}

func TestNamespaceFilter_SingleNamespace_Multiple_ReturnsEmpty(t *testing.T) {
	f := &NamespaceFilter{AllowedNamespaces: []string{"prod", "staging"}}
	if got := f.SingleNamespace(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ── NamespaceFilterSummary ────────────────────────────────────────────────────

func TestNamespaceFilterSummary_NilFilter_AllNamespaces(t *testing.T) {
	if got := NamespaceFilterSummary(nil); got != "all namespaces" {
		t.Errorf("expected 'all namespaces', got %q", got)
	}
}

func TestNamespaceFilterSummary_EmptyFilter_AllNamespaces(t *testing.T) {
	if got := NamespaceFilterSummary(&NamespaceFilter{}); got != "all namespaces" {
		t.Errorf("expected 'all namespaces', got %q", got)
	}
}

func TestNamespaceFilterSummary_AllowedList(t *testing.T) {
	f := &NamespaceFilter{AllowedNamespaces: []string{"prod", "staging"}}
	got := NamespaceFilterSummary(f)
	if got != "allowed: [prod, staging]" {
		t.Errorf("unexpected summary: %q", got)
	}
}

func TestNamespaceFilterSummary_RestrictedList(t *testing.T) {
	f := &NamespaceFilter{RestrictedNamespaces: []string{"kube-system"}}
	got := NamespaceFilterSummary(f)
	if got != "restricted: [kube-system]" {
		t.Errorf("unexpected summary: %q", got)
	}
}

// ── ensureGVKOnRuntimeObject ──────────────────────────────────────────────────

func TestEnsureGVKOnRuntimeObject_SetsGVKWhenMissing(t *testing.T) {
	obj := &unstructured.Unstructured{}
	gvk := &schema.GroupVersionKind{Group: "example.io", Version: "v1", Kind: "Foo"}
	ensureGVKOnRuntimeObject(obj, gvk)
	got := obj.GetObjectKind().GroupVersionKind()
	if got.Kind != "Foo" || got.Group != "example.io" {
		t.Errorf("expected GVK to be set, got %v", got)
	}
}

func TestEnsureGVKOnRuntimeObject_DoesNotOverwriteExistingGVK(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "original.io", Version: "v1", Kind: "Bar"})
	gvk := &schema.GroupVersionKind{Group: "new.io", Version: "v2", Kind: "Baz"}
	ensureGVKOnRuntimeObject(obj, gvk)
	got := obj.GetObjectKind().GroupVersionKind()
	if got.Kind != "Bar" {
		t.Errorf("expected original GVK to be preserved, got %v", got)
	}
}

func TestEnsureGVKOnRuntimeObject_NilObjNoError(t *testing.T) {
	gvk := &schema.GroupVersionKind{Kind: "Foo"}
	// Must not panic
	ensureGVKOnRuntimeObject(nil, gvk)
}

func TestEnsureGVKOnRuntimeObject_NilGVKNoError(t *testing.T) {
	obj := &unstructured.Unstructured{}
	// Must not panic
	ensureGVKOnRuntimeObject(obj, nil)
}
