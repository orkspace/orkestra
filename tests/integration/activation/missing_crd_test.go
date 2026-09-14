//go:build integration

// tests/integration/activation/missing_crd_test.go
// Tests for CRD health activation lifecycle — CRD appears, disappears, reappears.
package activation_test

import (
	"fmt"
	"testing"

	"github.com/orkspace/orkestra/pkg/runtime/kordinator/vitals"
)

func TestCRDActivation_InitialState_NotHealthy(t *testing.T) {
	h := vitals.NewCRDHealth("website")
	if h.IsHealthy() {
		t.Error("must start unhealthy")
	}
	if h.Started() {
		t.Error("must start unstarted")
	}
	if h.CRDExists() {
		t.Error("must report CRD absent")
	}
}

func TestCRDActivation_CRDAppearsInCluster(t *testing.T) {
	h := vitals.NewCRDHealth("website")
	h.SetCRDExists(false)
	if h.CRDExists() {
		t.Error("should be absent after SetCRDExists(false)")
	}

	h.SetCRDExists(true)
	h.SetStarted()
	h.RecordSuccess()

	if !h.CRDExists() {
		t.Error("should be present after SetCRDExists(true)")
	}
	if !h.Started() {
		t.Error("should be started after SetStarted()")
	}
	if !h.IsHealthy() {
		t.Error("should be healthy after RecordSuccess()")
	}
}

func TestCRDActivation_CRDDeletedAfterStartup(t *testing.T) {
	h := vitals.NewCRDHealth("website")
	h.SetStarted()
	h.RecordSuccess()
	h.SetCRDExists(true)

	h.SetCRDExists(false)
	h.SetNotStarted()

	if h.CRDExists() {
		t.Error("should be absent after deletion")
	}
	if h.Started() {
		t.Error("should be unstarted after deletion")
	}
}

func TestCRDActivation_CRDReappearsAfterDeletion(t *testing.T) {
	h := vitals.NewCRDHealth("website")
	h.SetStarted()
	h.RecordSuccess()
	h.SetCRDExists(true)

	h.SetCRDExists(false)
	h.SetNotStarted()

	h.SetCRDExists(true)
	h.SetStarted()
	h.RecordSuccess()

	if !h.CRDExists() {
		t.Error("should exist after reappearing")
	}
	if !h.IsHealthy() {
		t.Error("should be healthy after reactivation")
	}
}

func TestCRDActivation_StartupFailureDoesNotIncrementReconciles(t *testing.T) {
	h := vitals.NewCRDHealth("website")
	h.RecordStartupFailure(fmt.Errorf("crd not ready"), 3)
	h.RecordStartupFailure(fmt.Errorf("crd not ready"), 3)

	if h.TotalReconciles() != 0 {
		t.Errorf("startup failures must not increment TotalReconciles, got %d", h.TotalReconciles())
	}
	if h.ConsecutiveFails() != 2 {
		t.Errorf("expected 2 consecutive fails, got %d", h.ConsecutiveFails())
	}
}
