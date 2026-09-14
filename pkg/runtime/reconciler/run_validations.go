// pkg/reconciler/run_validation.go
package reconciler

import (
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ── Prometheus metrics ────────────────────────────────────────────────────────
// Four metrics — all meaningful, all unique to Orkestra.
// Nothing kubectl or Prometheus itself shows about per-CRD validation.

var (
	// validationTotal counts every validation pass or rejection across all CRDs.
	// Labeled by CRD and result (passed|rejected).
	// Use: alert when rejection rate exceeds a threshold.
	validationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "controller_validation_total",
			Help: "Total number of validation checks performed, labeled by result.",
		},
		[]string{"crd", "result"},
	)

	// validationRejectedDetail counts rejections with field and rule context.
	// More expensive than validationTotal but gives actionable detail:
	// which field is failing and which rule is triggering it.
	// Use: alert + investigate which rule is causing the most friction.
	validationRejectedDetail = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "controller_validation_rejected_total",
			Help: "Validation rejections labeled by CRD, field, and rule type.",
		},
		[]string{"crd", "field", "rule"},
	)
)

// ValidationResult holds the outcome of running all validation rules for one reconcile.
type ValidationResult struct {
	// Passed — true when all rules passed
	Passed bool

	// Deny
	Deny bool

	// Violations — one entry per failed rule
	// Warning
	Warnings []ValidationViolation // action: warn violations

	Violations []ValidationViolation // action: deny violations
}

// ValidationViolation describes one failed validation rule.
type ValidationViolation struct {
	Field   string
	Rule    string
	Value   string                    // the actual CR field value that failed
	Message string                    // the user-defined message from the Katalog
	Action  orktypes.ValidationAction // 'warn' or 'deny' as defined by user
}

// Error returns a combined error message for all violations.
func (r *ValidationResult) Error() error {
	if r.Passed {
		return nil
	}
	msgs := make([]string, 0, len(r.Violations))
	for _, v := range r.Violations {
		msgs = append(msgs, fmt.Sprintf("field %q: %s (got %q)", v.Field, v.Message, v.Value))
	}
	return fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
}

// runValidation evaluates all validation rules against the CR object.
// All rules are evaluated — multiple violations are collected and reported together.
// Returns a ValidationResult. The caller decides whether to halt reconciliation.
//
// data is resolver.Data() — the full CR map, works for both typed and unstructured.
// resolver is optional — when non-nil, template expressions in comparison values
// and messages are resolved against the full resolver context (notes, profiles, etc.).
// operator: unique rules check against a UniquenessChecker injected into data
// via resolver.WithUniquenessChecker — see orktypes.UniquenessChecker; they
// always pass when none was injected.
// Called from generic.go before runTemplateReconcile (or after runMutation
// when mutateFirst: true).
func runValidation(data map[string]interface{}, resolver *orktmpl.Resolver, cfg *orktypes.ValidationConfig, crdName string) *ValidationResult {
	result := &ValidationResult{Passed: true}
	if cfg == nil || len(cfg.Rules) == 0 {
		return result
	}

	for _, rule := range cfg.Rules {
		if !rule.Fires.FiresAtReconcile() {
			continue
		}
		var eval orktypes.TemplateEvaluator
		// resolver is a *orktmpl.Resolver — nil-check before converting to
		// the orktypes.TemplateResolver interface, since a typed nil
		// wrapped in an interface value is not itself a nil interface.
		var tr orktypes.TemplateResolver
		if resolver != nil {
			eval = resolver.TemplateEvaluator()
			tr = resolver
		}
		if !orktypes.EvaluateConditions(data, rule.When, rule.Or, eval) {
			continue
		}
		ruleViolation := orktypes.EvaluateValidationRule(data, tr, rule)
		if ruleViolation == nil {
			continue
		}

		action := orktypes.EffectiveAction(rule.Action)
		violation := ValidationViolation{
			Field:   ruleViolation.Field,
			Rule:    ruleViolation.Rule,
			Value:   ruleViolation.Value,
			Message: ruleViolation.Message,
			Action:  action,
		}

		result.Violations = append(result.Violations, violation)
		validationRejectedDetail.WithLabelValues(crdName, rule.Field, orktypes.RuleTypeLabel(rule)).Inc()

		switch action {
		case orktypes.ValidationActionDeny:
			result.Deny = true
			result.Passed = false
		case orktypes.ValidationActionWarn:
			result.Warnings = append(result.Warnings, violation)
			// Warn does NOT set Deny, does NOT set Passed=false
		}
	}

	resultLabel := "passed"
	if !result.Passed {
		resultLabel = "rejected"
	} else if len(result.Warnings) > 0 {
		resultLabel = "warned"
	}
	validationTotal.WithLabelValues(crdName, resultLabel).Inc()

	if result.Deny {
		logger.Info().
			Str("crd", crdName).
			Int("violations", len(result.Violations)).
			Msg("validation: rules failed — reconciliation halted")
	}

	return result
}

// ─────────────────────────────────────────────────────────────
// High‑level state checks
// ─────────────────────────────────────────────────────────────

// HasWarnings returns true if any advisory warnings were produced.
func (r *ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// Denied returns true if validation rules explicitly blocked reconciliation.
func (r *ValidationResult) Denied() bool {
	return r.Deny
}

// Blocked returns true if reconciliation must stop.
// Deny takes precedence; warnings alone do NOT block.
func (r *ValidationResult) Blocked() bool {
	return r.Deny
}

// ─────────────────────────────────────────────────────────────
// Error + message helpers
// ─────────────────────────────────────────────────────────────

// DenialError returns a proper error describing the denial reason.
func (r *ValidationResult) DenialError() error {
	if !r.Deny {
		return nil
	}
	return fmt.Errorf("validation denied: %s", r.DenialMessage())
}

// DenialMessage returns a human‑readable summary of the denial cause.
func (r *ValidationResult) DenialMessage() string {
	if !r.Deny {
		return ""
	}
	if len(r.Violations) == 0 {
		return "validation denied"
	}
	// First violation is usually the most relevant
	v := r.Violations[0]
	return fmt.Sprintf("field %q: %s (got %q)", v.Field, v.Message, v.Value)
}

// WarningSummary returns a semicolon‑joined list of warnings.
func (r *ValidationResult) WarningSummary() string {
	if len(r.Warnings) == 0 {
		return ""
	}
	msgs := make([]string, 0, len(r.Warnings))
	for _, w := range r.Warnings {
		msgs = append(msgs, fmt.Sprintf("field %q: %s", w.Field, w.Message))
	}
	return strings.Join(msgs, "; ")
}

// HasViolations returns true if any rules failed.
func (r *ValidationResult) HasViolations() bool {
	return len(r.Violations) > 0
}

// ViolationSummary returns a semicolon‑joined list of violations.
func (r *ValidationResult) ViolationSummary() string {
	if len(r.Violations) == 0 {
		return ""
	}
	out := make([]string, 0, len(r.Violations))
	for _, v := range r.Violations {
		out = append(out, fmt.Sprintf("%s: %s", v.Field, v.Message))
	}
	return strings.Join(out, "; ")
}

// Log helper for debugging
func (r *ValidationResult) Log(crd, name, ns string) {
	if r.Passed {
		return
	}
	logger.Info().
		Str("crd", crd).
		Str("name", name).
		Str("namespace", ns).
		Int("violations", len(r.Violations)).
		Msg("validation failed")
}
