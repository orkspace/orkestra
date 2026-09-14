//go:build !runtime && !gateway

package cli

import (
	"fmt"

	"github.com/orkspace/orkestra/pkg/runtime/reconciler"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
)

// admissionViolation is a single fired validation rule — deny or warn.
type admissionViolation struct {
	field   string
	message string
	deny    bool
}

// admissionValidationResult is the outcome of running all validation rules
// for one CRD+CR pair. Passed counts rules that either did not match their
// when: guard or evaluated to no violation.
type admissionValidationResult struct {
	violations []admissionViolation
	passed     int
	total      int
}

func (r admissionValidationResult) denied() int {
	var n int
	for _, v := range r.violations {
		if v.deny {
			n++
		}
	}
	return n
}

func (r admissionValidationResult) warned() int {
	var n int
	for _, v := range r.violations {
		if !v.deny {
			n++
		}
	}
	return n
}

// admissionMutationPreview describes one mutation that would be applied.
type admissionMutationPreview struct {
	field   string
	found   bool
	from    string
	to      interface{}
	mutType string // "default" or "override"
}

// admissionMutationResult collects all previews for one CRD+CR pair.
type admissionMutationResult struct {
	previews []admissionMutationPreview
}

// applyMutationPreviews returns a deep copy of obj with all mutation previews
// applied. Used to simulate mutateFirst: true behaviour locally.
func applyMutationPreviews(obj map[string]interface{}, previews []admissionMutationPreview) map[string]interface{} {
	clone := utils.DeepCopyMap(obj)
	for _, p := range previews {
		_ = utils.SetNestedPath(clone, p.field, p.to)
	}
	return clone
}

// evalAdmissionValidation runs validation.rules against obj and returns the
// result. It does not print — callers format the output for their context.
func evalAdmissionValidation(obj map[string]interface{}, crd *orktypes.CRDEntry, resolver *orktmpl.Resolver, eval orktypes.TemplateEvaluator) admissionValidationResult {
	if !crd.HasValidationRules() {
		return admissionValidationResult{}
	}
	result := admissionValidationResult{total: len(crd.Validation.Rules)}
	for _, rule := range crd.Validation.Rules {
		if !orktypes.EvaluateConditions(obj, rule.When, rule.Or, eval) {
			result.passed++
			continue
		}
		v := orktypes.EvaluateValidationRule(obj, resolver, rule)
		if v == nil {
			result.passed++
			continue
		}
		result.violations = append(result.violations, admissionViolation{
			field:   v.Field,
			message: v.Message,
			deny:    v.Action.IsDeny(),
		})
	}
	return result
}

// evalAdmissionMutation previews mutation.rules against obj and returns what
// would be applied. It does not print — callers format the output.
func evalAdmissionMutation(obj map[string]interface{}, crd *orktypes.CRDEntry, resolver *orktmpl.Resolver, eval orktypes.TemplateEvaluator) admissionMutationResult {
	var result admissionMutationResult
	if !crd.HasMutationRules() {
		return result
	}
	for _, rule := range crd.Mutation.Rules {
		if !orktypes.EvaluateConditions(obj, rule.When, rule.Or, eval) {
			continue
		}
		field := rule.Field
		if orktypes.IsTemplate(field) {
			if resolved, err := resolver.Resolve(field); err == nil {
				field = resolved
			}
		}
		currentVal, found := orktypes.ResolveScalarField(obj, field)
		desired, mutType, err := reconciler.ResolveRuleValue(rule, found, currentVal, resolver)
		if err != nil || desired == nil {
			continue
		}
		if fmt.Sprintf("%v", desired) == currentVal {
			continue
		}
		result.previews = append(result.previews, admissionMutationPreview{
			field:   field,
			found:   found,
			from:    currentVal,
			to:      desired,
			mutType: mutType,
		})
	}
	return result
}
