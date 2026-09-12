package validate

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/orkspace/orkestra/pkg/runtime/sentinel"
	orktemplate "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateWatchEntries validates operatorBox.observe.watch and preReconcile.sentinels
// across all enabled CRDs.
//
// Enforces:
//  1. Each watch entry must declare apiVersion and kind.
//  2. Each on: value must be a known WatchEvent (create, update, delete).
//  3. No duplicate watch entries (same apiVersion + kind + namespace + name).
//  4. Each preReconcile.sentinels value must be a known Sentinel.
//  5. Gate templates (enqueueGate/reconcileGate) must compile with the
//     declared sentinel FuncMap — undeclared sentinels cause a parse error.
func (e *executor) validateWatchEntries() error {
	for crdName, crd := range e.k.Enabled() {
		if err := validateCRDWatchEntries(crdName, crd); err != nil {
			return err
		}
		if err := validateCRDSentinels(crdName, crd); err != nil {
			return err
		}
		if err := e.validatePreReconcileGateTemplates(crdName, crd); err != nil {
			return err
		}
		if err := e.validateGateFailPolicies(crdName); err != nil {
			return err
		}
	}
	return nil
}

func (e *executor) validateGateFailPolicies(crdName string) error {
	crd, ok := e.k.EnabledCRDs()[crdName]
	if !ok {
		return nil
	}
	pr := crd.OperatorBox.PreReconcile
	if pr == nil {
		return nil
	}
	type gateInfo struct {
		gate     *orktypes.GateConditions
		location string
	}
	changed := false
	for _, gi := range []gateInfo{
		{pr.EnqueueGate, "preReconcile.enqueueGate"},
		{pr.ReconcileGate, "preReconcile.reconcileGate"},
	} {
		g := gi.gate
		if g == nil {
			continue
		}
		if g.FailPolicy != "" && !orktypes.IsValidFailPolicy((g.FailPolicy).String()) {
			return fmt.Errorf("%s crd %q: %s.failPolicy %q is not valid. Valid values: %s",
				failureMark(), crdName, gi.location, g.FailPolicy, orktypes.FailPolicyJoined())
		}
		if len(g.ExternalCalls()) > 0 && g.FailPolicy == "" {
			crd.Warnings.AddWarning(fmt.Sprintf(
				"%s has external: calls but no failPolicy declared. "+
					"Default is open — evaluation failure will pass the gate. "+
					"Set failPolicy: closed if unknown state should hold back reconciliation.",
				gi.location))
			changed = true
		}
		if g.FailPolicy == orktypes.FailPolicyClosed && allContinueOnError(g.ExternalCalls()) {
			crd.Warnings.AddWarning(fmt.Sprintf(
				"%s has failPolicy: closed but all external: calls have continueOnError: true. "+
					"continueOnError suppresses call errors before they reach the gate — "+
					"failPolicy: closed will never trigger. "+
					"Use when: conditions on external.*.error to gate on suppressed failures instead.",
				gi.location))
			changed = true
		}
	}
	if changed {
		e.k.EnabledCRDs()[crdName] = crd
	}
	return nil
}

func allContinueOnError(calls []orktypes.ExternalCallSpec) bool {
	if len(calls) == 0 {
		return false
	}
	for _, c := range calls {
		if !c.ContinueOnError {
			return false
		}
	}
	return true
}

func validateCRDWatchEntries(crdName string, crd orktypes.CRDEntry) error {
	entries := crd.WatchEntries()
	if len(entries) == 0 {
		return nil
	}

	type key struct{ apiVersion, kind, namespace, name string }
	seen := make(map[key]bool, len(entries))

	for i, w := range entries {
		if w.APIVersion == "" {
			return fmt.Errorf("%s crd %q: watch[%d]: apiVersion must not be empty", failureMark(), crdName, i)
		}
		if w.Kind == "" {
			return fmt.Errorf("%s crd %q: watch[%d]: kind must not be empty", failureMark(), crdName, i)
		}

		if invalid := crd.OperatorBox.Observe.InvalidOnValues(w.On); len(invalid) > 0 {
			return fmt.Errorf("%s crd %q: watch[%d] %s/%s: unknown on: value(s) [%s] — valid values: %s",
				failureMark(), crdName, i, w.APIVersion, w.Kind,
				strings.Join(invalid, ", "), strings.Join(orktypes.ValidObserveEvents(), ", "))
		}

		idx := fmt.Sprintf("watch[%d]", i)
		if err := validateWatchKeyFrom(crdName, idx, w); err != nil {
			return err
		}

		k := key{w.APIVersion, w.Kind, w.Namespace, w.Name}
		if seen[k] {
			return fmt.Errorf("%s crd %q: duplicate watch entry %s/%s (namespace=%q name=%q) — watch entries must be unique",
				failureMark(), crdName, w.APIVersion, w.Kind, w.Namespace, w.Name)
		}
		seen[k] = true
	}
	return nil
}

func validateWatchKeyFrom(crdName string, location string, w orktypes.WatchEntry) error {
	kf := w.KeyFrom
	if kf == nil {
		return nil
	}
	hasLabel := kf.Label != ""
	hasName := kf.Name != ""
	if hasLabel && hasName {
		return fmt.Errorf("%s crd %q: watch[%s] %s/%s: keyFrom must declare exactly one of label or name, not both",
			failureMark(), crdName, location, w.APIVersion, w.Kind)
	}
	if !hasLabel && !hasName {
		return fmt.Errorf("%s crd %q: watch[%s] %s/%s: keyFrom is declared but neither label nor name is set",
			failureMark(), crdName, location, w.APIVersion, w.Kind)
	}
	if hasLabel && kf.Namespace != "" {
		return fmt.Errorf("%s crd %q: watch[%s] %s/%s: keyFrom.namespace has no effect when label is set",
			failureMark(), crdName, location, w.APIVersion, w.Kind)
	}
	return nil
}

func validateCRDSentinels(crdName string, crd orktypes.CRDEntry) error {
	pr := crd.OperatorBox.PreReconcile
	invalid := pr.InvalidSentinels()
	if len(invalid) == 0 {
		if pr.HasEnqueueGateSentinel() {
			enqGateInvalid, invalid := pr.InvalidGateSentinels(pr.EnqueueGate)
			if invalid {
				return fmt.Errorf("%s crd %q: preReconcile.enqueueGate.sentinels: unregistered sentinel(s) [%s] in preReconcile.sentinels — valid values: %s",
					failureMark(), crdName, strings.Join(enqGateInvalid, ", "), strings.Join(pr.DeclaredSentinels(), ", "))
			}
		}

		if pr.HasReconcileGateSentinel() {
			recGateInvalid, invalid := pr.InvalidGateSentinels(pr.ReconcileGate)
			if invalid {
				return fmt.Errorf("%s crd %q: preReconcile.reconcileGate.sentinels: unregistered sentinel(s) [%s] in preReconcile.sentinels — valid values: %s",
					failureMark(), crdName, strings.Join(recGateInvalid, ", "), strings.Join(pr.DeclaredSentinels(), ", "))
			}
		}

		return nil
	}
	return fmt.Errorf("%s crd %q: preReconcile.sentinels: unknown sentinel(s) [%s] — valid values: %s",
		failureMark(), crdName, strings.Join(invalid, ", "), strings.Join(sentinel.ValidSentinels(), ", "))
}

// validatePreReconcileGateTemplates parses enqueueGate and reconcileGate templates
// with a FuncMap that includes only the declared sentinels. Any template that
// references an undeclared sentinel name fails to parse — caught here at validate
// time, not at runtime.
func (e *executor) validatePreReconcileGateTemplates(crdName string, crd orktypes.CRDEntry) error {
	pr := crd.OperatorBox.PreReconcile
	if pr == nil {
		return nil
	}

	// Build the FuncMap: notes (built-ins + user) + declared sentinel stubs.
	funcMap := buildFuncMapForValidation(e.k.Notes)
	for name, fn := range orktemplate.SentinelFuncMap(pr.DeclaredSentinels()) {
		funcMap[name] = fn
	}

	check := func(gate *orktypes.GateConditions, location string) error {
		for i, cond := range gate.WhenConditions() {
			if err := parseGateTemplate(crdName, location, fmt.Sprintf("when[%d].field", i), cond.Field, funcMap); err != nil {
				return err
			}
			if err := parseGateTemplate(crdName, location, fmt.Sprintf("when[%d].equals", i), cond.Equals, funcMap); err != nil {
				return err
			}
		}
		for i, cond := range gate.OrConditions() {
			if err := parseGateTemplate(crdName, location, fmt.Sprintf("or[%d].field", i), cond.Field, funcMap); err != nil {
				return err
			}
			if err := parseGateTemplate(crdName, location, fmt.Sprintf("or[%d].equals", i), cond.Equals, funcMap); err != nil {
				return err
			}
		}
		return nil
	}

	if err := check(pr.EnqueueGate, "preReconcile.enqueueGate"); err != nil {
		return err
	}
	return check(pr.ReconcileGate, "preReconcile.reconcileGate")
}

func parseGateTemplate(crdName, location, field, expr string, funcMap template.FuncMap) error {
	if expr == "" || !isTemplate(expr) {
		return nil
	}
	if _, err := template.New("").Funcs(funcMap).Parse(expr); err != nil {
		return fmt.Errorf("%s crd %q: %s %s: invalid template: %s",
			failureMark(), crdName, location, field, err.Error())
	}
	return nil
}
