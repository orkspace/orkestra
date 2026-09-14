package vitals

import "github.com/orkspace/orkestra/pkg/metrics"

// Worker state constants
const (
	WorkerStateIdle       = "idle"
	WorkerStateProcessing = "processing"
	WorkerStateStopped    = "stopped"
)

// SetTotalWorkers sets the expected number of workers
func (h *CRDHealth) SetTotalWorkers(count int32) {
	h.totalWorkers.Store(count)
	// When total workers is set, idle workers should be total (since all are idle initially)
	h.idleWorkers.Store(count)
	h.processingWorkers.Store(0)

	if h.gvk != "" {
		metrics.SetWorkersTotal(h.gvk, float64(count))
		metrics.SetWorkersIdle(h.gvk, float64(count))
		metrics.SetWorkersProcessing(h.gvk, 0)
	}
}

// GetTotalWorkers returns the expected total workers
func (h *CRDHealth) GetTotalWorkers() int32 {
	return h.totalWorkers.Load()
}

// MarkWorkerProcessing is called when a worker starts processing an item
func (h *CRDHealth) MarkWorkerProcessing(workerID string) {
	// Increment processing, decrement idle
	newProcessing := h.processingWorkers.Add(1)
	newIdle := h.idleWorkers.Add(-1)
	h.workerStates.Store(workerID, WorkerStateProcessing)

	// Update metrics
	if h.gvk != "" {
		metrics.SetWorkersProcessing(h.gvk, float64(newProcessing))
		metrics.SetWorkersIdle(h.gvk, float64(newIdle))
	}
}

// MarkWorkerIdle is called when a worker finishes processing
func (h *CRDHealth) MarkWorkerIdle(workerID string) {
	// Decrement processing, increment idle
	newProcessing := h.processingWorkers.Add(-1)
	newIdle := h.idleWorkers.Add(1)
	h.workerStates.Store(workerID, WorkerStateIdle)

	// Update metrics
	if h.gvk != "" {
		metrics.SetWorkersProcessing(h.gvk, float64(newProcessing))
		metrics.SetWorkersIdle(h.gvk, float64(newIdle))
	}
}

// GetProcessingWorkers returns number of workers currently reconciling
func (h *CRDHealth) GetProcessingWorkers() int32 {
	return h.processingWorkers.Load()
}

// GetIdleWorkers returns number of workers waiting for work
func (h *CRDHealth) GetIdleWorkers() int32 {
	return h.idleWorkers.Load()
}

// GetActiveWorkers returns total workers (all are "active" if running)
func (h *CRDHealth) GetActiveWorkers() int32 {
	return h.GetTotalWorkers()
}

// GetWorkerStates returns a map of worker states for debugging
func (h *CRDHealth) GetWorkerStates() map[string]string {
	states := make(map[string]string)
	h.workerStates.Range(func(key, value interface{}) bool {
		states[key.(string)] = value.(string)
		return true
	})
	return states
}

// ResetWorkerCounts resets all worker counters (used during deactivation)
func (h *CRDHealth) ResetWorkerCounts() {
	h.processingWorkers.Store(0)
	h.idleWorkers.Store(0)
	// Don't reset totalWorkers - it should stay as configured

	h.workerStates.Range(func(key, value interface{}) bool {
		h.workerStates.Store(key, WorkerStateStopped)
		return true
	})

	if h.gvk != "" {
		metrics.SetWorkersProcessing(h.gvk, 0)
		metrics.SetWorkersIdle(h.gvk, 0)
	}
}

// MarkWorkersStopped transitions every known worker to the stopped state.
// Unlike ResetWorkerCounts, it does not touch processing/idle counters or totalWorkers.
func (h *CRDHealth) MarkWorkersStopped() {
	h.workerStates.Range(func(key, _ any) bool {
		h.workerStates.Store(key, WorkerStateStopped)
		return true
	})
}

// MarkStartupWorkerIdle marks a worker idle on startup
func (h *CRDHealth) MarkStartupWorkerIdle(key string) {
	h.workerStates.Store(key, WorkerStateStopped)
}
