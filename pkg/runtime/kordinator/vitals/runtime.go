package vitals

import (
	"sync"
	"sync/atomic"
)

type RuntimeHealth struct {
	name        string
	orkReady    atomic.Bool
	katReady    atomic.Bool
	allOnline   atomic.Bool // For this katalog
	isKonductor atomic.Bool // true only on the pod that won the leader election
	mu          sync.RWMutex
}

// SetIsKonductor marks whether this pod holds the leader election lease.
// Set to true at the start of Kordinate(), false when leadership is lost.
func (h *RuntimeHealth) SetIsKonductor(v bool) {
	h.isKonductor.Store(v)
}

// IsKonductor reports whether this pod is the current konductor (leader).
// The control center uses this to decide whether to trust this pod's CRD data.
func (h *RuntimeHealth) IsKonductor() bool {
	return h.isKonductor.Load()
}

// SetOrkReady marks orkestra engine as ready
func (h *RuntimeHealth) SetOrkReady() {
	h.orkReady.Store(true)
}

// SetOrkDegraded marks orkestra engine as degraded
func (h *RuntimeHealth) SetOrkDegraded() {
	h.orkReady.Store(false)
}

// IsOrkReady is used to track ready state of orkestra
func (h *RuntimeHealth) IsOrkReady() bool {
	return h.orkReady.Load()
}

// SetKatalogReady marks a katalog as ready
func (h *RuntimeHealth) SetKatalogReady() {
	h.katReady.Store(true)
}

func (h *RuntimeHealth) SetRuntimeReady(ready bool) {
	h.orkReady.Store(ready)
}

func (h *RuntimeHealth) SetAllOnline() {
	h.allOnline.Store(true)
}

func (h *RuntimeHealth) SetAllNotOnline() {
	h.allOnline.Store(false)
}

func (h *RuntimeHealth) SetReconciling(isKonductor bool) {
	h.isKonductor.Store(isKonductor)
}

// SetKatalogDegraded marks a katalog as degraded
func (h *RuntimeHealth) SetKatalogDegraded() {
	h.katReady.Store(false)
}

// IsKatalogReady is used to track ready state of a katalog
func (h *RuntimeHealth) IsKatalogReady() bool {
	return h.katReady.Load()
}
