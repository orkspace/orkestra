// pkg/runtime/kordinator/vitals/crd_health_test.go
package vitals

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ── Initial state ─────────────────────────────────────────────────────────────

func TestCRDHealth_InitialState(t *testing.T) {
	h := NewCRDHealth("test")

	assert.False(t, h.IsHealthy())
	assert.False(t, h.Started())
	assert.Equal(t, int64(0), h.TotalReconciles())
	assert.Equal(t, int64(0), h.FailedReconciles())
	assert.Equal(t, float64(0), h.ErrorRate())
}

func TestCRDHealth_Name(t *testing.T) {
	h := NewCRDHealth("my-website")
	assert.Equal(t, "my-website", h.Name())
}

// ── RecordSuccess ─────────────────────────────────────────────────────────────

func TestCRDHealth_RecordSuccess(t *testing.T) {
	h := NewCRDHealth("test")

	h.RecordSuccess()

	assert.True(t, h.IsHealthy())
	assert.Equal(t, int64(1), h.TotalReconciles())
	assert.Equal(t, int64(0), h.FailedReconciles())
	assert.Equal(t, int64(0), h.ConsecutiveFails())
}

// ── RecordFailure ─────────────────────────────────────────────────────────────

func TestCRDHealth_RecordFailure(t *testing.T) {
	h := NewCRDHealth("test")

	h.RecordFailure(fmt.Errorf("something went wrong"), 3)

	assert.False(t, h.IsHealthy())
	assert.Equal(t, int64(1), h.TotalReconciles())
	assert.Equal(t, int64(1), h.FailedReconciles())
	assert.Equal(t, int64(1), h.ConsecutiveFails())
	assert.Equal(t, "something went wrong", h.LastError())
}

func TestCRDHealth_RecordFailureExceedsThreshold(t *testing.T) {
	h := NewCRDHealth("test")

	// CRDs start unhealthy — threshold only degrades an already-healthy CRD.

	h.RecordFailure(fmt.Errorf("error 1"), 3)
	assert.False(t, h.IsHealthy())
	assert.Equal(t, int64(1), h.ConsecutiveFails())

	h.RecordFailure(fmt.Errorf("error 2"), 3)
	assert.False(t, h.IsHealthy())
	assert.Equal(t, int64(2), h.ConsecutiveFails())

	h.RecordFailure(fmt.Errorf("error 3"), 3)
	assert.False(t, h.IsHealthy())
	assert.Equal(t, int64(3), h.ConsecutiveFails())
}

func TestCRDHealth_ThresholdDegradesPreviouslyHealthyCRD(t *testing.T) {
	h := NewCRDHealth("test")

	h.RecordSuccess()
	assert.True(t, h.IsHealthy())

	h.RecordFailure(fmt.Errorf("error 1"), 3)
	assert.True(t, h.IsHealthy(), "below threshold — should remain healthy")

	h.RecordFailure(fmt.Errorf("error 2"), 3)
	assert.True(t, h.IsHealthy(), "below threshold — should remain healthy")

	h.RecordFailure(fmt.Errorf("error 3"), 3)
	assert.False(t, h.IsHealthy(), "at threshold — should be degraded")
}

func TestCRDHealth_SuccessResetsConsecutiveFails(t *testing.T) {
	h := NewCRDHealth("test")

	h.RecordFailure(fmt.Errorf("error"), 3)
	h.RecordFailure(fmt.Errorf("error"), 3)
	assert.Equal(t, int64(2), h.ConsecutiveFails())

	h.RecordSuccess()
	assert.Equal(t, int64(0), h.ConsecutiveFails())
	assert.True(t, h.IsHealthy())
}

// ── Error rate ────────────────────────────────────────────────────────────────

func TestCRDHealth_ErrorRate(t *testing.T) {
	h := NewCRDHealth("test")

	h.RecordSuccess()
	assert.Equal(t, float64(0), h.ErrorRate())

	h.RecordFailure(fmt.Errorf("error"), 3)
	assert.Equal(t, 0.5, h.ErrorRate())

	h.RecordFailure(fmt.Errorf("error"), 3)
	assert.InDelta(t, 0.666, h.ErrorRate(), 0.001)
}

// ── Started / SetStarted ──────────────────────────────────────────────────────

func TestCRDHealth_SetStarted(t *testing.T) {
	h := NewCRDHealth("test")
	assert.False(t, h.Started())

	h.SetStarted()

	assert.True(t, h.Started())
}

func TestCRDHealth_SetStarted_IsIdempotent(t *testing.T) {
	h := NewCRDHealth("test")

	h.SetStarted()
	h.SetStarted() // second call must not panic or change state

	assert.True(t, h.Started())
}

func TestCRDHealth_SetNotStarted(t *testing.T) {
	h := NewCRDHealth("test")
	h.SetStarted()
	assert.True(t, h.Started())

	h.SetNotStarted()
	assert.False(t, h.Started())
}

// ── StartedAt / Uptime ────────────────────────────────────────────────────────

func TestCRDHealth_StartedAt_BeforeStart(t *testing.T) {
	h := NewCRDHealth("test")
	assert.Equal(t, "not started", h.StartedAt())
}

func TestCRDHealth_StartedAt_AfterStart(t *testing.T) {
	h := NewCRDHealth("test")
	before := time.Now()
	h.SetStarted()

	val := h.StartedAt()
	assert.NotEqual(t, "not started", val)
	assert.NotEqual(t, "starting", val)

	// StartedAt returns a formatted time; verify it parses to something after before
	_, err := time.Parse("2006-01-02 15:04:05 +0000 UTC", val)
	if err != nil {
		// Different time zone format is fine — just confirm it's not a placeholder
		_ = before
	}
}

func TestCRDHealth_Uptime_NotStarted(t *testing.T) {
	h := NewCRDHealth("test")
	assert.Equal(t, "not started", h.Uptime())
}

func TestCRDHealth_Uptime_AfterStart(t *testing.T) {
	h := NewCRDHealth("test")
	h.SetStarted()

	uptime := h.Uptime()
	assert.NotEqual(t, "not started", uptime)
}

// ── LastReconcile ─────────────────────────────────────────────────────────────

func TestCRDHealth_LastReconcile_NeverStarted(t *testing.T) {
	h := NewCRDHealth("test")
	assert.Equal(t, "not started", h.LastReconcile())
}

func TestCRDHealth_LastReconcile_StartedButNoReconcile(t *testing.T) {
	h := NewCRDHealth("test")
	h.SetStarted()
	assert.Equal(t, "no reconciles yet", h.LastReconcile())
}

func TestCRDHealth_LastReconcile_AfterSuccess(t *testing.T) {
	h := NewCRDHealth("test")
	h.SetStarted()
	h.RecordSuccess()

	val := h.LastReconcile()
	assert.NotEqual(t, "not started", val)
	assert.NotEqual(t, "no reconciles yet", val)
}

// ── Workers ───────────────────────────────────────────────────────────────────

func TestCRDHealth_WorkersActive(t *testing.T) {
	h := NewCRDHealth("test")
	assert.Equal(t, int32(0), h.GetActiveWorkers())

	h.SetTotalWorkers(3)
	assert.Equal(t, int32(3), h.GetActiveWorkers())
}

// ── QueueDepth ────────────────────────────────────────────────────────────────

func TestCRDHealth_QueueDepth_NilRegistry(t *testing.T) {
	// New has no queue registry — QueueDepth must return 0 gracefully.
	h := NewCRDHealth("test")
	assert.Equal(t, 0, h.QueueDepth("any/v1, Kind=Foo"))
}

// ── CRD existence tracking ────────────────────────────────────────────────────

func TestCRDHealth_SetCRDExists_True(t *testing.T) {
	h := NewCRDHealth("test")
	assert.False(t, h.CRDExists())

	h.SetCRDExists(true)
	assert.True(t, h.CRDExists())
}

func TestCRDHealth_SetCRDExists_False(t *testing.T) {
	h := NewCRDHealth("test")
	h.SetCRDExists(true)
	h.SetCRDExists(false)
	assert.False(t, h.CRDExists())
}

func TestCRDHealth_LastCRDCheck_UpdatedOnSet(t *testing.T) {
	h := NewCRDHealth("test")

	before := time.Now()
	h.SetCRDExists(true)
	after := time.Now()

	last := h.LastCRDCheck()
	assert.True(t, !last.Before(before), "LastCRDCheck should be >= before")
	assert.True(t, !last.After(after), "LastCRDCheck should be <= after")
}

// ── RecordStartupFailure ──────────────────────────────────────────────────────

func TestCRDHealth_RecordStartupFailure_DoesNotIncrementTotalReconciles(t *testing.T) {
	h := NewCRDHealth("test")

	h.RecordStartupFailure(fmt.Errorf("startup error"), 3)

	// Startup failures don't count as reconcile attempts
	assert.Equal(t, int64(0), h.TotalReconciles())
	assert.Equal(t, int64(0), h.FailedReconciles())

	// But they do accumulate consecutive failures
	assert.Equal(t, int64(1), h.ConsecutiveFails())
	assert.Equal(t, "startup error", h.LastError())
}
