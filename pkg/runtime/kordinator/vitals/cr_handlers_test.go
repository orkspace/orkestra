package vitals

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
)

// fakeInformer satisfies cache.SharedIndexInformer for tests that only need
// GetIndexer() — matches the same pattern used in pkg/registry/simulate.
type fakeInformer struct {
	indexer cache.Indexer
	cache.SharedIndexInformer
}

func (f *fakeInformer) GetIndexer() cache.Indexer { return f.indexer }

func newTestInformer(t *testing.T, objs ...map[string]interface{}) cache.SharedIndexInformer {
	t.Helper()
	idx := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, o := range objs {
		if err := idx.Add(&unstructured.Unstructured{Object: o}); err != nil {
			t.Fatalf("seeding indexer: %v", err)
		}
	}
	return &fakeInformer{indexer: idx}
}

func website(name, namespace, domain string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "testground.orkestra.io/v1alpha1",
		"kind":       "Website",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]interface{}{
			"domain": domain,
		},
	}
}

func TestBuildCRListHandler_NoFieldParam(t *testing.T) {
	inf := newTestInformer(t, website("site-a", "default", "a.example.com"))
	crd := orktypes.CRDEntry{Name: "website", APITypes: orktypes.APITypes{Kind: "Website"}}
	handler := BuildCRListHandler(crd, inf, &RuntimeHealth{})

	req := httptest.NewRequest(http.MethodGet, "/katalog/website/cr", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	var resp CRListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(resp.Items))
	}
	if resp.Items[0].Value != "" {
		t.Errorf("Value = %q, want empty — no ?field= was requested", resp.Items[0].Value)
	}
}

func TestBuildCRListHandler_FieldParamResolvesSpecValue(t *testing.T) {
	inf := newTestInformer(t,
		website("site-a", "default", "a.example.com"),
		website("site-b", "default", "b.example.com"),
	)
	crd := orktypes.CRDEntry{Name: "website", APITypes: orktypes.APITypes{Kind: "Website"}}
	handler := BuildCRListHandler(crd, inf, &RuntimeHealth{})

	req := httptest.NewRequest(http.MethodGet, "/katalog/website/cr?field=spec.domain", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	var resp CRListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(resp.Items))
	}
	got := map[string]string{}
	for _, item := range resp.Items {
		got[item.Name] = item.Value
	}
	if got["site-a"] != "a.example.com" || got["site-b"] != "b.example.com" {
		t.Errorf("resolved values = %+v, want site-a=a.example.com site-b=b.example.com", got)
	}
}

func TestBuildCRListHandler_FieldParamMissingOnCR(t *testing.T) {
	inf := newTestInformer(t, website("site-a", "default", "a.example.com"))
	crd := orktypes.CRDEntry{Name: "website", APITypes: orktypes.APITypes{Kind: "Website"}}
	handler := BuildCRListHandler(crd, inf, &RuntimeHealth{})

	req := httptest.NewRequest(http.MethodGet, "/katalog/website/cr?field=spec.doesNotExist", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	var resp CRListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Value != "" {
		t.Errorf("got %+v, want a single item with empty Value", resp.Items)
	}
}
