package kordinator

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/metrics"
	"github.com/orkspace/orkestra/pkg/runtime/kordinator/vitals"
	"github.com/orkspace/orkestra/pkg/runtime/queue"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// queueDepthReporter is a local interface satisfied by GenericReconciler so the
// worker loop can push live queue depth into AutoMetrics for autoscale evaluation.
// Defined here (not imported from reconciler) to avoid import cycles.
type queueDepthReporter interface {
	ReportQueueDepth(depth int64)
}

// Worker that only processes items for a specific GVK
func (k *Kontroller) runWorkerForGVK(ctx context.Context, gvk string, workerID string) {
	wq, ok := k.queueRegistry.For(gvk)

	if !ok {
		logger.Warn().Str("gvk", gvk).Msg("no queue for CRD. Using default queue")
		wq = k.defaultWorkqueue
	}

	logger.Debug().Str("worker_id", workerID).Str("gvk", gvk).Msg("worker started")

	for {
		select {
		case <-ctx.Done():
			logger.Debug().Str("worker_id", workerID).Str("gvk", gvk).Msg("worker stopped")
			return
		default:
			// Worker is idle - waiting for work
			// (already idle from previous iteration or initialization)

			// Wait for an item
			item, shutdown := wq.Get()
			if shutdown {
				logger.Debug().Str("worker_id", workerID).Str("gvk", gvk).Msg("worker stopping (queue shutdown)")
				return
			}

			// Worker is now processing
			k.crdHealthMap[gvk].MarkWorkerProcessing(workerID)

			// Process the item
			func() {
				defer wq.Done(item)
				k.processItemForGVK(ctx, gvk, item)
			}()

			// Back to idle after processing
			k.crdHealthMap[gvk].MarkWorkerIdle(workerID)

			// Update metrics (outside of processing state)
			depth := float64(wq.Depth())
			metrics.SetQueueDepth(gvk, depth)

			// Push live depth into the reconciler's AutoMetrics so the autoscaler
			// can read it without an API call. No-op for non-autoscaled CRDs.
			k.mu.RLock()
			rec := k.reconcilers[gvk]
			k.mu.RUnlock()
			if reporter, ok := rec.(queueDepthReporter); ok {
				reporter.ReportQueueDepth(int64(wq.Depth()))
			}

			// Resource count — read from this CRD's informer cache
			if entry, ok := k.katalog.Get(gvk); ok && entry.Informer != nil {
				count := float64(len(entry.Informer.GetIndexer().List()))
				metrics.SetResourceCount(gvk, count)
			}
		}
	}
}

// processItemForGVK handles a single reconciliation item
func (k *Kontroller) processItemForGVK(ctx context.Context, gvk string, item queue.QueueItem) {
	if err := ctx.Err(); err != nil {
		logger.Debug().Msg("process item: context cancelled")
		return
	}

	// Resolve queue — per-CRD if registered, default otherwise
	wq, ok := k.queueRegistry.For(gvk)
	if !ok {
		wq = k.defaultWorkqueue
	}

	// Added to help shutdown workers and preserve the queue on missing crds
	// After dequeuing, they check ctx.Done() at the top of the loop and exit.
	// The queue is intact for reactivation. No ShutDown() called.
	// TODO: Currently does not shutdown the workers
	if item.Key == drainSentinel {
		wq.Forget(item)
		return
	}

	// With per-CRD queues this check is only needed for the default queue path
	// where multiple GVKs share one queue
	if item.GVK != gvk {
		if ok {
			// This item is in the wrong per-CRD queue — should not happen
			// Log and drop rather than spin
			logger.Error().
				Str("expected", gvk).
				Str("got", item.GVK).
				Msg("GVK mismatch in per-CRD queue — dropping item")
			wq.Forget(item)
		} else {
			// Default queue — item belongs to a different GVK, put it back
			// This is the only valid re-queue case
			wq.AddRateLimited(item)
		}
		return
	}

	// Look up the pre-built reconciler
	k.mu.RLock()
	rec := k.reconcilers[gvk]
	k.mu.RUnlock()

	if rec == nil {
		logger.Error().Str("gvk", gvk).Str("key", item.Key).Msg("no reconciler found — dropping item")
		wq.Forget(item)
		return
	}

	// Pre-reconcile gate: evaluate operatorBox.preReconcile.when/or conditions.
	// The reconciler is never called when conditions are not met — gated state
	// is idle, not failure; error rate and health state are unaffected.
	if entry, ok := k.katalog.Get(gvk); ok {
		if entry.CRD.HasAnyReconcileGate() {
			obj := k.objectFromCache(entry, item.Key)

			sentinelMap := wq.Sentinels(item)

			if gated, reason := k.evaluatePreReconcileCheck(ctx, obj, entry.CRD.Name, sentinelMap); gated {
				k.crdHealthMap[gvk].RecordGated(reason)
				wq.Forget(item)
				return
			}
		}
	}

	// safeReconcile catches panics
	result, err := k.safeReconcile(rec, k.crdHealthMap[gvk], ctx, item.Key, gvk)
	if err != nil {
		logger.Error().Err(err).Str("gvk", gvk).Str("key", item.Key).Msg("reconcile failed")
		wq.AddRateLimited(item)
		k.failedReconcile(gvk)
		return
	}

	wq.Forget(item)

	requeueAfter := result.RequeueAfter
	if requeueAfter == 0 {
		if entry, ok := k.katalog.Get(gvk); ok {
			obj := k.objectFromCache(entry, item.Key)
			requeueAfter = k.kat.EvaluateRequeue(ctx, entry.CRD.Name, obj)
		}
	}
	if requeueAfter > 0 {
		wq.AddAfter(item, requeueAfter)
	}
}

// safeReconcile wraps a Reconciler's Reconcile() call in a fully isolated,
// panic‑protected execution boundary. This is the core of Orkestra’s
// "operator sandbox" model: each CRD runs in its own safe compartment,
// ensuring that failures in one operator never cascade into others.
//
// Responsibilities:
//   - Measure reconcile duration for metrics
//   - Catch and convert panics into errors (preventing controller crash)
//   - Record success/failure into CRDHealth
//   - Emit success/error metrics
//   - Apply degrade thresholds for health tracking
//
// This function guarantees that no matter what happens inside rec.Reconcile(),
// the controller process stays alive and the failure is reported deterministically.
func (k *Kontroller) safeReconcile(
	rec domain.Reconciler,
	health *vitals.CRDHealth,
	ctx context.Context,
	key string,
	gvk string,
) (result domain.Result, err error) {

	// Track how long this reconcile took.
	// The defer ensures duration is recorded even if a panic occurs.
	start := time.Now()
	defer func() {
		metrics.ObserveReconcileDuration(gvk, time.Since(start).Seconds())

		// Panic recovery: this is the isolation boundary.
		// Any panic inside the operator is caught, logged, and converted into an error.
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)

			err = fmt.Errorf("reconciler panic: %v", r)

			logger.Error().
				Str("gvk", gvk).
				Str("key", key).
				Str("panic", fmt.Sprint(r)).
				Str("stack", string(buf[:n])).
				Msg("reconciler panic recovered")

			// Update CRD health state and metrics.
			health.RecordFailure(err, k.failureThreshold[gvk])
			metrics.RecordReconcile(gvk, "error")

			// TODO: track panic stats differently with recovery
		}
	}()

	// Inject per-request fields into the logr context so ctrl.LoggerFrom(ctx)
	// automatically carries the resource key on every reconciler log line.
	ctx = ctrllog.IntoContext(ctx, ctrllog.FromContext(ctx).WithValues("resource", key, "gvk", gvk))

	// Execute the operator's reconcile logic.
	// Any returned error is treated as a reconcile failure.
	ns, name, _ := cache.SplitMetaNamespaceKey(key)
	result, err = rec.Reconcile(ctx, domain.Request{
		Key:            key,
		NamespacedName: apitypes.NamespacedName{Namespace: ns, Name: name},
	})
	if err != nil {
		// Update CRD health state and metrics.
		health.RecordFailure(err, k.failureThreshold[gvk])
		metrics.RecordReconcile(gvk, "error")
		return result, err
	}

	// Successful reconcile path.
	health.RecordSuccess()
	k.successReconcile(gvk)
	metrics.RecordReconcile(gvk, "success")
	return result, nil
}
