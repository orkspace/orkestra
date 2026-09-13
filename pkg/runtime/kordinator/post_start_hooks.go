package kordinator

import (
	"context"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/informer"
	"github.com/orkspace/orkestra/pkg/runtime/queue"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// retryMissingCRDs runs continuously to detect and activate CRDs that were missing at startup
// or deferred because dependencies were not ready.
func (k *DependencyKordinator) retryMissingCRDs(ctx context.Context) {
	retryInterval := postStartRetryInterval
	if !utils.IsRunningInCluster() {
		retryInterval = postStartRetryIntervalDev
	}
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	backoff := PostStartBackoff
	nameToGVK := k.NameToGVKMap()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			runtimeMissing := make(map[string]*informer.InformerEntry)

			// ── Custom child CRDs ────────────────────────────────────────────
			// Check GVKs declared in onCreate.custom / onReconcile.custom blocks.
			// These CRDs must exist for the reconciler to create instances via the
			// dynamic client. Failures are silent by default; this makes them visible.
			if len(k.missingChildGVKs) > 0 {
				stillMissingChild := make(map[string]schema.GroupVersionKind)
				for gvkStr, gvk := range k.missingChildGVKs {
					gvkCopy := gvk
					ok, err := k.crdExists(&gvkCopy)
					if err != nil {
						logger.Warn().Str("gvk", gvkStr).Err(err).Msg("crdExists transient error — keeping child in missing list")
						stillMissingChild[gvkStr] = gvk
						continue
					}
					if ok {
						logger.Info().Str("gvk", gvkStr).Msg("custom child CRD now available — refreshing RESTMapper")
						k.kube.RefreshMapper()
					} else {
						logger.Warn().Str("gvk", gvkStr).Msg("custom child CRD still not available — retrying")
						stillMissingChild[gvkStr] = gvk
					}
				}
				k.missingChildGVKs = stillMissingChild
			}

			// ───────────────────────────────────────────────
			// 1. Detect CRDs that disappeared at runtime
			// ───────────────────────────────────────────────
			registered := k.informerFactory.Registered()
			for gvkStr, entry := range registered {
				if entry == nil || entry.Missing {
					continue
				}
				ok, err := k.crdExists(entry.GVK)
				if err != nil {
					// Transient API server error — cannot determine CRD state.
					// Skip all health changes; we will re-check on the next tick.
					logger.Warn().Str("gvk", gvkStr).Err(err).Msg("crdExists transient error — skipping health update")
					continue
				}
				if !ok {
					logger.Warn().Str("gvk", gvkStr).Msg("CRD disappeared at runtime — marking missing")
					entry.Missing = true
					runtimeMissing[gvkStr] = entry
					k.informerFactory.SetMissingOnStartup(runtimeMissing)

					// Health tracking
					// 	CRD
					if h := k.crdHealthMap[gvkStr]; h != nil {
						h.SetMissingAtRuntime()
						h.SetDegraded()
					}

					// 	Katalog
					k.allOnline.Store(false)
					k.orkHealth.SetAllNotOnline()
					k.orkHealth.SetKatalogDegraded()

					// Degrade dependents that require healthy condition
					deps := k.depGraph.GetDependents(entry.Name)
					for _, dep := range deps {
						crd := k.depGraph.GetNode(dep).CRD
						if crd.DependsOn[entry.Name].Condition == string(orktypes.DependencyConditionHealthy) {
							if h := k.crdHealthMap[crd.GVKString()]; h != nil {
								h.SetDegraded()
							}
						} else {
							logger.Info().Str("gvk", gvkStr).Msgf("dependency %s is unhealthy", entry.Name)
						}
					}

					if !k.deactivated[gvkStr] {
						if h := k.crdHealthMap[gvkStr]; h != nil {
							h.StartedAt()
						}
						logger.Info().Str("gvk", gvkStr).Msg("stopping workers")
						k.deactivateCRD(gvkStr)
					}
				}
			}

			// ───────────────────────────────────────────────
			// 2. Handle CRDs currently missing
			// ───────────────────────────────────────────────
			missing := k.informerFactory.Missing()
			if len(missing) == 0 {
				backoff = PostStartBackoff
				logger.Debug().Msg("retry loop: no missing CRDs")
				// Continue to Phase 3 before marking all online
			} else {
				backoff = PostStartBackoff
				logger.Debug().Msgf("retry loop: checking %d missing CRD(s)", len(missing))
				stillMissing := make(map[string]*informer.InformerEntry)

				for gvkStr, entry := range missing {
					ok, err := k.crdExists(entry.GVK)
					if err != nil {
						// Transient error — cannot determine whether CRD exists.
						// Keep it in the missing list and retry next tick without
						// touching health state (avoids "not started" flip on a
						// pending CRD caused by an API server blip).
						logger.Warn().Str("gvk", gvkStr).Err(err).Msg("crdExists transient error — keeping in missing list")
						stillMissing[gvkStr] = entry
						continue
					}
					if ok {
						k.activateCRD(ctx, entry)
						k.deactivated[gvkStr] = false
						if h := k.crdHealthMap[gvkStr]; h != nil {
							h.SetStarted()
						}
					} else {
						stillMissing[gvkStr] = entry
						if h := k.crdHealthMap[gvkStr]; h != nil {
							h.SetMissingAtRuntime()
							h.SetDegraded()
						}
						k.orkHealth.SetAllNotOnline()
						k.orkHealth.SetKatalogDegraded()
						logger.Debug().Msgf("retry loop: %s still not available", gvkStr)
					}
				}
				k.informerFactory.SetMissingOnStartup(stillMissing)

				if len(stillMissing) > 0 {
					logger.Info().Msgf("retry loop: %d CRD(s) still missing", len(stillMissing))
					k.allOnline.Store(false)
					k.orkHealth.SetAllNotOnline()
					k.orkHealth.SetKatalogDegraded()
				}
			}

			// ───────────────────────────────────────────────────────────
			// 3. Activate deferred CRDs (skipped at startup because
			//    dependencies were not ready)
			// ───────────────────────────────────────────────────────────
			for _, name := range k.depGraph.StartupOrder() {
				gvk := nameToGVK[name]

				k.mu.RLock()
				started := k.started[gvk]
				k.mu.RUnlock()
				if started {
					continue // already running
				}

				// Skip if it's already in the missing map (handled by Phase 2)
				if k.informerFactory.IsMissing(gvk) {
					continue
				}

				crd := k.depGraph.GetNode(name).CRD
				if !k.dependenciesReady(crd, nameToGVK) {
					continue // still waiting for dependencies
				}

				// CRD exists and dependencies are ready → activate
				logger.Info().Str("crd", name).Msg("dependencies now satisfied, activating")
				entry, ok := k.informerFactory.Registered()[gvk]
				if !ok || entry == nil {
					logger.Error().Str("crd", name).Str("gvk", gvk).Msg("no informer entry found for deferred CRD")
					continue
				}
				k.activateCRD(ctx, entry)
				k.deactivated[gvk] = false
				k.crdHealthMap[gvk].SetStarted()
			}

			// ───────────────────────────────────────────────
			// 4. Update overall health
			// ───────────────────────────────────────────────
			if len(k.informerFactory.Missing()) == 0 && k.allCRDsStarted() {
				k.allOnline.Store(true)
				k.orkHealth.SetAllOnline()
				k.orkHealth.SetKatalogReady()
				logger.Debug().Msg("retry loop: all CRDs active")
			}

			// Exponential backoff only when CRDs are still missing
			if len(k.informerFactory.Missing()) > 0 {
				time.Sleep(backoff)
				if backoff < PostStartBackoffMax {
					backoff *= 2
					if backoff > PostStartBackoffMax {
						backoff = PostStartBackoffMax
					}
				}
			}
		}
	}
}

// allCRDsStarted returns true if every CRD in the graph has started.
func (k *DependencyKordinator) allCRDsStarted() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	for _, node := range k.depGraph.GetNodes() {
		gvk := node.CRD.GroupVersionKind.String()
		if !k.started[gvk] {
			return false
		}
	}
	return true
}

// activateCRD brings a previously missing CRD online after it appears in the cluster.
//
// The informer for this CRD was already created during factory initialization
// but was never started because the CRD did not exist at startup.
// This method starts the existing informer, launches its worker pool, and signals
// any dependents that this CRD is now ready.
//
// This method does NOT recreate the informer — it reuses the one created during
// factory setup, which avoids the "sharedIndexInformer started twice" warning.
//
// Flow:
//  1. Start the existing (but not yet running) informer
//  2. Remove from missing map so it won't be retried again
//  3. Start worker goroutines for this CRD
//  4. Update health tracking to reflect started state
//  5. Close the ready channel to unblock any CRDs that depend on this one
//
// Parameters:
//   - ctx: context for cancellation propagation
//   - entry: the informer entry from the missing map (contains the pre-created informer)
func (k *DependencyKordinator) activateCRD(ctx context.Context, entry *informer.InformerEntry) {
	gvkStr := entry.GVK.String()
	name := entry.Name

	logger.Info().Msgf("activating CRD %s (%s)", name, gvkStr)

	// Safety check — the informer should exist because it was created during factory init
	if entry.Informer == nil {
		logger.Error().Msgf("activateCRD: informer for %s is nil", gvkStr)
		return
	}

	// Start the existing informer (it was created but not started at startup)
	// Only start informer if it was never started (startup-missing case).
	if entry.WasNeverStarted { // you can track this with a bool on InformerEntry
		go entry.Informer.Run(ctx.Done())
		entry.WasNeverStarted = false
		logger.Info().Msgf("activateCRD: informer started for %s", name)
	}

	// Mark as no longer missing so the retry loop won't keep processing it
	entry.Missing = false
	k.informerFactory.RemoveMissing(gvkStr)

	// Start the worker pool for this CRD
	workers := k.katalog.GetWorkers(gvkStr, k.defaultWorkers)
	k.startCRDWorkers(ctx, gvkStr, workers)

	// Update health tracking
	k.deactivated[gvkStr] = false
	k.crdHealthMap[gvkStr].SetStarted()
	k.crdHealthMap[gvkStr].SetTotalWorkers(int32(workers))

	// Signal dependents that this CRD has now started
	// The started channel was created during Kordinate and may still be open
	if ch, exists := k.startedCh[gvkStr]; exists {
		select {
		case <-ch:
			// Channel already closed (should not happen, but safe)
			logger.Debug().Msgf("activateCRD: started channel for %q was already closed", name)
		default:
			close(ch)
			k.crdHealthMap[gvkStr].SetStarted()
			deps := k.depGraph.GetDependents(name)
			if len(deps) > 0 {
				logger.Info().Msgf("activateCRD: closed started channel for %q — unblocking %d dependent(s): %s",
					name, len(deps), strings.Join(deps, ", "))
			}
		}
	}

	// Check dependencies whose dependency condition is 'healthy' and leave healthy channel open
	deps := k.depGraph.GetDependents(name)
	if len(deps) > 0 {
		for _, dep := range deps {
			crd := k.NameToCRD(dep)
			if crd.DependsOn.ConditionHealthy(name) {
				if ch, exists := k.healthyCh[crd.GVKString()]; exists {
					select {
					case <-ch:
						// Channel already closed (should not happen, but safe)
						logger.Debug().Msgf("activateCRD: healthy channel for %q was already closed", name)
					default:
						if !k.crdHealthMap[crd.GVKString()].IsHealthy() {
							continue
						}
						close(ch)
						k.crdHealthMap[crd.GVKString()].SetStarted()
						logger.Info().Msgf("activateCRD: closed healthy channel for %q", name)
					}
				}
			}
		}
	}

	logger.Info().Msgf("CRD %s activated", name)
}

// deactivateCRD — drain without permanent shutdown
func (k *DependencyKordinator) deactivateCRD(gvk string) {
	k.mu.RLock()
	cancel, okCancel := k.cancelFuncs[gvk]
	wg, okWG := k.wgs[gvk]
	k.mu.RUnlock()

	if okCancel {
		cancel() // signals workers via context
	}

	// Add a single sentinel item to each worker's queue to unblock
	// any worker currently waiting in GetWithContext.
	// Workers check ctx.Done() after dequeuing — sentinel is dropped.
	if wq, ok := k.queueReg.For(gvk); ok {
		numWorkers := k.katalog.GetWorkers(gvk, k.defaultWorkers)
		for i := 0; i < numWorkers; i++ {
			wq.Add(queue.QueueItem{GVK: gvk, Key: drainSentinel})
		}
	}

	if !okWG {
		return
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		k.crdHealthMap[gvk].ResetWorkerCounts()
		k.crdHealthMap[gvk].MarkWorkersStopped()
		// k.crdHealthMap[gvk].workerStates.Range(func(key, value interface{}) bool {
		// 	k.crdHealthMap[gvk].workerStates.Store(key, crdhealth.WorkerStateStopped)
		// 	return true
		// })

		k.deactivated[gvk] = true
		logger.Info().Str("gvk", gvk).Msg("workers drained cleanly")
	case <-time.After(drainTimeout):
		logger.Warn().Str("gvk", gvk).Msg("drain timeout exceeded")
	}
}

// crdExists checks if a CRD is present in the cluster by querying the API server.
//
// Return values:
//   - (true,  nil) — CRD exists
//   - (false, nil) — CRD definitively not registered ("not installed")
//   - (false, err) — transient failure (network blip, timeout, dial error);
//     callers must skip all health-state changes and retry next tick
func (k *DependencyKordinator) crdExists(gvk *schema.GroupVersionKind) (bool, error) {
	err := utils.WaitForCRD(
		k.kube.RestConfig(),
		gvk.Group,
		gvk.Kind,
		gvk.Version,
	)
	if err == nil {
		return true, nil
	}
	// WaitForCRD converts meta.IsNoMatchError into "not installed" — that is the
	// only definitive "CRD absent" signal. Everything else (dial, timeout, TLS, …)
	// is a transient error; don't degrade CRD health based on it.
	if strings.Contains(err.Error(), "not installed") {
		return false, nil
	}
	return false, err
}

// collectCustomChildGVKs scans all registered CRDs for custom resource entries declared
// in onCreate.custom / onReconcile.custom and returns a de-duped map of GVK string → GVK.
func collectCustomChildGVKs(katalog *ResourceKatalog) map[string]schema.GroupVersionKind {
	result := make(map[string]schema.GroupVersionKind)
	for _, entry := range katalog.Entries() {
		box := entry.CRD.OperatorBox
		var srcs []orktypes.CustomResourceTemplateSource
		if box.OnCreate != nil {
			srcs = append(srcs, box.OnCreate.CustomResource...)
		}
		if box.OnReconcile != nil {
			srcs = append(srcs, box.OnReconcile.CustomResource...)
		}
		for i := range srcs {
			gvk, err := srcs[i].BuildGVK()
			if err != nil {
				continue
			}
			result[gvk.String()] = gvk
		}
	}
	return result
}
