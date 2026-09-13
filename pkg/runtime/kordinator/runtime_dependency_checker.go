package kordinator

import (
	"context"
	"time"

	"github.com/orkspace/orkestra/pkg/runtime/kordinator/vitals"
	"github.com/orkspace/orkestra/pkg/types"
)

// dependencyHealthChecker runs continuously during the lifetime of the controller.
// It does NOT participate in startup dependency gating.
//
// Its responsibilities are:
//   - periodically evaluate the runtime health of all CRDs
//   - update dependency health status for Control Center and metrics
//   - signal healthyCh[gvk] exactly once when a CRD transitions to Healthy
//
// This checker is the *runtime* half of the dependency system.
// The *startup* half is handled inside Kordinate() using startedCh and healthyCh.
func (k *DependencyKordinator) dependencyHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(RuntimeHealthCheckInterval) // periodic runtime health evaluation
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			k.checkAllDependencyHealth()
		}
	}
}

// checkAllDependencyHealth iterates over all CRDs that have been started
// and updates their dependency health state.
//
// This is purely runtime observability. It does not block startup ordering.
func (k *DependencyKordinator) checkAllDependencyHealth() {
	k.mu.RLock()
	activeCRDs := make([]string, 0, len(k.crdHealthMap))
	for gvk := range k.crdHealthMap {
		activeCRDs = append(activeCRDs, gvk)
	}
	k.mu.RUnlock()

	for _, gvk := range activeCRDs {
		k.checkDependencyHealthForGVK(gvk)
	}
}

// checkDependencyHealthForGVK evaluates the health of all dependencies for a single CRD.
//
// It updates the CRD's dependency health map with:
//   - missing
//   - unknown
//   - pending
//   - started
//   - healthy
//   - degraded
//
// IMPORTANT:
//
//	If a dependency becomes Healthy for the first time, this method is responsible
//	for closing healthyCh[depGVK], which unblocks dependents that require "healthy".
//
// This is the integration point between runtime health and startup gating.
func (k *DependencyKordinator) checkDependencyHealthForGVK(gvk string) {
	entry, ok := k.katalog.Get(gvk)
	if !ok {
		return
	}

	health := k.crdHealthMap[gvk]
	if health == nil {
		return
	}

	crd := entry.CRD
	if len(crd.DependsOn) == 0 { // no dependencies to evaluate
		return
	}

	for depName, depCond := range crd.DependsOn {
		depGVK, exists := k.NameToGVKMap()[depName]
		if !exists {
			// Dependency declared but not present in graph
			health.SetDependencyHealth(depName, vitals.DependencyStatus{
				Name:      depName,
				State:     "missing",
				Condition: "started",
				Satisfied: false,
			})
			continue
		}

		// Dependency present in graph - health lookup with gvk
		depHealth := k.crdHealthMap[depGVK]
		if depHealth == nil {
			health.SetDependencyHealth(depName, vitals.DependencyStatus{
				Name:      depName,
				State:     "unknown",
				Condition: "started",
				Satisfied: false,
			})
			continue
		}

		// Determine runtime dependency state
		state := types.DependencyConditionDegraded
		acceptableCondition := depCond.Condition
		satisfied := false

		if depHealth.IsHealthy() {
			state = types.DependencyConditionHealthy

			// Signal healthyCh exactly once when dependency becomes healthy
			if !depHealth.SignaledHealthy() {
				close(k.healthyCh[depGVK])
				depHealth.MarkHealthySignaled()
				depHealth.SetStarted()
			}

		} else if depHealth.Started() && depCond.Condition == string(types.DependencyConditionStarted) {
			state = types.DependencyConditionStarted
		} else if depHealth.Pending() {
			state = types.DependencyCondtionPending
		} else {
			state = types.DependencyConditionDegraded
		}

		if acceptableCondition == string(state) || depHealth.IsHealthy() {
			satisfied = true
		}

		health.SetDependencyHealth(depName, vitals.DependencyStatus{
			Name:                depName,
			AcceptableCondition: acceptableCondition,
			State:               string(state),
			Condition:           string(state),
			Satisfied:           satisfied,
		})
	}
}
