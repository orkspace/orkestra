package katalog

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// serveEntry builds a minimal serve-enabled CRDEntry for tests.
// If targetName is empty, ServeTarget() falls back to the lowercased kind.
func serveEntry(kind, targetName string) orktypes.CRDEntry {
	tv := orktypes.ServeTargetValue{}
	if targetName != "" {
		tv.Entries = map[string]*orktypes.ServeTargetConfig{
			targetName: {Primary: true},
		}
	}
	return orktypes.CRDEntry{
		APITypes: orktypes.APITypes{Kind: kind},
		Serve: &orktypes.ServeConfig{
			Enabled: true,
			Target:  tv,
		},
	}
}

// withAliases returns a copy of e with the given alias entries merged into Target.Entries.
func withAliases(e orktypes.CRDEntry, aliases map[string]*orktypes.ServeTargetConfig) orktypes.CRDEntry {
	if e.Serve.Target.Entries == nil {
		e.Serve.Target.Entries = make(map[string]*orktypes.ServeTargetConfig)
	}
	for name, cfg := range aliases {
		e.Serve.Target.Entries[name] = cfg
	}
	return e
}

// katalogWith builds a minimal Katalog with the given enabledCRDs map.
func katalogWith(crds map[string]orktypes.CRDEntry) *Katalog {
	return &Katalog{enabledCRDs: crds}
}

func TestLookupByTargetOrAlias_PrimaryTarget(t *testing.T) {
	k := katalogWith(map[string]orktypes.CRDEntry{
		"smartapp": serveEntry("SmartApp", "smartapp"),
	})
	r := k.LookupByTargetOrAlias("smartapp")
	if r == nil {
		t.Fatal("expected resolution, got nil")
	}
	if r.Alias != "" {
		t.Errorf("Alias = %q, want empty (primary target hit)", r.Alias)
	}
	if r.CRD.APITypes.Kind != "SmartApp" {
		t.Errorf("Kind = %q, want SmartApp", r.CRD.APITypes.Kind)
	}
}

func TestLookupByTargetOrAlias_KindDefault(t *testing.T) {
	// No explicit target → kind is lowercased
	k := katalogWith(map[string]orktypes.CRDEntry{
		"app": serveEntry("App", ""),
	})
	r := k.LookupByTargetOrAlias("app")
	if r == nil {
		t.Fatal("expected resolution via lowercased kind default")
	}
}

func TestLookupByTargetOrAlias_Alias(t *testing.T) {
	crd := withAliases(serveEntry("SmartApp", "smartapp"), map[string]*orktypes.ServeTargetConfig{
		"public":   {},
		"internal": {},
	})
	k := katalogWith(map[string]orktypes.CRDEntry{"smartapp": crd})

	r := k.LookupByTargetOrAlias("public")
	if r == nil {
		t.Fatal("expected alias resolution")
	}
	if r.Alias != "public" {
		t.Errorf("Alias = %q, want %q", r.Alias, "public")
	}
	if r.CRD.APITypes.Kind != "SmartApp" {
		t.Errorf("Kind = %q, want SmartApp", r.CRD.APITypes.Kind)
	}
}

func TestLookupByTargetOrAlias_PrimaryWinsOverAlias(t *testing.T) {
	// "shared" is both a primary target on one CRD and an alias on another.
	// Primary target must win.
	crd1 := serveEntry("App", "shared")
	crd2 := withAliases(serveEntry("SmartApp", "smartapp"), map[string]*orktypes.ServeTargetConfig{
		"shared": {},
	})
	k := katalogWith(map[string]orktypes.CRDEntry{
		"app":      crd1,
		"smartapp": crd2,
	})
	r := k.LookupByTargetOrAlias("shared")
	if r == nil {
		t.Fatal("expected resolution")
	}
	if r.Alias != "" {
		t.Errorf("expected primary target hit (Alias Empty(), got alias %q", r.Alias)
	}
	if r.CRD.ServeTarget() != "shared" {
		t.Errorf("ServeTarget = %q, want %q (primary target winner)", r.CRD.ServeTarget(), "shared")
	}
}

// TestScalarTargetNoAliases asserts that a CRD with only a primary target
// (scalar or single-entry map) has no aliases — any other name returns nil.
func TestScalarTargetNoAliases(t *testing.T) {
	crd := serveEntry("SmartApp", "smartapp")
	// serveEntry builds {Entries: {"smartapp": {Primary: true}}} — no aliases.
	if crd.HasServeAliases() {
		t.Error("scalar shorthand target should have no aliases")
	}
	if crd.HasServeAliases() {
		t.Error("ServeAliases() should be empty for single-target CRD")
	}
	k := katalogWith(map[string]orktypes.CRDEntry{"smartapp": crd})
	if r := k.LookupByTargetOrAlias("anything-else"); r != nil {
		t.Errorf("expected nil for unknown name on single-target CRD, got %+v", r)
	}
}

func TestLookupByTargetOrAlias_NotFound(t *testing.T) {
	k := katalogWith(map[string]orktypes.CRDEntry{
		"app": serveEntry("App", "app"),
	})
	if r := k.LookupByTargetOrAlias("unknown"); r != nil {
		t.Errorf("expected nil, got %+v", r)
	}
}

func TestLookupByTargetOrAlias_NilKatalog(t *testing.T) {
	var k *Katalog
	if r := k.LookupByTargetOrAlias("anything"); r != nil {
		t.Error("nil katalog should return nil")
	}
}

func TestLookupByTargetOrAlias_EmptyTarget(t *testing.T) {
	k := katalogWith(map[string]orktypes.CRDEntry{
		"app": serveEntry("App", "app"),
	})
	if r := k.LookupByTargetOrAlias(""); r != nil {
		t.Error("empty target should return nil")
	}
}

func TestAvailableTargets_IncludesAliases(t *testing.T) {
	crd := withAliases(serveEntry("SmartApp", "smartapp"), map[string]*orktypes.ServeTargetConfig{
		"public": {},
		"v2":     {},
	})
	k := katalogWith(map[string]orktypes.CRDEntry{"smartapp": crd})

	targets := k.AvailableTargets()
	want := map[string]bool{"smartapp": true, "public": true, "v2": true}
	for _, t2 := range targets {
		delete(want, t2)
	}
	if len(want) > 0 {
		t.Errorf("AvailableTargets missing: %v", want)
	}
}
