//go:build integration

// tests/integration/dependency/cycle_detection_test.go
package dependency_test

import (
	"testing"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/validate"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

func TestCycleDetection_TwoNodeCycle(t *testing.T) {
	k := katalog.NewKatalogForTest(map[string]orktypes.CRDEntry{
		"a": {Name: "a", DependsOn: orktypes.DependsOnMap{"b": {}}},
		"b": {Name: "b", DependsOn: orktypes.DependsOnMap{"a": {}}},
	})
	if err := validate.DetectCycles(k); err == nil {
		t.Error("expected cycle error for a ↔ b")
	}
}

func TestCycleDetection_ThreeNodeCycle(t *testing.T) {
	k := katalog.NewKatalogForTest(map[string]orktypes.CRDEntry{
		"a": {Name: "a", DependsOn: orktypes.DependsOnMap{"c": {}}},
		"b": {Name: "b", DependsOn: orktypes.DependsOnMap{"a": {}}},
		"c": {Name: "c", DependsOn: orktypes.DependsOnMap{"b": {}}},
	})
	if err := validate.DetectCycles(k); err == nil {
		t.Error("expected cycle error for a → c → b → a")
	}
}

func TestCycleDetection_SelfLoop(t *testing.T) {
	k := katalog.NewKatalogForTest(map[string]orktypes.CRDEntry{
		"a": {Name: "a", DependsOn: orktypes.DependsOnMap{"a": {}}},
	})
	if err := validate.DetectCycles(k); err == nil {
		t.Error("expected cycle error for self-loop")
	}
}

func TestCycleDetection_AcyclicGraph_NoError(t *testing.T) {
	k := katalog.NewKatalogForTest(map[string]orktypes.CRDEntry{
		"db":    {Name: "db"},
		"cache": {Name: "cache"},
		"app":   {Name: "app", DependsOn: orktypes.DependsOnMap{"db": {}, "cache": {}}},
	})
	if err := validate.DetectCycles(k); err != nil {
		t.Errorf("acyclic graph must not produce cycle error: %v", err)
	}
}

func TestCycleDetection_EmptyGraph_NoError(t *testing.T) {
	k := katalog.NewKatalogForTest(map[string]orktypes.CRDEntry{})
	if err := validate.DetectCycles(k); err != nil {
		t.Errorf("empty graph must not produce cycle error: %v", err)
	}
}
