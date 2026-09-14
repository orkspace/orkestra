package validate

import (
	"fmt"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateAdmissionRules checks all admission rules present in the katalog
// for correctness
func (e *executor) validateAdmissionRules() error {
	if err := e.validateMutationRules(); err != nil {
		return err
	}
	if err := e.validateValidationRules(); err != nil {
		return err
	}
	if err := e.validateValidationRuleLinks(); err != nil {
		return err
	}
	if err := e.validateAdmissionOperators(); err != nil {
		return err
	}

	return nil
}

// validateAdmissionOperators rejects any validation.rules or mutation.rules
// entry — including their own when:/or: gating conditions — that declares
// an operator: string outside the known ConditionOperator set. An unknown
// operator is otherwise silently skipped by the evaluator and the rule
// always passes; this is exactly how operator: in went unimplemented in
// validation.rules before evaluation was consolidated into
// pkg/types/validation_eval.go.
func (e *executor) validateAdmissionOperators() error {
	for _, crd := range e.k.EnabledCRDs() {
		if crd.HasValidationRules() {
			for _, rule := range crd.Validation.Rules {
				if err := checkKnownOperator(rule.Operator, crd.Name, rule.Field); err != nil {
					return err
				}
				if err := checkConditionOperators(rule.When, crd.Name, rule.Field); err != nil {
					return err
				}
				if err := checkConditionOperators(rule.Or, crd.Name, rule.Field); err != nil {
					return err
				}
			}
		}

		if crd.HasMutationRules() {
			for _, rule := range crd.Mutation.Rules {
				if err := checkConditionOperators(rule.When, crd.Name, rule.Field); err != nil {
					return err
				}
				if err := checkConditionOperators(rule.Or, crd.Name, rule.Field); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// checkKnownOperator returns an error for a set-but-unrecognized operator.
// Empty is fine — it means no explicit operator: was set, not an unknown one.
func checkKnownOperator(op orktypes.ConditionOperator, crdName, field string) error {
	if op == "" || orktypes.IsValidConditionOperator(op) {
		return nil
	}
	return errUnknownOperator(op, crdName, field)
}

func checkConditionOperators(conditions []orktypes.Condition, crdName, field string) error {
	for _, cond := range conditions {
		if err := checkKnownOperator(cond.Operator, crdName, field); err != nil {
			return err
		}
	}
	return nil
}

func errUnknownOperator(op orktypes.ConditionOperator, crd, field string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s Unknown operator %q
   CRD: %s
   field: %s

Allowed values:
  • equals, notEquals
  • contains, notContains, prefix, suffix, regex
  • exists, notExists
  • gt, lt, gte, lte, between, notBetween
  • in, notIn
  • unique
  • typeOf, typeMap, typeList, typeString, typeNumber, typeBool, typeNull
──────────────────────────────────────────────`, failureMark(), op, crd, field)
}

// validateValidationRuleLinks rejects a validation.rules entry's link: value
// that either doesn't name any serve.fields / serve.labels and serve.annotations key on the
// CRD (typo, or the field was renamed/removed and this rule wasn't updated),
// or names an serve.fields key whose Field is already the redundant plain
// "spec.<name>" form — link: only earns its keep when Field isn't already a
// clean display name on its own. See ValidationRule.Link.
func (e *executor) validateValidationRuleLinks() error {
	for _, crd := range e.k.EnabledCRDs() {
		if !crd.HasValidationRules() {
			continue
		}
		for _, rule := range crd.Validation.Rules {
			if rule.Link == "" {
				continue
			}
			if err := checkLink(rule, crd); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkLink(rule orktypes.ValidationRule, crd orktypes.CRDEntry) error {
	if crd.ServeEnabled() {
		if _, ok := crd.Serve.Fields[rule.Link]; ok {
			if rule.Field == "spec."+rule.Link {
				return errRedundantLink(rule.Link, crd.Name)
			}
			return nil
		}
		if _, ok := crd.ServeLabels()[rule.Link]; ok {
			return nil
		}
		if _, ok := crd.ServeAnnotations()[rule.Link]; ok {
			return nil
		}
	}
	return errUnknownLink(rule.Link, crd.Name, rule.Field)
}

// validateMutationRules alks all declared and synthesized mutation rules
// and ensures that:
//
//   - rules.field is not empty
//   - Either default or override is declared - not both
//   - Adds warning if both are declared
func (e *executor) validateMutationRules() error {
	for name, crd := range e.k.EnabledCRDs() {
		// validate serve configuration
		if crd.ServeEnabled() {
			if err := e.validateServeMutationSynth(name); err != nil {
				return err
			}
		}

		if !crd.HasMutationRules() {
			continue
		}
		if crd.HasMutationRules() {
			for i, rule := range crd.Mutation.Rules {
				if rule.Field == "" {
					return fmt.Errorf("%s CRD: %s - mutation.rules[%d].field is required", failureMark(), crd.Name, i)
				}
				if !rule.IsValidChangeType() {
					return fmt.Errorf("%s CRD: %s - mutation.rules[%d] requires one of `default` or `override` to be defined", failureMark(), crd.Name, i)
				}
				if rule.HasDefaultAndOverride() {
					message := fmt.Sprintf("CRD: %s - mutation.rules[%d] has both default and override defined. default will be ignored.", crd.Name, i)
					crd.Warnings.AddWarning(message)
				}
			}
		}

		e.k.EnabledCRDs()[name] = crd
	}
	return nil
}

func (e *executor) validateServeMutationSynth(crdName string) error {
	for name, crd := range e.k.EnabledCRDs() {
		// serve.fields
		for name, cfg := range crd.ServeFields() {
			if cfg.HasDefault() {
				return fmt.Errorf("%s CRD: %s - 'default' is not allowed for serve.fields.%s since this is available from the CRD schema. You may use 'override' instead.",
					failureMark(), crdName, name)
			}
		}

		// serve.labels
		for name, cfg := range crd.ServeLabels() {
			if cfg.HasDefaultAndOverride() {
				message := fmt.Sprintf("CRD: %s - serve.labels.%s has both default and override defined. default will be ignored.", crdName, name)
				crd.Warnings.AddWarning(message)
			}
		}

		// serve.annotations
		for name, cfg := range crd.ServeAnnotations() {
			if cfg.HasDefaultAndOverride() {
				message := fmt.Sprintf("CRD: %s - serve.annotations.%s has both default and override defined. default will be ignored.", crdName, name)
				crd.Warnings.AddWarning(message)
			}
		}

		e.k.EnabledCRDs()[name] = crd
	}
	return nil
}

func (e *executor) validateValidationRules() error {
	for _, crd := range e.k.EnabledCRDs() {
		if !crd.HasValidationRules() {
			continue
		}

		for i, rule := range crd.Validation.Rules {
			if rule.Field == "" {
				return fmt.Errorf("%s CRD: %s - validation.rules[%d].field is required", failureMark(), crd.Name, i)
			}
			if rule.IsEmptyAssertions() {
				return fmt.Errorf("%s CRD: %s - validation.rules[%d] requires one of `operator` or shorthand (eg: equals) to be defined", failureMark(), crd.Name, i)
			}
		}
	}
	return nil
}

func errUnknownLink(link, crd, field string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s link %q does not match any serve field
   CRD: %s
   field: %s

link: must name a key declared in serve.fields, serve.labels,
or serve.annotations for this CRD.
──────────────────────────────────────────────`, failureMark(), link, crd, field)
}

func errRedundantLink(link, crd string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s link %q is redundant
   CRD: %s

This rule's field is already "spec.%s" — already a clean display name on
its own. link: only matters when field is a template expression that isn't
itself a valid display name (e.g. wraps getLabel/getAnnotation, or a notes:
function). Remove link: here.
──────────────────────────────────────────────`, failureMark(), link, crd, link)
}
