package validate

import (
	"fmt"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateServeTarget is called from the main Validate() chain after the
// existing Serve checks. It catches:
//
//  1. Invalid target format — targets must be lowercase alphanumeric + hyphens.
//     The gateway uses the target as a URL path segment; anything else is unsafe.
//
//  2. Duplicate targets — two CRDs that resolve to the same target string would
//     make lookups ambiguous. Most commonly caused by two CRDs with the same
//     lowercased kind when serve.target is not set explicitly on one of them.
//
//  3. Reconciler-level fields on per-target operatorBox — workers, resync, queue,
//     autoscale, rollback are fixed at CRD level and ignored at runtime if declared
//     on a target entry. Reject early to make the mistake visible.
//
//     if err := e.validateServeTarget(); err != nil { return err }
func (e *executor) validateServeTarget() error {
	// Track seen targets → CRD name for duplicate detection.
	seen := make(map[string]string)

	for crdName, crd := range e.k.EnabledCRDs() {
		if !crd.HasServeTarget() {
			continue
		}

		target := crd.ServeTarget()

		// 1. Format — lowercase alphanumeric + hyphens only.
		if !isValidServeTarget(target) {
			return fmt.Errorf(
				"%s crd %q: target %q is invalid — must be lowercase alphanumeric with "+
					"optional hyphens (a-z, 0-9, -)\n"+
					"  Set serve.target explicitly to a valid value.",
				failureMark(), crdName, target,
			)
		}

		// 2. Uniqueness — two CRDs cannot share a target.
		if first, clash := seen[target]; clash {
			return fmt.Errorf(
				"%s crd %q and %q both resolve to target %q\n"+
					"  Set serve.target explicitly on one or both to make them unique.",
				failureMark(), first, crdName, target,
			)
		}
		seen[target] = crdName

		// 3. Per-target operatorBox may not declare reconciler-level settings.
		if crd.Serve != nil {
			for entryName, entry := range crd.Serve.Target.Entries {
				if entry.OperatorBox == nil {
					continue
				}
				if err := validateTargetOperatorBox(crdName, entryName, entry.OperatorBox); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// validateTargetOperatorBox rejects fields that configure the reconciler worker
// pool — these are fixed at CRD level and have no effect per target.
func validateTargetOperatorBox(crdName, targetName string, box *orktypes.OperatorBoxConfig) error {
	var bad []string

	if box.Reconciler != nil {
		r := box.Reconciler
		if r.Workers != 0 {
			bad = append(bad, "reconciler.workers")
		}
		if r.Resync.Duration != 0 {
			bad = append(bad, "reconciler.resync")
		}
		if r.Queue != (orktypes.Queue{}) {
			bad = append(bad, "reconciler.queue")
		}
		if r.Profile != "" {
			bad = append(bad, "reconciler.profile")
		}
	}
	if box.Autoscale != nil {
		bad = append(bad, "autoscale")
	}
	if box.Rollback != nil {
		bad = append(bad, "rollback")
	}
	if box.RollBackOnError {
		bad = append(bad, "rollBackOnError")
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%s crd %q: serve.target.%s.operatorBox declares reconciler-level field(s): %s\n"+
			"  These settings govern the worker pool and are fixed at CRD level.\n"+
			"  Move them to the top-level operatorBox.",
		failureMark(), crdName, targetName, strings.Join(bad, ", "),
	)
}

// isValidServeTarget reports whether s is a valid target string.
// Valid: lowercase letters, digits, hyphens. Must be non-empty.
func isValidServeTarget(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}
