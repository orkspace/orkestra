package validate

import (
	"strings"
	"testing"
	"time"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func katalogWithTargetCRDs(crds map[string]orktypes.CRDEntry) *executor {
	return newKatalogExec(crds)
}

func crdWithTarget(targetName string) orktypes.CRDEntry {
	return orktypes.CRDEntry{
		APITypes: orktypes.APITypes{Kind: "MyResource"},
		Serve: &orktypes.ServeConfig{
			Enabled: true,
			Target: orktypes.ServeTargetValue{
				Entries: map[string]*orktypes.ServeTargetConfig{
					targetName: {Primary: true},
				},
			},
		},
	}
}

func crdWithTargetAndBox(targetName string, box *orktypes.OperatorBoxConfig) orktypes.CRDEntry {
	crd := crdWithTarget(targetName)
	crd.Serve.Target.Entries[targetName].OperatorBox = box
	return crd
}

// ── isValidServeTarget ────────────────────────────────────────────────────────

func TestIsValidServeTarget_Valid(t *testing.T) {
	cases := []string{"app", "my-app", "web123", "a", "apifixture", "v2-preview"}
	for _, c := range cases {
		if !isValidServeTarget(c) {
			t.Errorf("%q should be valid", c)
		}
	}
}

func TestIsValidServeTarget_Invalid(t *testing.T) {
	cases := []string{"", "MyApp", "my_app", "app app", "app.v2", "APP"}
	for _, c := range cases {
		if isValidServeTarget(c) {
			t.Errorf("%q should be invalid", c)
		}
	}
}

// ── validateServeTarget — format ──────────────────────────────────────────────

func TestValidateServeTarget_ValidFormat(t *testing.T) {
	k := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTarget("my-resource"),
	})
	if err := k.validateServeTarget(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTarget_InvalidFormat(t *testing.T) {
	k := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTarget("My_Resource"),
	})
	err := k.validateServeTarget()
	if err == nil {
		t.Fatal("expected error for invalid target format")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error should mention invalid format, got: %v", err)
	}
}

// ── validateServeTarget — uniqueness ──────────────────────────────────────────

func TestValidateServeTarget_DuplicateTarget(t *testing.T) {
	k := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res1": crdWithTarget("app"),
		"res2": crdWithTarget("app"),
	})
	err := k.validateServeTarget()
	if err == nil {
		t.Fatal("expected error for duplicate target")
	}
	if !strings.Contains(err.Error(), "app") {
		t.Errorf("error should mention the duplicate target name, got: %v", err)
	}
}

func TestValidateServeTarget_UniqueTargets(t *testing.T) {
	k := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res1": crdWithTarget("web"),
		"res2": crdWithTarget("api"),
	})
	if err := k.validateServeTarget(); err != nil {
		t.Fatalf("unexpected error for unique targets: %v", err)
	}
}

func TestValidateServeTarget_NoServe(t *testing.T) {
	k := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": {APITypes: orktypes.APITypes{Kind: "MyResource"}},
	})
	if err := k.validateServeTarget(); err != nil {
		t.Fatalf("unexpected error for CRD without serve: %v", err)
	}
}

// ── validateTargetOperatorBox — valid boxes ───────────────────────────────────

func TestValidateServeTarget_TargetBoxTemplatesOnly(t *testing.T) {
	box := &orktypes.OperatorBoxConfig{
		OnCreate:   &orktypes.HookTemplates{},
		Finalizers: []string{"my.finalizer/cleanup"},
	}
	k := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTargetAndBox("web", box),
	})
	if err := k.validateServeTarget(); err != nil {
		t.Fatalf("unexpected error for valid target box: %v", err)
	}
}

func TestValidateServeTarget_NilTargetBox(t *testing.T) {
	k := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTargetAndBox("web", nil),
	})
	if err := k.validateServeTarget(); err != nil {
		t.Fatalf("unexpected error for nil target box: %v", err)
	}
}

// ── validateTargetOperatorBox — forbidden fields ──────────────────────────────

func TestValidateServeTarget_ReconcilerWorkers(t *testing.T) {
	box := &orktypes.OperatorBoxConfig{
		Reconciler: &orktypes.ReconcilerConfig{Workers: 3},
	}
	err := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTargetAndBox("web", box),
	}).validateServeTarget()
	if err == nil {
		t.Fatal("expected error for reconciler.workers on target box")
	}
	if !strings.Contains(err.Error(), "reconciler.workers") {
		t.Errorf("error should name reconciler.workers, got: %v", err)
	}
}

func TestValidateServeTarget_ReconcilerResync(t *testing.T) {
	box := &orktypes.OperatorBoxConfig{
		Reconciler: &orktypes.ReconcilerConfig{
			Resync: orktypes.Duration{Duration: 30 * time.Second},
		},
	}
	err := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTargetAndBox("web", box),
	}).validateServeTarget()
	if err == nil {
		t.Fatal("expected error for reconciler.resync on target box")
	}
	if !strings.Contains(err.Error(), "reconciler.resync") {
		t.Errorf("error should name reconciler.resync, got: %v", err)
	}
}

func TestValidateServeTarget_ReconcilerQueue(t *testing.T) {
	box := &orktypes.OperatorBoxConfig{
		Reconciler: &orktypes.ReconcilerConfig{
			Queue: orktypes.Queue{MaxDepth: 50},
		},
	}
	err := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTargetAndBox("web", box),
	}).validateServeTarget()
	if err == nil {
		t.Fatal("expected error for reconciler.queue on target box")
	}
	if !strings.Contains(err.Error(), "reconciler.queue") {
		t.Errorf("error should name reconciler.queue, got: %v", err)
	}
}

func TestValidateServeTarget_ReconcilerProfile(t *testing.T) {
	box := &orktypes.OperatorBoxConfig{
		Reconciler: &orktypes.ReconcilerConfig{Profile: "high-throughput"},
	}
	err := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTargetAndBox("web", box),
	}).validateServeTarget()
	if err == nil {
		t.Fatal("expected error for reconciler.profile on target box")
	}
	if !strings.Contains(err.Error(), "reconciler.profile") {
		t.Errorf("error should name reconciler.profile, got: %v", err)
	}
}

func TestValidateServeTarget_Autoscale(t *testing.T) {
	box := &orktypes.OperatorBoxConfig{
		Autoscale: &orktypes.AutoscaleSpec{},
	}
	err := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTargetAndBox("web", box),
	}).validateServeTarget()
	if err == nil {
		t.Fatal("expected error for autoscale on target box")
	}
	if !strings.Contains(err.Error(), "autoscale") {
		t.Errorf("error should name autoscale, got: %v", err)
	}
}

func TestValidateServeTarget_Rollback(t *testing.T) {
	box := &orktypes.OperatorBoxConfig{
		Rollback: &orktypes.RollbackBlock{},
	}
	err := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTargetAndBox("web", box),
	}).validateServeTarget()
	if err == nil {
		t.Fatal("expected error for rollback on target box")
	}
	if !strings.Contains(err.Error(), "rollback") {
		t.Errorf("error should name rollback, got: %v", err)
	}
}

func TestValidateServeTarget_RollBackOnError(t *testing.T) {
	box := &orktypes.OperatorBoxConfig{RollBackOnError: true}
	err := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTargetAndBox("web", box),
	}).validateServeTarget()
	if err == nil {
		t.Fatal("expected error for rollBackOnError on target box")
	}
	if !strings.Contains(err.Error(), "rollBackOnError") {
		t.Errorf("error should name rollBackOnError, got: %v", err)
	}
}

func TestValidateServeTarget_MultipleViolations(t *testing.T) {
	box := &orktypes.OperatorBoxConfig{
		Reconciler: &orktypes.ReconcilerConfig{
			Workers: 5,
			Profile: "high-throughput",
		},
		Autoscale: &orktypes.AutoscaleSpec{},
	}
	err := katalogWithTargetCRDs(map[string]orktypes.CRDEntry{
		"res": crdWithTargetAndBox("web", box),
	}).validateServeTarget()
	if err == nil {
		t.Fatal("expected error for multiple violations")
	}
	msg := err.Error()
	for _, field := range []string{"reconciler.workers", "reconciler.profile", "autoscale"} {
		if !strings.Contains(msg, field) {
			t.Errorf("error should name %q, got: %v", field, msg)
		}
	}
}
