package domain

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
	"testing"
)

// ── extractNamespace ──────────────────────────────────────────────────────────

func TestExtractNamespace_UnstructuredWithNamespace(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetNamespace("my-ns")
	if got := ExtractNamespace(obj); got != "my-ns" {
		t.Errorf("expected my-ns, got %q", got)
	}
}

func TestExtractNamespace_UnstructuredClusterScoped(t *testing.T) {
	obj := &unstructured.Unstructured{}
	// no namespace set
	if got := ExtractNamespace(obj); got != "" {
		t.Errorf("expected empty for cluster-scoped, got %q", got)
	}
}

func TestExtractNamespace_Tombstone_ExtractsNamespace(t *testing.T) {
	inner := &unstructured.Unstructured{}
	inner.SetNamespace("tombstone-ns")
	tombstone := cache.DeletedFinalStateUnknown{Obj: inner}
	if got := ExtractNamespace(tombstone); got != "tombstone-ns" {
		t.Errorf("expected tombstone-ns from tombstone, got %q", got)
	}
}

// ── extractResourceVersion ──────────────────────────────────────────────────────────
func TestExtractResourceVersion_UnstructuredWithResourceVersion(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetResourceVersion("123")
	if got := ExtractResourceVersion(obj); got != "123" {
		t.Errorf("expected 123, got %q", got)
	}
}

func TestExtractResourceVersion_UnstructuredNoResourceVersion(t *testing.T) {
	obj := &unstructured.Unstructured{}
	if got := ExtractResourceVersion(obj); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractResourceVersion_Tombstone_ExtractsResourceVersion(t *testing.T) {
	inner := &unstructured.Unstructured{}
	inner.SetResourceVersion("456")
	tombstone := cache.DeletedFinalStateUnknown{Obj: inner}
	if got := ExtractResourceVersion(tombstone); got != "456" {
		t.Errorf("expected 456 from tombstone, got %q", got)
	}
}

func TestExtractResourceVersion_NonObject_ReturnsEmpty(t *testing.T) {
	if got := ExtractResourceVersion("not-an-object"); got != "" {
		t.Errorf("expected empty for non-object, got %q", got)
	}
}

// ── extractGeneration ─────────────────────────────────────────────────────────

func TestExtractGeneration_UnstructuredWithGeneration(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetGeneration(7)
	if got := ExtractGeneration(obj); got != 7 {
		t.Errorf("expected 7, got %d", got)
	}
}

func TestExtractGeneration_UnstructuredNoGeneration(t *testing.T) {
	obj := &unstructured.Unstructured{}
	if got := ExtractGeneration(obj); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestExtractGeneration_Tombstone_ExtractsGeneration(t *testing.T) {
	inner := &unstructured.Unstructured{}
	inner.SetGeneration(3)
	tombstone := cache.DeletedFinalStateUnknown{Obj: inner}
	if got := ExtractGeneration(tombstone); got != 3 {
		t.Errorf("expected 3 from tombstone, got %d", got)
	}
}

func TestExtractGeneration_NonObject_ReturnsZero(t *testing.T) {
	if got := ExtractGeneration("not-an-object"); got != 0 {
		t.Errorf("expected 0 for non-object, got %d", got)
	}
}
