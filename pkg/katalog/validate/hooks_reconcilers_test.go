package validate

import (
	"testing"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

var testGVK = schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "MyApp"}

func katalogWith(crds map[string]orktypes.CRDEntry) *executor {
	return newKatalogExec(crds)
}

func stubHookFn() func() domain.AnyReconcileHooks {
	return func() domain.AnyReconcileHooks { return nil }
}

func stubRecFn() orktypes.NewReconcilerFunc {
	return func(kubeclient.Interface) domain.Reconciler {
		return nil
	}
}

// crdWithGVK builds a typed (non-dynamic) CRDEntry for tests.
// Setting APITypes.Location makes IsDynamic() return false so the constructor
// and per-target reconciler paths inside addReconcilers() are exercised.
func crdWithGVK(gvk schema.GroupVersionKind) orktypes.CRDEntry {
	return orktypes.CRDEntry{
		GroupVersionKind: gvk,
		APITypes: orktypes.APITypes{
			Group:    gvk.Group,
			Version:  gvk.Version,
			Kind:     gvk.Kind,
			Location: "github.com/test/apis/v1",
		},
		OperatorBox: orktypes.OperatorBoxConfig{
			Reconciler: &orktypes.ReconcilerConfig{},
		},
	}
}

func withTargetHookLocation(crd orktypes.CRDEntry, targetName, location string) orktypes.CRDEntry {
	if crd.Serve == nil {
		crd.Serve = &orktypes.ServeConfig{Enabled: true}
	}
	if crd.Serve.Target.Entries == nil {
		crd.Serve.Target.Entries = map[string]*orktypes.ServeTargetConfig{}
	}
	crd.Serve.Target.Entries[targetName] = &orktypes.ServeTargetConfig{
		OperatorBox: &orktypes.OperatorBoxConfig{
			Reconciler: &orktypes.ReconcilerConfig{
				Hooks: &orktypes.HookDeclaration{Location: location, Function: "New"},
			},
		},
	}
	return crd
}

func withTargetDefaultFalse(crd orktypes.CRDEntry, targetName string) orktypes.CRDEntry {
	if crd.Serve == nil {
		crd.Serve = &orktypes.ServeConfig{Enabled: true}
	}
	if crd.Serve.Target.Entries == nil {
		crd.Serve.Target.Entries = map[string]*orktypes.ServeTargetConfig{}
	}
	crd.Serve.Target.Entries[targetName] = &orktypes.ServeTargetConfig{
		OperatorBox: &orktypes.OperatorBoxConfig{
			Reconciler: &orktypes.ReconcilerConfig{
				Default:         boolPtr(false),
				ConstructorDecl: &orktypes.ConstructorDeclaration{Location: "github.com/test/rec", Function: "New"},
			},
		},
	}
	return crd
}

// ── addHooks ─────────────────────────────────────────────────────────────────

func TestAddHooks_WiresFactoryWhenRegistered(t *testing.T) {
	fn := stubHookFn()
	orktypes.HookRegistry[testGVK] = fn
	t.Cleanup(func() { delete(orktypes.HookRegistry, testGVK) })

	crd := crdWithGVK(testGVK)
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddHooks(); err != nil {
		t.Fatalf("addHooks returned error: %v", err)
	}
	got := k.k.EnabledCRDs()["myapp"]
	if got.OperatorBox.HookFactory == nil {
		t.Error("expected HookFactory to be set, got nil")
	}
}

func TestAddHooks_NoEntryIsOK(t *testing.T) {
	delete(orktypes.HookRegistry, testGVK)

	crd := crdWithGVK(testGVK)
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddHooks(); err != nil {
		t.Fatalf("addHooks returned unexpected error: %v", err)
	}
	if k.k.EnabledCRDs()["myapp"].OperatorBox.HookFactory != nil {
		t.Error("HookFactory should be nil when no registry entry exists")
	}
}

func TestAddHooks_ErrorWhenTargetSharesBinaryButNotRegistered(t *testing.T) {
	// Target shares the CRD-level hook binary (same location) but no factory is
	// registered. addHooks should error — the shared binary is missing.
	delete(orktypes.HookRegistry, testGVK)

	crd := crdWithGVK(testGVK)
	// Set the CRD-level hook location.
	crd.OperatorBox.Reconciler.Hooks = &orktypes.HookDeclaration{Location: "github.com/test/hooks", Function: "New"}
	// Target declares the same location — sharing the binary.
	crd = withTargetHookLocation(crd, "v2", "github.com/test/hooks")
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddHooks(); err == nil {
		t.Fatal("expected error when target shares binary but factory not registered, got nil")
	}
}

func TestAddHooks_NoErrorWhenTargetHasDistinctBinary(t *testing.T) {
	// Target has a different location → addTargetHooks handles it; addHooks should not error.
	delete(orktypes.HookRegistry, testGVK)

	crd := withTargetHookLocation(crdWithGVK(testGVK), "v2", "github.com/test/v2hooks")
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddHooks(); err != nil {
		t.Fatalf("addHooks should not error for distinct-binary target (addTargetHooks validates): %v", err)
	}
}

func TestAddHooks_SkipsNonDefaultReconcilers(t *testing.T) {
	crd := crdWithGVK(testGVK)
	crd.OperatorBox.Reconciler.Default = boolPtr(false)
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddHooks(); err != nil {
		t.Fatalf("addHooks returned error: %v", err)
	}
}

// ── addReconcilers ────────────────────────────────────────────────────────────

func TestAddReconcilers_DefaultReconcileSkipsConstructor(t *testing.T) {
	crd := crdWithGVK(testGVK)
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddReconcilers(); err != nil {
		t.Fatalf("addReconcilers returned error: %v", err)
	}
	if k.k.EnabledCRDs()["myapp"].OperatorBox.Constructor != nil {
		t.Error("Constructor should not be set for default reconciler")
	}
}

func TestAddReconcilers_WiresConstructorWhenRegistered(t *testing.T) {
	fn := stubRecFn()
	orktypes.ReconcilerRegistry[testGVK] = fn
	t.Cleanup(func() { delete(orktypes.ReconcilerRegistry, testGVK) })

	crd := crdWithGVK(testGVK)
	crd.OperatorBox.Reconciler.Default = boolPtr(false)
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddReconcilers(); err != nil {
		t.Fatalf("addReconcilers returned error: %v", err)
	}
	if k.k.EnabledCRDs()["myapp"].OperatorBox.Constructor == nil {
		t.Error("expected Constructor to be set, got nil")
	}
}

func TestAddReconcilers_ErrorWhenDefaultFalseAndNotRegistered(t *testing.T) {
	delete(orktypes.ReconcilerRegistry, testGVK)

	crd := crdWithGVK(testGVK)
	crd.OperatorBox.Reconciler.Default = boolPtr(false)
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddReconcilers(); err == nil {
		t.Fatal("expected error for missing constructor registration, got nil")
	}
}

func TestAddReconcilers_PerTargetDefaultFalseWiresConstructor(t *testing.T) {
	fn := stubRecFn()
	orktypes.ReconcilerRegistry[testGVK] = fn
	t.Cleanup(func() { delete(orktypes.ReconcilerRegistry, testGVK) })

	crd := withTargetDefaultFalse(crdWithGVK(testGVK), "v2")
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddReconcilers(); err != nil {
		t.Fatalf("addReconcilers returned error: %v", err)
	}
	entry := k.k.EnabledCRDs()["myapp"].Serve.Target.Entries["v2"]
	if entry.OperatorBox.Constructor == nil {
		t.Error("expected Constructor to be set on per-target config, got nil")
	}
}

func TestAddReconcilers_PerTargetDefaultFalseErrorWhenMissing(t *testing.T) {
	delete(orktypes.ReconcilerRegistry, testGVK)

	crd := withTargetDefaultFalse(crdWithGVK(testGVK), "v2")
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddReconcilers(); err == nil {
		t.Fatal("expected error for missing per-target constructor, got nil")
	}
}

// ── addTargetHooks ────────────────────────────────────────────────────────────

func TestAddTargetHooks_WiresFactoryForDistinctBinary(t *testing.T) {
	fn := stubHookFn()
	orktypes.TargetHookRegistry[testGVK] = map[string]func() domain.AnyReconcileHooks{
		"v2": fn,
	}
	t.Cleanup(func() { delete(orktypes.TargetHookRegistry, testGVK) })

	crd := withTargetHookLocation(crdWithGVK(testGVK), "v2", "github.com/test/v2hooks")
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddTargetHooks(); err != nil {
		t.Fatalf("addTargetHooks returned error: %v", err)
	}
	got := k.k.EnabledCRDs()["myapp"]
	if got.TargetHookFactories == nil || got.TargetHookFactories["v2"] == nil {
		t.Error("expected TargetHookFactories[v2] to be set, got nil")
	}
}

func TestAddTargetHooks_SkipsTargetWithSameBinaryAsBase(t *testing.T) {
	// Target location matches CRD-level → no TargetHookRegistry needed.
	crd := crdWithGVK(testGVK)
	crd.OperatorBox.Reconciler.Hooks = &orktypes.HookDeclaration{Location: "github.com/test/hooks"}
	crd = withTargetHookLocation(crd, "v2", "github.com/test/hooks") // same location
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddTargetHooks(); err != nil {
		t.Fatalf("addTargetHooks returned error: %v", err)
	}
	if k.k.EnabledCRDs()["myapp"].TargetHookFactories != nil {
		t.Error("TargetHookFactories should be nil when target shares base binary")
	}
}

func TestAddTargetHooks_ErrorWhenGVKMissingFromRegistry(t *testing.T) {
	delete(orktypes.TargetHookRegistry, testGVK)

	crd := withTargetHookLocation(crdWithGVK(testGVK), "v2", "github.com/test/v2hooks")
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddTargetHooks(); err == nil {
		t.Fatal("expected error when GVK missing from TargetHookRegistry, got nil")
	}
}

func TestAddTargetHooks_ErrorWhenTargetNameMissingFromRegistry(t *testing.T) {
	orktypes.TargetHookRegistry[testGVK] = map[string]func() domain.AnyReconcileHooks{
		"other": stubHookFn(), // registered for a different target
	}
	t.Cleanup(func() { delete(orktypes.TargetHookRegistry, testGVK) })

	crd := withTargetHookLocation(crdWithGVK(testGVK), "v2", "github.com/test/v2hooks")
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddTargetHooks(); err == nil {
		t.Fatal("expected error when target name missing from TargetHookRegistry, got nil")
	}
}

func TestAddTargetHooks_SkipsWhenNoServeTargetEntries(t *testing.T) {
	crd := crdWithGVK(testGVK)
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddTargetHooks(); err != nil {
		t.Fatalf("addTargetHooks returned error for CRD with no serve.target.entries: %v", err)
	}
}

// ── addTargetConstructors ─────────────────────────────────────────────────────

func TestAddTargetConstructors_WiresFactoryForDistinctConstructor(t *testing.T) {
	fn := stubRecFn()
	orktypes.TargetReconcilerRegistry[testGVK] = map[string]orktypes.NewReconcilerFunc{
		"v2": fn,
	}
	t.Cleanup(func() { delete(orktypes.TargetReconcilerRegistry, testGVK) })

	crd := withTargetDefaultFalse(crdWithGVK(testGVK), "v2")
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddTargetConstructors(); err != nil {
		t.Fatalf("addTargetConstructors returned error: %v", err)
	}
	got := k.k.EnabledCRDs()["myapp"]
	if got.TargetReconcilerFactories == nil || got.TargetReconcilerFactories["v2"] == nil {
		t.Error("expected TargetReconcilerFactories[v2] to be set, got nil")
	}
}

func TestAddTargetConstructors_SkipsTargetWithDefaultReconciler(t *testing.T) {
	// target has default: true → no TargetReconcilerRegistry needed
	crd := crdWithGVK(testGVK)
	if crd.Serve == nil {
		crd.Serve = &orktypes.ServeConfig{Enabled: true}
	}
	crd.Serve.Target.Entries = map[string]*orktypes.ServeTargetConfig{
		"v2": {OperatorBox: &orktypes.OperatorBoxConfig{Reconciler: &orktypes.ReconcilerConfig{Default: boolPtr(true)}}},
	}
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddTargetConstructors(); err != nil {
		t.Fatalf("addTargetConstructors returned error: %v", err)
	}
	if k.k.EnabledCRDs()["myapp"].TargetReconcilerFactories != nil {
		t.Error("TargetReconcilerFactories should be nil when target uses default reconciler")
	}
}

func TestAddTargetConstructors_ErrorWhenGVKMissingFromRegistry(t *testing.T) {
	delete(orktypes.TargetReconcilerRegistry, testGVK)

	crd := withTargetDefaultFalse(crdWithGVK(testGVK), "v2")
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddTargetConstructors(); err == nil {
		t.Fatal("expected error when GVK missing from TargetReconcilerRegistry, got nil")
	}
}

func TestAddTargetConstructors_ErrorWhenTargetNameMissingFromRegistry(t *testing.T) {
	orktypes.TargetReconcilerRegistry[testGVK] = map[string]orktypes.NewReconcilerFunc{
		"other": stubRecFn(),
	}
	t.Cleanup(func() { delete(orktypes.TargetReconcilerRegistry, testGVK) })

	crd := withTargetDefaultFalse(crdWithGVK(testGVK), "v2")
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddTargetConstructors(); err == nil {
		t.Fatal("expected error when target name missing from TargetReconcilerRegistry, got nil")
	}
}

func TestAddTargetConstructors_SkipsWhenNoServeTargetEntries(t *testing.T) {
	crd := crdWithGVK(testGVK)
	k := katalogWith(map[string]orktypes.CRDEntry{"myapp": crd})

	if err := k.k.AddTargetConstructors(); err != nil {
		t.Fatalf("addTargetConstructors returned error for CRD with no serve.target.entries: %v", err)
	}
}
