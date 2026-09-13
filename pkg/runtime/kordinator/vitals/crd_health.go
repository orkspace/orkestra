// pkg/runtime/kordinator/vitals/crd_health.go
package vitals

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	ork_autoscaler "github.com/orkspace/orkestra/pkg/runtime/autoscaler"
	"github.com/orkspace/orkestra/pkg/runtime/queue"
)

// CRDHealth tracks the runtime health of a single CRD's reconciler.
// It is fully concurrency‑safe and designed to be updated from multiple goroutines.
//
// Fields tracked:
//
//   - started:           whether the reconciler has begun processing events
//
//   - healthy:           whether the reconciler is currently considered healthy
//
//   - totalReconciles:   total number of reconcile attempts (success + failure)
//
//   - failedReconciles:  number of failed reconciles
//
//   - consecutiveFails:  number of failures in a row (used for degradation)
//
//   - lastError:         last error message (string)
//
//   - lastReconcile:     timestamp of last reconcile attempt
//
//   - startTime:         timestamp when the reconciler first started
//
//   - The activeWarnings: tracks current warn-mode violations per CR.
//     It answers the question: "which CRs are currently violating advisory rules?"
//
//     Map key: "namespace/name" — unique per CR
//     Map value: slice of ActiveWarning, one per violated warn rule
//
// This struct powers:
//   - /katalog/<crd> endpoint
//   - /katalog/<crd>/health endpoint
//   - dashboard status
//   - operator self‑diagnostics
type CRDHealth struct {
	name             string
	started          atomic.Bool
	pending          atomic.Bool
	healthy          atomic.Bool
	degraded         atomic.Bool
	totalReconciles  atomic.Int64
	failedReconciles atomic.Int64
	consecutiveFails atomic.Int64
	lastError        atomic.Value // stores string
	lastReconcile    atomic.Value // stores time.Time
	startTime        atomic.Value // stores time.Time
	queueReg         *queue.QueueRegistry

	// track CRD readines
	lastCRDCheck time.Time
	crdExists    atomic.Bool
	crdCheckMu   sync.RWMutex

	// track workers
	totalWorkers      atomic.Int32
	idleWorkers       atomic.Int32
	processingWorkers atomic.Int32

	// Track individual worker states for debugging
	workerStates sync.Map // workerID -> state (idle, processing, stopped)
	gvk          string   // Store GVK for metrics

	// Dependency tracking
	dependencies     map[string]DependencyStatus
	dependenciesMu   sync.RWMutex
	hasUnhealthyDeps atomic.Bool // Overall dependency health status
	healthySignaled  atomic.Bool

	// Track katalog health through the orkHealth tracker
	orkHealth *RuntimeHealth

	// workerInfoFn returns the live WorkerInfo for this operatorbox.
	// Set by startCRDWorkers after the reconciler is constructed.
	// nil when autoscale is not declared or reconciler doesn't expose metrics.
	workerInfoFn func() *ork_autoscaler.WorkerInfo

	// autoMetricsFn returns the live AutoMetrics as a string-keyed map.
	// Used to populate the "metrics" key in the /katalog/{crd} response so
	// cross-binary autoscale conditions can read them via HTTP fallback.
	autoMetricsFn func() map[string]interface{}

	// Rollback stats — updated by the reconciler on each rollback event.
	rollbackTotal      atomic.Int64
	rollbackActive     atomic.Bool
	rollbackLastAt     atomic.Value // stores time.Time or zero
	rollbackMu         sync.RWMutex
	rollbackLastReason string // protected by rollbackMu

	// Gate state — set when a reconcile item is discarded by pre-reconcile conditions.
	// Cleared on the next successful reconcile.
	gated       atomic.Bool
	gatedReason atomic.Value // stores string
}

// SetWorkerInfoFn stores the function that returns live WorkerInfo for this CRD.
// Called by startCRDWorkers after constructing the reconciler.
func (h *CRDHealth) SetWorkerInfoFn(fn func() *ork_autoscaler.WorkerInfo) {
	h.workerInfoFn = fn
}

// GetWorkerInfo returns the live WorkerInfo, or nil when not available.
func (h *CRDHealth) GetWorkerInfo() *ork_autoscaler.WorkerInfo {
	if h.workerInfoFn == nil {
		return nil
	}
	info := h.workerInfoFn()
	return info
}

// SetAutoMetricsFn stores the function that returns live AutoMetrics as a map.
// Called by startCRDWorkers when autoscale: is declared.
func (h *CRDHealth) SetAutoMetricsFn(fn func() map[string]interface{}) {
	h.autoMetricsFn = fn
}

// GetAutoMetrics returns the live AutoMetrics map, or nil when not available.
// Included in the /katalog/{crd} response as "metrics" so cross-binary
// autoscale conditions can observe this CRD's metrics via HTTP fallback.
func (h *CRDHealth) GetAutoMetrics() map[string]interface{} {
	if h.autoMetricsFn == nil {
		return nil
	}
	return h.autoMetricsFn()
}

// RollbackStats is a snapshot of rollback activity for one CRD.
type RollbackStats struct {
	// TotalRollbacks is the number of times rollback was triggered since startup.
	TotalRollbacks int64 `json:"totalRollbacks"`
	// Active is true when rollback is currently blocking normal reconciliation.
	Active bool `json:"active"`
	// LastRollbackAt is the RFC3339 timestamp of the most recent rollback trigger.
	// Empty when no rollback has occurred.
	LastRollbackAt string `json:"lastRollbackAt,omitempty"`
}

// RecordRollbackTriggered increments the rollback counter and marks rollback active.
// Called by the reconciler when it triggers a rollback.
func (h *CRDHealth) RecordRollbackTriggered() {
	h.rollbackTotal.Add(1)
	h.rollbackActive.Store(true)
	h.rollbackLastAt.Store(time.Now())
}

// RecordRollbackCleared marks rollback inactive.
// Called by the reconciler when it clears the rollback annotation.
func (h *CRDHealth) RecordRollbackCleared() {
	h.rollbackActive.Store(false)
}

// RollbackStats returns a snapshot of rollback activity for this CRD.
func (h *CRDHealth) RollbackStats() RollbackStats {
	s := RollbackStats{
		TotalRollbacks: h.rollbackTotal.Load(),
		Active:         h.rollbackActive.Load(),
	}
	if v := h.rollbackLastAt.Load(); v != nil {
		if t, ok := v.(time.Time); ok && !t.IsZero() {
			s.LastRollbackAt = t.Format(time.RFC3339)
		}
	}
	return s
}

type DependencyStatus struct {
	Name                string `json:"name"`
	State               string `json:"state"`               // "healthy", "degraded", "missing", "started"
	Condition           string `json:"condition"`           // "started", "healthy", "ready"
	AcceptableCondition string `json:"acceptableCondition"` // "started", "healthy", "ready"
	Satisfied           bool   `json:"satisfied"`
	LastCheck           string `json:"lastCheck,omitempty"`
}

// RecordSuccess marks a successful reconcile event.
// It resets the consecutive failure counter and updates timestamps.
func (h *CRDHealth) RecordSuccess() {
	h.totalReconciles.Add(1)
	h.consecutiveFails.Store(0)
	h.lastReconcile.Store(time.Now())
	h.healthy.Store(true)
	h.pending.Store(false)
	h.degraded.Store(false)
	h.gated.Store(false)

	// If all online for this katalog
	if h.orkHealth.allOnline.Load() {
		h.orkHealth.katReady.Store(true)
	}
}

// RecordFailure marks a failed reconcile event.
// It increments failure counters, stores the error, and may degrade health
// if the number of consecutive failures exceeds the configured threshold.
func (h *CRDHealth) RecordFailure(err error, degradeThreshold int) {
	h.totalReconciles.Add(1)
	h.failedReconciles.Add(1)
	h.consecutiveFails.Add(1)
	h.lastError.Store(err.Error())
	h.lastReconcile.Store(time.Now())

	// If too many failures in a row, mark the reconciler and katalog unhealthy.
	if h.consecutiveFails.Load() >= int64(degradeThreshold) {
		h.orkHealth.mu.Lock()
		h.orkHealth.katReady.Store(false)
		h.orkHealth.mu.Unlock()

		h.crdCheckMu.Lock()
		defer h.crdCheckMu.Unlock()

		h.healthy.Store(false)
		h.pending.Store(false)
		h.degraded.Store(true)
	}
}

// RecordStartupFailure is used when the reconciler fails before it has fully started.
// It does not affect total reconcile counts, only consecutive failure tracking.
func (h *CRDHealth) RecordStartupFailure(err error, degradeThreshold int) {
	h.consecutiveFails.Add(1)
	h.lastError.Store(err.Error())
}

// RecordGated records that a reconcile item was discarded because pre-reconcile
// gate conditions were not met. The reconciler was not called — this is not a
// failure and does not affect error rate or degradation thresholds.
func (h *CRDHealth) RecordGated(reason string) {
	h.gated.Store(true)
	h.gatedReason.Store(reason)
}

// IsGated reports whether the most recent reconcile item was discarded by the
// pre-reconcile gate. Cleared on the next successful reconcile.
func (h *CRDHealth) IsGated() bool {
	return h.gated.Load()
}

// GatedReason returns the human-readable reason the last item was gated.
// Empty when IsGated is false.
func (h *CRDHealth) GatedReason() string {
	v, _ := h.gatedReason.Load().(string)
	return v
}

// ErrorRate returns the ratio of failed reconciles to total reconciles.
// If no reconciles have occurred, the error rate is 0.
func (h *CRDHealth) ErrorRate() float64 {
	total := h.totalReconciles.Load()
	if total == 0 {
		return 0
	}
	return float64(h.failedReconciles.Load()) / float64(total)
}

// ErrorRatePercent returns the error rate as a percentage.
func (h *CRDHealth) ErrorRatePercent() float64 {
	return h.ErrorRate() * 100
}

// LastReconcile returns a human‑readable timestamp of the last reconcile.
// If the reconciler has started but not yet reconciled, it returns a placeholder.
func (h *CRDHealth) LastReconcile() string {
	v := h.lastReconcile.Load()

	// Case 1: nothing stored yet
	if v == nil {
		if h.Started() {
			return "no reconciles yet"
		}
		return "not started"
	}

	// Case 2: stored value is nil inside interface{}
	t, ok := v.(time.Time)
	if !ok || t.IsZero() {
		if h.Started() {
			return "no reconciles yet"
		}
		return "not started"
	}

	return t.UTC().Format(time.RFC3339)
}

// IsHealthy reports whether the reconciler is currently considered healthy.
// Health is degraded after N consecutive failures.
func (h *CRDHealth) IsHealthy() bool {
	return h.healthy.Load()
}

// Started reports whether the reconciler has begun processing events.
func (h *CRDHealth) Started() bool {
	return h.started.Load()
}

// Pending reports the reconciler has started but yet not yet reconciled.
func (h *CRDHealth) Pending() bool {
	return h.pending.Load()
}

// StartedAt returns the timestamp when the reconciler first started.
// If not started, returns "not started". If starting, returns "starting".
func (h *CRDHealth) StartedAt() string {
	v := h.startTime.Load()
	t, ok := v.(time.Time)
	if !ok {
		return "not started"
	}
	if t.IsZero() {
		return "starting"
	}
	return t.UTC().Format(time.RFC3339)
}

// SetStarted marks the reconciler as started and records the start time.
// CompareAndSwap ensures the timestamp is only set once.
// pending is only set to true if the CRD has not yet had a successful reconcile —
// avoids flipping healthy CRDs back to pending on resync-triggered worker restarts.
func (h *CRDHealth) SetStarted() {
	h.startTime.CompareAndSwap(nil, time.Now())
	h.started.Store(true)
	if !h.healthy.Load() {
		h.pending.Store(true)
	}
}

// SetDegraded marks the reconciler as degraded
// This is used when a CRD goes missing at runtime
func (h *CRDHealth) SetDegraded() {
	h.healthy.Store(false)
	h.pending.Store(false)
	h.degraded.Store(true)
}

func (h *CRDHealth) SignaledHealthy() bool {
	return h.healthySignaled.Load()
}

func (h *CRDHealth) MarkHealthySignaled() {
	h.healthySignaled.Store(true)
}

// SetGVK records the GroupVersionKind this health record belongs to.
func (h *CRDHealth) SetGVK(gvk string) {
	h.gvk = gvk
}

// SetNotStarted marks the reconciler as not started.
func (h *CRDHealth) SetNotStarted() {
	h.started.Store(false)
}

func (h *CRDHealth) SetMissingAtRuntime() {
	h.crdCheckMu.Lock()
	defer h.crdCheckMu.Unlock()

	h.lastCRDCheck = time.Now()
	h.crdExists.Store(false)
	h.healthy.Store(false)
	h.pending.Store(false)
	h.consecutiveFails.Add(1)
	h.lastError.Store("CRD missing at runtime")
}

func (h *CRDHealth) IsMissing() bool {
	return h.crdExists.Load()
}

// Name returns the CRD name associated with this health tracker.
func (h *CRDHealth) Name() string {
	return h.name
}

// TotalReconciles returns the total number of reconcile attempts.
func (h *CRDHealth) TotalReconciles() int64 {
	return h.totalReconciles.Load()
}

// FailedReconciles returns the number of failed reconcile attempts.
func (h *CRDHealth) FailedReconciles() int64 {
	return h.failedReconciles.Load()
}

// LastError returns the last recorded error message.
// If no error has occurred, it returns an empty string.
func (h *CRDHealth) LastError() string {
	if v, ok := h.lastError.Load().(string); ok {
		return v
	}
	return ""
}

// ConsecutiveFails returns the number of consecutive failed reconciles.
func (h *CRDHealth) ConsecutiveFails() int64 {
	return h.consecutiveFails.Load()
}

// Uptime returns how long the reconciler has been running.
// If not started, returns "not started".
func (h *CRDHealth) Uptime() string {
	v := h.startTime.Load()
	t, ok := v.(time.Time)
	if !ok {
		return "not started"
	}
	return time.Since(t).Round(time.Second).String()
}

// QueueDepth returns the queue for this CRD
func (h *CRDHealth) QueueDepth(gvk string) int {
	if h.queueReg == nil {
		return 0
	}
	depth := h.queueReg.Depth(gvk)
	if depth < 0 {
		return 0
	}
	return depth
}

// SetCRDExists records that the CRD exists in the cluster.
func (h *CRDHealth) SetCRDExists(exists bool) {
	h.crdExists.Store(exists)
	h.crdCheckMu.Lock()
	defer h.crdCheckMu.Unlock()
	h.lastCRDCheck = time.Now()
}

// SetQueueReg assigns the queue registry used to track this CRD's reconcile queue.
func (h *CRDHealth) SetQueueReg(reg *queue.QueueRegistry) {
	h.queueReg = reg
}

// CRDExists returns whether the CRD exists in the cluster.
func (h *CRDHealth) CRDExists() bool {
	return h.crdExists.Load()
}

// LastCRDCheck returns when the CRD existence was last verified.
func (h *CRDHealth) LastCRDCheck() time.Time {
	h.crdCheckMu.RLock()
	defer h.crdCheckMu.RUnlock()
	return h.lastCRDCheck
}

// Dependency tracking
func (h *CRDHealth) UpdateDependencyStatus(depName string, status DependencyStatus) {
	h.dependenciesMu.Lock()
	defer h.dependenciesMu.Unlock()

	if h.dependencies == nil {
		h.dependencies = make(map[string]DependencyStatus)
	}
	status.LastCheck = time.Now().Format(time.RFC3339)
	h.dependencies[depName] = status
}

// SetDependencyHealth updates a single dependency's status
func (h *CRDHealth) SetDependencyHealth(depName string, status DependencyStatus) {
	h.dependenciesMu.Lock()
	defer h.dependenciesMu.Unlock()

	if h.dependencies == nil {
		h.dependencies = make(map[string]DependencyStatus)
	}

	status.LastCheck = time.Now().Format(time.RFC3339)
	h.dependencies[depName] = status

	// Recalculate overall health after updating
	h.recalculateOverallDependencyHealth()
}

// recalculateOverallDependencyHealth checks all dependencies and updates the atomic bool
// It uses the long-running updates from dependencyHealthChecker() goroutine
func (h *CRDHealth) recalculateOverallDependencyHealth() {
	anyUnhealthy := false
	for _, dep := range h.dependencies {
		if !dep.Satisfied {
			anyUnhealthy = true
			break
		}
	}
	h.hasUnhealthyDeps.Store(anyUnhealthy)
}

// HasUnhealthyDependencies returns true if any dependency is not satisfied
func (h *CRDHealth) HasUnhealthyDependencies() bool {
	return h.hasUnhealthyDeps.Load()
}

// GetDependencyStatuses returns a copy of all dependency statuses
func (h *CRDHealth) GetDependencyStatuses() map[string]DependencyStatus {
	h.dependenciesMu.RLock()
	defer h.dependenciesMu.RUnlock()

	result := make(map[string]DependencyStatus, len(h.dependencies))
	for k, v := range h.dependencies {
		result[k] = v
	}
	return result
}

// StateAndStatus returns the reconciler's derived health state and the
// corresponding HTTP status code used by the /health endpoint.
//
// State is one of:
//
//	"not started", "pending", "degraded", "healthy"
//
// Status is either:
//
//	200 — healthy or pending
//	503 — degraded or not started
func (h *CRDHealth) StateAndStatus() (string, int) {
	if h == nil {
		return "not started", http.StatusServiceUnavailable
	}

	isStarted := h.Started()
	isPending := h.Pending()
	isHealthy := h.IsHealthy()

	switch {
	case !isStarted && !isPending:
		return "not started", http.StatusServiceUnavailable
	case isPending:
		return "pending", http.StatusOK
	case isStarted && !isHealthy:
		return "degraded", http.StatusServiceUnavailable
	case isHealthy:
		return "healthy", http.StatusOK
	default:
		return "pending", http.StatusOK
	}
}

// HealthAsMap returns a snapshot of this CRD's health as a plain map, suitable
// for injection into the template resolver under the "health" key.
//
// Available in conditions status.fields templates as:
//
//		{{ .health.healthy }}            — bool: reconciler is healthy
//	 	{{ .health.state }}              — string: "healthy" / "degraded" / "pending" / "not started"
//	 	{{ .health.status }}             — int: HTTP status code representing health state
//		{{ .health.started }}            — bool: reconciler has started
//		{{ .health.pending }}            — bool: reconciler is pending
//		{{ .health.degraded }}           — bool: reconciler is degraded
//		{{ .health.totalReconciles }}    — int64: total reconcile attempts
//		{{ .health.failedReconciles }}   — int64: total failed reconciles
//		{{ .health.consecutiveFails }}   — int64: current consecutive failure streak
//		{{ .health.errorRatePercent }}   — float64: error rate as percentage
//		{{ .health.lastReconcile }}      — string: RFC3339 timestamp of last reconcile
//		{{ .health.uptime }}             — string: how long the reconciler has been running
//		{{ .health.lastError }}          — string: most recent error message (empty if none)
//		{{ .health.rollbackActive }}     — bool: rollback is currently blocking reconcile
//		{{ .health.rollbackTotal }}      — int64: total rollback triggers since startup
//		{{ .health.hasUnhealthyDeps }}   — bool: any dependency is unsatisfied
func (h *CRDHealth) HealthAsMap() map[string]interface{} {
	if h == nil {
		return map[string]interface{}{}
	}

	state, status := h.StateAndStatus()

	return map[string]interface{}{
		"healthy":          h.IsHealthy(),
		"state":            state,
		"status":           status,
		"started":          h.started.Load(),
		"pending":          h.pending.Load(),
		"degraded":         h.degraded.Load(),
		"totalReconciles":  h.totalReconciles.Load(),
		"failedReconciles": h.failedReconciles.Load(),
		"consecutiveFails": h.consecutiveFails.Load(),
		"errorRatePercent": h.ErrorRatePercent(),
		"lastReconcile":    h.LastReconcile(),
		"uptime":           h.Uptime(),
		"lastError":        h.LastError(),
		"rollbackActive":   h.rollbackActive.Load(),
		"rollbackTotal":    h.rollbackTotal.Load(),
		"hasUnhealthyDeps": h.hasUnhealthyDeps.Load(),
	}
}
