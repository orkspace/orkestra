// Package vitals holds the live health state of the Orkestra runtime.
//
// It tracks two things:
//
//   - CRDHealth: the lifecycle state of each CRD as the kordinator
//     processes it — pending, started, healthy, degraded — along with
//     the counters, worker states, and dependency status that feed the
//     Control Center and the /katalog health endpoints.
//
//   - RuntimeHealth: the runtimes's own readiness — engine ready,
//     Katalog loaded, all CRDs online, and whether this pod is the
//     current konductor (leader).
//
// Both are state only. This package does not serve HTTP, register
// routes, or decide when to reconcile. The handlers that expose this
// state, and the kordinator logic that drives the transitions, live
// one level up in pkg/runtime/kordinator.
package vitals

import (
	"github.com/orkspace/orkestra/pkg/konfig"
)

// NewCRDHealth initializes a CRDHealth tracker for a given CRD name.
// The reconciler starts in an "unhealthy" state until the first successful reconcile.
func NewCRDHealth(name string) *CRDHealth {
	h := &CRDHealth{name: name}
	h.healthy.Store(false)
	h.pending.Store(true)
	h.degraded.Store(false)

	// Add katalog tracker
	h.orkHealth = NewRuntimeHealth()
	return h
}

// NewRuntimeHealth initializes a CRDHealth tracker for Orkestra
func NewRuntimeHealth() *RuntimeHealth {
	h := &RuntimeHealth{name: konfig.Ork}
	h.orkReady.Store(true)
	h.katReady.Store(false)
	return h
}
