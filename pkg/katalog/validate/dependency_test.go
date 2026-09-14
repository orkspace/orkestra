package validate

import (
	"testing"

	"github.com/orkspace/orkestra/pkg/katalog"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

func crdMapForCycle(crds ...orktypes.CRDEntry) map[string]orktypes.CRDEntry {
	m := make(map[string]orktypes.CRDEntry, len(crds))
	for _, c := range crds {
		m[c.Name] = c
	}
	return m
}

func depEntry(name string, deps ...string) orktypes.CRDEntry {
	e := orktypes.CRDEntry{Name: name}
	if len(deps) > 0 {
		e.DependsOn = make(orktypes.DependsOnMap, len(deps))
		for _, d := range deps {
			e.DependsOn[d] = orktypes.DependsOnCondition{Condition: "started"}
		}
	}
	return e
}

func TestDetectCycles_TwoNodeCycle(t *testing.T) {
	k := katalog.NewKatalogForTest(crdMapForCycle(depEntry("a", "b"), depEntry("b", "a")))
	if err := DetectCycles(k); err == nil {
		t.Error("expected cycle detection error for a ↔ b cycle")
	}
}

func TestDetectCycles_ThreeNodeCycle(t *testing.T) {
	k := katalog.NewKatalogForTest(crdMapForCycle(depEntry("a", "c"), depEntry("b", "a"), depEntry("c", "b")))
	if err := DetectCycles(k); err == nil {
		t.Error("expected cycle detection error for a → c → b → a cycle")
	}
}

func TestDetectCycles_NoCycle(t *testing.T) {
	k := katalog.NewKatalogForTest(crdMapForCycle(depEntry("a"), depEntry("b", "a"), depEntry("c", "b")))
	if err := DetectCycles(k); err != nil {
		t.Errorf("expected no error for acyclic graph, got %v", err)
	}
}
