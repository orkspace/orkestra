// pkg/types/validation_eval.go — ValidationRule evaluation, shared by the
// reconciler (every reconcile) and the admission webhook (admission time).
//
// This used to be two independently hand-maintained copies of the same
// switch statement, one in pkg/runtime/reconciler, one in
// pkg/gateway/webhook. That's how operator: in — defined for when:/or:
// conditions — went unimplemented in validation.rules in both places at
// once, silently, without either side noticing: a rule using it always
// passed. One implementation means one place to fix, and no way for the two
// enforcement paths to drift out of sync with each other again.
package types

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/orkspace/orkestra/pkg/logger"
)

// TemplateResolver resolves a Go-template expression string against CR data
// and notes. Satisfied structurally by *pkg/template.Resolver,
// which this package can't import directly — that package already imports
// pkg/types, so a reverse import would cycle.
type TemplateResolver interface {
	Resolve(value string) (string, error)
}

// UniquenessChecker reports whether no other existing instance of the CRD
// under evaluation currently has the same value for a given field. Satisfied
// structurally by two concrete implementations, both outside this package to
// avoid a reverse-import-cycle (same reason as TemplateResolver):
// pkg/runtime/reconciler's live-List()-backed checker (reconcile time, the
// authoritative one) and pkg/gateway/webhook's HTTP-backed checker
// (admission time, best-effort — queries the runtime's own informer cache
// instead of doing a live List() itself).
type UniquenessChecker interface {
	// IsUnique reports whether no other CR of this kind currently has
	// field == value. selfNamespace/selfName identify the CR under
	// evaluation so it's excluded from the comparison — a CR is never a
	// duplicate of its own already-stored value.
	IsUnique(field, value, selfNamespace, selfName string) (bool, error)
}

// uniquenessCheckerKey is the data-map key an injected UniquenessChecker is
// stored under — same convention as _cronWindows in when.go: state that
// doesn't belong in a CR's template context travels alongside it in the
// data map instead of as an explicit parameter on every evaluation
// function. Set via template.Resolver.WithUniquenessChecker — the
// reconciler wires in a live-List() checker on every reconcile, the
// admission webhook wires in an HTTP-backed one on every admission request,
// so operator: unique is enforced identically in both validation.rules and
// when:/or: at both points. Always passes elsewhere (e2e, template-only
// contexts, simulate without a seeded fixture) where there's no live CRD to
// check against.
const uniquenessCheckerKey = "_uniquenessChecker"

// resolveUnique evaluates operator: unique against a checker injected under
// uniquenessCheckerKey, excluding the CR under evaluation by its own
// metadata.namespace/name. Shared by EvaluateValidationRule and
// applyOperator (when.go) so the operator behaves identically wherever it's
// used.
//
// Returns true (pass) when no checker is injected or the checker errors —
// uniqueness is enforced when a live checker is available, not required.
func resolveUnique(data map[string]interface{}, field, fieldVal string) bool {
	checker, ok := data[uniquenessCheckerKey].(UniquenessChecker)
	if !ok || checker == nil {
		return true
	}
	selfNS, _ := ResolveScalarField(data, "metadata.namespace")
	selfName, _ := ResolveScalarField(data, "metadata.name")
	unique, err := checker.IsUnique(field, fieldVal, selfNS, selfName)
	if err != nil {
		logger.Warn().Err(err).Str("field", field).
			Msg("validation: unique check failed — rule skipped")
		return true
	}
	return unique
}

// RuleViolation is the outcome of one failed ValidationRule.
type RuleViolation struct {
	Field   string // the rule's original field expression (template or dot-path)
	Rule    string // effective operator name, e.g. "exists", "in", "gt"
	Value   string // the actual field value that failed
	Message string
	Action  ValidationAction
}

// ResolveScalarField resolves a dot-notation field path against a CR data
// map and returns its scalar string form, and whether it was found. Shared
// by validation-rule and mutation-rule evaluation, at both reconcile time
// and admission time.
func ResolveScalarField(data map[string]interface{}, path string) (string, bool) {
	parts := strings.Split(path, ".")
	current := data

	for i, part := range parts {
		val, ok := current[part]
		if !ok {
			return "", false
		}
		if i == len(parts)-1 {
			return ScalarToString(val), true
		}
		next, ok := val.(map[string]interface{})
		if !ok {
			return "", false
		}
		current = next
	}
	return "", false
}

// ScalarToString converts a decoded Kubernetes JSON scalar (string, bool,
// float64 for numbers, or a pre-converted int64) into its string form.
func ScalarToString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// ResolveValidationOp resolves the effective operator and comparison value
// from a ValidationRule, handling all shorthand fields. Falls back to an
// explicit operator+value pair, or a bare exists check when neither is set.
func ResolveValidationOp(r ValidationRule) (ConditionOperator, string) {
	switch {
	case r.Equals != "":
		return ConditionEquals, r.Equals
	case r.NotEquals != "":
		return ConditionNotEquals, r.NotEquals
	case r.Prefix != "":
		return ConditionPrefix, r.Prefix
	case r.NotPrefix != "":
		return ConditionNotPrefix, r.NotPrefix
	case r.Suffix != "":
		return ConditionSuffix, r.Suffix
	case r.NotSuffix != "":
		return ConditionNotSuffix, r.NotSuffix
	case r.Contains != "":
		return ConditionContains, r.Contains
	case r.NotContains != "":
		return ConditionNotContains, r.NotContains
	case r.Regex != "":
		return ConditionRegex, r.Regex
	case r.Min != "":
		// Min/Max are documented as inclusive bounds — Gte/Lte, not Gt/Lt.
		return ConditionGte, r.Min
	case r.Max != "":
		return ConditionLte, r.Max
	case r.GreaterThan != "":
		return ConditionGt, r.GreaterThan
	case r.LessThan != "":
		return ConditionLt, r.LessThan
	case r.GreaterThanOrEqual != "":
		return ConditionGte, r.GreaterThanOrEqual
	case r.LessThanOrEqual != "":
		return ConditionLte, r.LessThanOrEqual
	case r.Between != "":
		return ConditionBetween, r.Between
	case r.NotBetween != "":
		return ConditionNotBetween, r.NotBetween
	case r.In != "":
		return ConditionIn, r.In
	case r.NotIn != "":
		return ConditionNotIn, r.NotIn
	case r.Operator != "":
		return r.Operator, r.Value
	default:
		return ConditionExists, ""
	}
}

// RuleTypeLabel returns a short string identifying a ValidationRule's
// effective comparison for use as a low-cardinality metric label. Prefers
// the shorthand name the katalog author actually wrote (e.g. "min", "max")
// over the operator it resolves to (min resolves to ConditionGte, not a
// distinct "min" operator), since that's the more actionable label for
// "which rule type is causing the most friction" alerting.
func RuleTypeLabel(r ValidationRule) string {
	switch {
	case r.Equals != "":
		return "equals"
	case r.NotEquals != "":
		return "notEquals"
	case r.Prefix != "":
		return "prefix"
	case r.Suffix != "":
		return "suffix"
	case r.Contains != "":
		return "contains"
	case r.NotContains != "":
		return "notContains"
	case r.Regex != "":
		return "regex"
	case r.Min != "":
		return "min"
	case r.Max != "":
		return "max"
	case r.GreaterThan != "":
		return "greaterThan"
	case r.LessThan != "":
		return "lessThan"
	case r.GreaterThanOrEqual != "":
		return "greaterThanOrEqual"
	case r.LessThanOrEqual != "":
		return "lessThanOrEqual"
	case r.Between != "":
		return "between"
	case r.NotBetween != "":
		return "notBetween"
	case r.In != "":
		return "in"
	case r.NotIn != "":
		return "notIn"
	case r.Operator != "":
		return string(r.Operator)
	default:
		return "exists"
	}
}

// inCommaList reports whether value equals one of expected's comma-separated
// entries (each trimmed of surrounding whitespace).
func inCommaList(value, expected string) bool {
	for _, v := range strings.Split(expected, ",") {
		if strings.TrimSpace(v) == value {
			return true
		}
	}
	return false
}

// EvaluateValidationRule evaluates one ValidationRule against CR data and
// returns the violation if it fails, nil if it passes.
//
// resolver may be nil — the reconciler runs without one when no resolver
// context is available; template expressions in field/value/message are
// then left unresolved and matched against the raw value instead. Callers
// passing a possibly-nil concrete resolver type must nil-check before
// converting to this interface, since a typed nil wrapped in an interface
// is not itself nil.
func EvaluateValidationRule(data map[string]interface{}, resolver TemplateResolver, rule ValidationRule) *RuleViolation {
	op, expected := ResolveValidationOp(rule)

	// Resolve template expressions in comparison values and messages.
	// Notes are called by name directly: {{ inBusinessHours }}, {{ allowedRegistry }}.
	if resolver != nil && IsTemplate(expected) {
		if resolved, err := resolver.Resolve(expected); err == nil {
			expected = resolved
		}
	}
	message := rule.Message
	if resolver != nil && IsTemplate(message) {
		if resolved, err := resolver.Resolve(message); err == nil {
			message = resolved
		}
	}

	// displayField is shown in violation messages and is what UI clients
	// (the Control Center IDP form) match back to the field they rendered.
	// rule.Link overrides it when set — see ValidationRule.Link. Otherwise
	// it's the original expression: when field is a template, the resolved
	// value is the result of the expression directly (not a CR path), so we
	// skip ResolveScalarField and use it as fieldVal.
	displayField := rule.Field
	if rule.Link != "" {
		displayField = rule.Link
	}
	isTemplate := IsTemplate(rule.Field)

	var fieldVal string
	var found bool
	if isTemplate && resolver != nil {
		fieldVal, _ = resolver.Resolve(rule.Field)
		found = fieldVal != ""
	} else {
		fieldVal, found = ResolveScalarField(data, rule.Field)
		// Plain path not found in the CR object — try the resolver data.
		// This lets rules use bare paths like "request.schedule" for resolver-injected
		// fields (e.g. WithRequest) without needing {{ }} template syntax.
		if !found && resolver != nil {
			if resolved, err := resolver.Resolve("{{ ." + rule.Field + " }}"); err == nil && resolved != "" {
				fieldVal = resolved
				found = true
			}
		}
	}

	fail := func() *RuleViolation {
		return &RuleViolation{
			Field:   displayField,
			Rule:    string(op),
			Value:   fieldVal,
			Message: message,
			Action:  rule.Action,
		}
	}

	switch op {
	case ConditionExists:
		if !found || fieldVal == "" {
			return fail()
		}

	case ConditionNotExists:
		if found && fieldVal != "" {
			return fail()
		}

	case ConditionEquals:
		if !found || fieldVal != expected {
			return fail()
		}

	case ConditionNotEquals:
		if found && fieldVal == expected {
			return fail()
		}

	case ConditionContains:
		if !found || !strings.Contains(fieldVal, expected) {
			return fail()
		}

	case ConditionPrefix:
		if !found || !strings.HasPrefix(fieldVal, expected) {
			return fail()
		}

	case ConditionNotPrefix:
		if !found || strings.HasPrefix(fieldVal, expected) {
			return fail()
		}

	case ConditionSuffix:
		if !found || !strings.HasSuffix(fieldVal, expected) {
			return fail()
		}

	case ConditionNotSuffix:
		if !found || strings.HasSuffix(fieldVal, expected) {
			return fail()
		}

	case ConditionIn:
		if !found || !inCommaList(fieldVal, expected) {
			return fail()
		}

	case ConditionNotIn:
		if !found || inCommaList(fieldVal, expected) {
			return fail()
		}

	case ConditionNotContains:
		if !found || strings.Contains(fieldVal, expected) {
			return fail()
		}

	case ConditionRegex:
		re, err := regexp.Compile(expected)
		if err != nil {
			logger.Warn().Str("field", rule.Field).Str("val", expected).
				Msg("validation: regex is not a valid pattern — rule skipped")
			return nil
		}
		if !found || !re.MatchString(fieldVal) {
			return fail()
		}

	case ConditionGt: // strict — use min: or gte: for an inclusive bound
		cv, err := strconv.ParseFloat(expected, 64)
		if err != nil {
			logger.Warn().Str("field", rule.Field).Str("val", expected).
				Msg("validation: gt requires a numeric value — rule skipped")
			return nil
		}
		fv, err := strconv.ParseFloat(fieldVal, 64)
		if err != nil || fv <= cv {
			return fail()
		}

	case ConditionLt: // strict — use max: or lte: for an inclusive bound
		cv, err := strconv.ParseFloat(expected, 64)
		if err != nil {
			logger.Warn().Str("field", rule.Field).Str("val", expected).
				Msg("validation: lt requires a numeric value — rule skipped")
			return nil
		}
		fv, err := strconv.ParseFloat(fieldVal, 64)
		if err != nil || fv >= cv {
			return fail()
		}

	case ConditionGte: // used as Min when coming from rule.Min
		cv, err := strconv.ParseFloat(expected, 64)
		if err != nil {
			logger.Warn().Str("field", rule.Field).Str("val", expected).
				Msg("validation: gte requires a numeric value — rule skipped")
			return nil
		}
		fv, err := strconv.ParseFloat(fieldVal, 64)
		if err != nil || fv < cv {
			return fail()
		}

	case ConditionLte: // used as Max when coming from rule.Max
		cv, err := strconv.ParseFloat(expected, 64)
		if err != nil {
			logger.Warn().Str("field", rule.Field).Str("val", expected).
				Msg("validation: lte requires a numeric value — rule skipped")
			return nil
		}
		fv, err := strconv.ParseFloat(fieldVal, 64)
		if err != nil || fv > cv {
			return fail()
		}

	case ConditionBetween:
		lo, hi, ok := parseBetween(expected)
		if !ok {
			logger.Warn().Str("field", rule.Field).Str("val", expected).
				Msg("validation: between requires \"min,max\" numeric values — rule skipped")
			return nil
		}
		fv, err := strconv.ParseFloat(fieldVal, 64)
		if err != nil || fv < lo || fv > hi {
			return fail()
		}

	case ConditionNotBetween:
		lo, hi, ok := parseBetween(expected)
		if !ok {
			logger.Warn().Str("field", rule.Field).Str("val", expected).
				Msg("validation: notBetween requires \"min,max\" numeric values — rule skipped")
			return nil
		}
		fv, err := strconv.ParseFloat(fieldVal, 64)
		if err != nil || (fv >= lo && fv <= hi) {
			return fail()
		}

	case ConditionUnique:
		if _, hasChecker := data[uniquenessCheckerKey].(UniquenessChecker); !hasChecker {
			// No checker injected (e2e, simulate without a seeded fixture,
			// or a CRD the webhook couldn't resolve) — always passes, same
			// as EvaluateOneCond.
			return nil
		}
		if !found || !resolveUnique(data, rule.Field, fieldVal) {
			return fail()
		}
	}

	return nil // rule passed
}

// ConvertToType converts a string value to the requested type.
// Supported valueType: "int", "integer", "bool", "boolean", "float", "number", "string" (default).
// Returns the typed value (int64, bool, float64, or string) suitable for JSON patch.
func ConvertToType(val string, valueType string) (interface{}, error) {
	switch valueType {
	case "int", "integer":
		i, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot convert %q to int: %w", val, err)
		}
		return i, nil
	case "bool", "boolean":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("cannot convert %q to bool: %w", val, err)
		}
		return b, nil
	case "float", "number":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot convert %q to float: %w", val, err)
		}
		return f, nil
	default: // "string" or empty
		return val, nil
	}
}
