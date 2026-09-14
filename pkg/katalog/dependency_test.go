// pkg/katalog/dependency_test.go
// White-box unit tests for the DependencyGraph topological sort.
package katalog

import (
	"reflect"
	"sort"
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// crd builds a map entry for use in test Katalog construction.
// dependsOn names are mapped with condition "started".
func crdMap(crds ...orktypes.CRDEntry) map[string]orktypes.CRDEntry {
	m := make(map[string]orktypes.CRDEntry, len(crds))
	for _, c := range crds {
		m[c.Name] = c
	}
	return m
}

func dep(name string, deps ...string) orktypes.CRDEntry {
	e := orktypes.CRDEntry{Name: name}
	if len(deps) > 0 {
		e.DependsOn = make(orktypes.DependsOnMap, len(deps))
		for _, d := range deps {
			e.DependsOn[d] = orktypes.DependsOnCondition{Condition: "started"}
		}
	}
	return e
}

// ── No dependencies ───────────────────────────────────────────────────────────

func TestDependencyGraph_StartupOrder_NoDeps_SingleCRD(t *testing.T) {
	k := &Katalog{enabledCRDs: crdMap(dep("website"))}
	order := NewDependencyGraph(k).StartupOrder()

	if len(order) != 1 || order[0] != "website" {
		t.Errorf("expected [website], got %v", order)
	}
}

func TestDependencyGraph_StartupOrder_NoDeps_MultiCRD_IsSorted(t *testing.T) {
	// No dependencies → alphabetical order (deterministic).
	k := &Katalog{enabledCRDs: crdMap(dep("zebra"), dep("apple"), dep("mango"))}
	order := NewDependencyGraph(k).StartupOrder()

	expected := []string{"apple", "mango", "zebra"}
	if !reflect.DeepEqual(order, expected) {
		t.Errorf("expected %v, got %v", expected, order)
	}
}

// ── Linear chain ──────────────────────────────────────────────────────────────

func TestDependencyGraph_StartupOrder_LinearChain(t *testing.T) {
	// database → cache → application (database starts first)
	k := &Katalog{enabledCRDs: crdMap(
		dep("application", "cache"),
		dep("cache", "database"),
		dep("database"),
	)}
	order := NewDependencyGraph(k).StartupOrder()

	assertBefore(t, order, "database", "cache")
	assertBefore(t, order, "cache", "application")
}

// ── Fan-out ───────────────────────────────────────────────────────────────────

func TestDependencyGraph_StartupOrder_FanOut(t *testing.T) {
	k := &Katalog{enabledCRDs: crdMap(
		dep("consumer-a", "provider"),
		dep("consumer-b", "provider"),
		dep("provider"),
	)}
	order := NewDependencyGraph(k).StartupOrder()

	assertBefore(t, order, "provider", "consumer-a")
	assertBefore(t, order, "provider", "consumer-b")
}

// ── Fan-in ────────────────────────────────────────────────────────────────────

func TestDependencyGraph_StartupOrder_FanIn(t *testing.T) {
	k := &Katalog{enabledCRDs: crdMap(
		dep("app", "db", "cache"),
		dep("db"),
		dep("cache"),
	)}
	order := NewDependencyGraph(k).StartupOrder()

	assertBefore(t, order, "db", "app")
	assertBefore(t, order, "cache", "app")
}

// ── ShutdownOrder is reverse of StartupOrder ──────────────────────────────────

func TestDependencyGraph_ShutdownOrder_IsReversedStartup(t *testing.T) {
	k := &Katalog{enabledCRDs: crdMap(dep("app", "db"), dep("db"))}
	g := NewDependencyGraph(k)

	startup := g.StartupOrder()
	shutdown := g.ShutdownOrder()

	if len(startup) != len(shutdown) {
		t.Fatalf("startup and shutdown orders have different lengths")
	}
	for i, v := range startup {
		mirror := shutdown[len(shutdown)-1-i]
		if v != mirror {
			t.Errorf("shutdown[%d]=%q is not the reverse of startup[%d]=%q", len(shutdown)-1-i, mirror, i, v)
		}
	}
}

// ── Accessors ─────────────────────────────────────────────────────────────────

func TestDependencyGraph_GetNode(t *testing.T) {
	k := &Katalog{enabledCRDs: crdMap(dep("website"))}
	g := NewDependencyGraph(k)

	n := g.GetNode("website")
	if n == nil {
		t.Fatal("expected node for 'website'")
	}
	if n.Name != "website" {
		t.Errorf("node name: expected website, got %q", n.Name)
	}
}

func TestDependencyGraph_InDegree_OutDegree(t *testing.T) {
	k := &Katalog{enabledCRDs: crdMap(dep("app", "db"), dep("db"))}
	g := NewDependencyGraph(k)

	if g.GetInDegree("db") != 0 {
		t.Errorf("db inDegree: expected 0, got %d", g.GetInDegree("db"))
	}
	if g.GetOutDegree("db") != 1 {
		t.Errorf("db outDegree: expected 1, got %d", g.GetOutDegree("db"))
	}
	if g.GetInDegree("app") != 1 {
		t.Errorf("app inDegree: expected 1, got %d", g.GetInDegree("app"))
	}
	if g.GetOutDegree("app") != 0 {
		t.Errorf("app outDegree: expected 0, got %d", g.GetOutDegree("app"))
	}
}

func TestDependencyGraph_GetDependents(t *testing.T) {
	k := &Katalog{enabledCRDs: crdMap(
		dep("app-a", "db"),
		dep("app-b", "db"),
		dep("db"),
	)}
	g := NewDependencyGraph(k)

	dependents := g.GetDependents("app-a")
	_ = dependents // just verify no panic
}

func TestDependencyGraph_Validate_NoError(t *testing.T) {
	k := &Katalog{enabledCRDs: crdMap(dep("app", "db"), dep("db"))}
	g := NewDependencyGraph(k)
	if err := g.Validate(); err != nil {
		t.Errorf("expected no validation error, got %v", err)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertBefore(t *testing.T, order []string, before, after string) {
	t.Helper()
	idx := make(map[string]int, len(order))
	for i, v := range order {
		idx[v] = i
	}
	bi, bOk := idx[before]
	ai, aOk := idx[after]
	if !bOk {
		t.Errorf("%q not found in order %v", before, order)
		return
	}
	if !aOk {
		t.Errorf("%q not found in order %v", after, order)
		return
	}
	if bi >= ai {
		sorted := append([]string{}, order...)
		sort.Strings(sorted)
		t.Errorf("expected %q before %q in order %v", before, after, order)
	}
}
