package simulate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/pipeline"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/merger"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const uniqueTestCRD = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: websites.testground.orkestra.io
spec:
  group: testground.orkestra.io
  names:
    kind: Website
    plural: websites
    singular: website
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              required: [domain]
              properties:
                domain:
                  type: string
`

const uniqueTestKatalog = `
apiVersion: orkestra.orkspace.io/v1
kind: Katalog
metadata:
  name: unique-operator-harness-test
  author: claude
  version: 0.1.0
  description: "harness test for operator: unique via ExistingInstances"

spec:
  crds:
    website:
      apiTypes:
        group: testground.orkestra.io
        version: v1alpha1
        kind: Website
        plural: websites
      crdFile: ./crd.yaml

      validation:
        rules:
          - field: spec.domain
            operator: unique
            message: "spec.domain must be unique across all Website instances"
            action: deny
`

// writeUniqueTestKatalog materializes the katalog+CRD fixture used by both
// TestRun_OperatorUnique_* tests into a temp dir, matching what
// merger.New/katalog.BuildExpanded expect on disk.
func writeUniqueTestKatalog(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "crd.yaml"), []byte(uniqueTestCRD), 0o644); err != nil {
		t.Fatalf("writing crd.yaml: %v", err)
	}
	katalogPath := filepath.Join(dir, "katalog.yaml")
	if err := os.WriteFile(katalogPath, []byte(uniqueTestKatalog), 0o644); err != nil {
		t.Fatalf("writing katalog.yaml: %v", err)
	}
	return katalogPath
}

func loadUniqueTestKatalog(t *testing.T) *katalog.Katalog {
	t.Helper()
	katalogPath := writeUniqueTestKatalog(t)
	m := merger.New(katalogPath)
	if err := m.Merge(); err != nil {
		t.Fatalf("merging katalog: %v", err)
	}
	kat, err := pipeline.BuildExpanded(konfig.NewDefaultKonfig(), m)
	if err != nil {
		t.Fatalf("building katalog: %v", err)
	}
	return kat
}

func websiteCR(name, domain string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "testground.orkestra.io/v1alpha1",
		"kind":       "Website",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"domain": domain,
		},
	}}
}

// TestRun_OperatorUnique_NoDuplicate proves operator: unique passes end to
// end through the real reconcile pipeline (resolver → injected
// UniquenessChecker → validation.rules) when no other instance shares the
// field value.
func TestRun_OperatorUnique_NoDuplicate(t *testing.T) {
	kat := loadUniqueTestKatalog(t)
	cr := websiteCR("site-a", "a.example.com")

	result, err := Run(context.Background(), kat, "website", cr, 1, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(result.Cycles))
	}
	if result.Cycles[0].Error != nil {
		t.Errorf("expected no reconcile error, got: %v", result.Cycles[0].Error)
	}
}

// TestRun_OperatorUnique_Duplicate proves the actual point of this feature:
// a pre-existing same-kind instance (RunOptions.ExistingInstances, from a
// second document of the CRD's own kind in a multi-doc CR file) is seeded
// into the fake dynamic client and makes operator: unique correctly deny —
// not a checker that trivially always passes because nothing was seeded.
func TestRun_OperatorUnique_Duplicate(t *testing.T) {
	kat := loadUniqueTestKatalog(t)
	cr := websiteCR("site-b", "shared.example.com")
	existing := websiteCR("site-a", "shared.example.com")

	result, err := Run(context.Background(), kat, "website", cr, 1, RunOptions{
		ExistingInstances: []*unstructured.Unstructured{existing},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(result.Cycles))
	}
	if result.Cycles[0].Error == nil {
		t.Fatal("expected a validation-denied reconcile error, got none")
	}
}
