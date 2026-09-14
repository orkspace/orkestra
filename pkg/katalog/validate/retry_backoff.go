package validate

import (
	"fmt"
	"time"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateRetryBackoff checks that declared retryBackoff configurations are
// internally valid and warns when the worst-case retry window exceeds the
// effective resync period.
//
// Why warn rather than error: a long retry window is not always wrong — an
// external: block retrying a slow API and a short resync window can coexist
// (the queue re-enqueues on the next resync anyway). The warning surfaces the
// math so the user can make an informed choice.
func (e *executor) validateRetryBackoff() error {
	for name, crd := range e.k.EnabledCRDs() {
		changed := false
		rec := crd.OperatorBox.Reconciler
		resync := effectiveResync(rec)

		// queue.retryBackoff — applies to the reconcile loop as a whole.
		if rec.HasRetryBackoff() {
			rb := rec.Queue.RetryBackoff
			if err := validateBackoffConfig("queue.retryBackoff", rb); err != nil {
				return fmt.Errorf("%s CRD %q: %w", failureMark(), name, err)
			}
			if resync > 0 {
				if wc := rb.WorstCaseDuration(); wc > resync {
					crd.Warnings.AddWarning(fmt.Sprintf(
						"queue.retryBackoff worst-case delay (%s) exceeds resync (%s) — "+
							"the queue will re-enqueue before retries finish; "+
							"consider reducing maxAttempts or initial delay",
						wc.Round(time.Millisecond), resync.Round(time.Millisecond),
					))
					changed = true
				}
			}
		}

		// queue.behaviour.onLimit.retryBackoff — applies per queue item on limit.

		// external[].retryBackoff — applies per external call.
		for _, phase := range allExternalPhases(crd) {
			for i, ext := range phase {
				if ext.RetryBackoff == nil {
					continue
				}
				label := fmt.Sprintf("external[%d] %q retryBackoff", i, ext.Name)
				if err := validateBackoffConfig(label, ext.RetryBackoff); err != nil {
					return fmt.Errorf("%s CRD %q: %w", failureMark(), name, err)
				}
				if resync > 0 {
					if wc := ext.RetryBackoff.WorstCaseDuration(); wc > resync {
						crd.Warnings.AddWarning(fmt.Sprintf(
							"external[%d] %q: retryBackoff worst-case delay (%s) exceeds resync (%s) — "+
								"the reconcile will block longer than the resync window for this call",
							i, ext.Name,
							wc.Round(time.Millisecond), resync.Round(time.Millisecond),
						))
						changed = true
					}
				}
			}
		}

		if changed {
			e.k.EnabledCRDs()[name] = crd
		}
	}
	return nil
}

// validateBackoffConfig returns a hard error for structurally invalid values.
func validateBackoffConfig(label string, rb *orktypes.RetryBackoffConfig) error {
	if rb.Multiplier < 0 {
		return fmt.Errorf("%s %s: multiplier must be >= 0, got %g", failureMark(), label, rb.Multiplier)
	}
	if rb.MaxAttempts < 0 {
		return fmt.Errorf("%s %s: maxAttempts must be >= 0, got %d", failureMark(), label, rb.MaxAttempts)
	}
	opts := rb.ToRetryDoOptions()
	opts.ApplyDefaults()
	if opts.Max < opts.Base {
		return fmt.Errorf("%s %s: max (%s) must be >= initial (%s)", failureMark(), label, opts.Max, opts.Base)
	}
	return nil
}

// effectiveResync returns the resolved resync duration for this CRD's reconciler.
// By the time validation runs, enrichCRDs has already applied global defaults,
// so Reconciler.Resync.Duration is the effective value.
func effectiveResync(rec *orktypes.ReconcilerConfig) time.Duration {
	if rec == nil {
		return 0
	}
	return rec.Resync.Duration
}

// allExternalPhases collects all external call lists from onReconcile, onCreate,
// onDelete, and the preReconcile enqueueGate/reconcileGate.
func allExternalPhases(crd orktypes.CRDEntry) [][]orktypes.ExternalCallSpec {
	box := &crd.OperatorBox
	phases := [][]orktypes.ExternalCallSpec{
		box.OnReconcile.ExternalCalls(),
		box.OnCreate.ExternalCalls(),
		box.OnDelete.ExternalCalls(),
	}
	phases = append(phases, box.PreReconcile.GateExternalCalls()...)
	return phases
}
