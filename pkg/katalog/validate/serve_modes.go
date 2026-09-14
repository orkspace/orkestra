package validate

import "fmt"

// validateServeModes validates the serve.modes configuration for all enabled CRDs.
//
// It enforces two rules:
//  1. At least one mode (target or CR) must be enabled — a CRD cannot have both modes disabled.
//  2. If target mode is disabled, the CRD must not declare a target — a target is only meaningful
//     when target mode is enabled.
//
// Both modes default to true when omitted, preserving backward compatibility with
// existing Katalogs. This validation ensures the API surface is coherent and
// that the platform team's intent is reflected in the configuration.
func (e *executor) validateServeModes() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		if !crd.ServeEnabled() {
			continue
		}

		// Rule 1: At least one mode must be enabled
		if !crd.TargetModeEnabled() && !crd.FullCRModeEnabled() {
			return errServeModesBothDisabled(crdName)
		}

		// Rule 2: Target mode disabled → target must not be set
		if !crd.TargetModeEnabled() && !crd.Serve.Target.IsZero() {
			return errServeTargetModeDisabledWithTarget(crdName)

		}
	}
	return nil
}

// ── error helpers ────────────────────────────────────────────────────────────

func errServeModesBothDisabled(crd string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s CRD %q: at least one of serve.modes.target or serve.modes.cr must be enabled

Both modes are disabled — this CRD would be unreachable via the Gateway API.
Callers would have no way to create or update resources of this type.

Enable at least one mode:
  serve:
    enabled: true
    modes:
      target: true   # callers submit fields with a target
      # or
      cr: true       # callers submit full Kubernetes CRs
──────────────────────────────────────────────`, failureMark(), crd)
}

func errServeTargetModeDisabledWithTarget(crd string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s CRD %q: serve.modes.target is false but serve.target is set

Target mode is disabled, but this CRD still declares a target.
A target is only meaningful when target mode is enabled.

Either:
  • Enable target mode:
      serve:
        modes:
          target: true

  • Or remove the target
      serve:
        target: ""   # or omit the field entirely
──────────────────────────────────────────────`, failureMark(), crd)
}
