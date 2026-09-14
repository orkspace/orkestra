// pkg/types/conditions.go
package types

// ── Conditional provisioning ───────────────────────────────────────────────────
//
// When conditions are declared on individual template sources under onCreate,
// onReconcile, or onDelete. The reconciler evaluates them against the live CR
// before calling the registry. If any condition fails, the resource is skipped
// for this reconcile cycle — not an error, just a no-op.
//
// All conditions in a When block are AND'd:
//   when:
//     - field: spec.environment
//       equals: production
//     - field: spec.enabled
//       equals: "true"
//
// Both must pass for the resource to be created.

// Condition declares a single condition to evaluate against the CR or runtime state.
// Fields reference CR paths using dot notation: spec.environment, metadata.name.
//
// The same type is used in:
//   - when: / or: on template sources (resource conditions)
//   - operatorBox.autoscale.conditions.or and when: (autoscale conditions)
//   - operatorBox.rollback.trigger (rollback conditions)
//   - notification condition blocks
type Condition struct {
	// Field — dot-notation path to a field in the CR object or a runtime metric.
	// e.g. "spec.environment", "metrics.queueDepth", "cross.managed-database.metrics.queueDepth"
	Field string `yaml:"field" validate:"required" json:"field"`

	// Operator — how to compare the field value.
	// See ConditionOperator constants below.
	Operator ConditionOperator `yaml:"operator,omitempty" json:"operator,omitempty"`

	// Value — the value to compare against.
	// Not used for exists/notExists operators.
	// Supports template expressions: "{{ .metadata.name }}-prod"
	Value string `yaml:"value,omitempty" json:"value,omitempty"`

	// Equals is a shorthand for operator: equals.
	Equals string `yaml:"equals,omitempty" json:"equals,omitempty"`

	// NotEquals is a shorthand for operator: notEquals.
	NotEquals string `yaml:"notEquals,omitempty" json:"notEquals,omitempty"`

	// Contains is a shorthand for operator: contains.
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`

	// NotContains is a shorthand for operator: notContains.
	NotContains string `yaml:"notContains,omitempty" json:"notContains,omitempty"`

	// Regex is a shorthand for operator: regex.
	Regex string `yaml:"regex,omitempty" json:"regex,omitempty"`

	// Prefix is a shorthand for operator: prefix.
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`

	// NotPrefix is a shorthand for operator: notPrefix.
	NotPrefix string `yaml:"notPrefix,omitempty" json:"notPrefix,omitempty"`

	// Suffix is a shorthand for operator: suffix.
	Suffix string `yaml:"suffix,omitempty" json:"suffix,omitempty"`

	// NotSuffix is a shorthand for operator: notSuffix.
	NotSuffix string `yaml:"notSuffix,omitempty" json:"notSuffix,omitempty"`

	// Exists is a shorthand for operator: exists.
	// When true, the condition passes when the field is present and non-empty.
	Exists *bool `yaml:"exists,omitempty" json:"exists,omitempty"`

	// NotExists is a shorthand for operator: notExists.
	// When true, the condition passes when the field is absent or empty.
	NotExists *bool `yaml:"notExists,omitempty" json:"notExists,omitempty"`

	// Numeric shorthands
	GreaterThan string `yaml:"greaterThan,omitempty" json:"greaterThan,omitempty"`
	LessThan    string `yaml:"lessThan,omitempty" json:"lessThan,omitempty"`

	// GreaterThanOrEqual is a shorthand for operator: gte.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty" json:"greaterThanOrEqual,omitempty"`

	// LessThanOrEqual is a shorthand for operator: lte.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty" json:"lessThanOrEqual,omitempty"`

	// Min is a shorthand for operator: gte. Same operator as GreaterThanOrEqual —
	// reads better for a bound on a quantity (min: "1") than a direct comparison.
	Min string `yaml:"min,omitempty" json:"min,omitempty"`

	// Max is a shorthand for operator: lte. Same operator as LessThanOrEqual.
	Max string `yaml:"max,omitempty" json:"max,omitempty"`

	// Between is a shorthand for operator: between. Comma-separated "min,max",
	// both inclusive.
	Between string `yaml:"between,omitempty" json:"between,omitempty"`

	// NotBetween is a shorthand for operator: notBetween. Comma-separated
	// "min,max", same bounds as between.
	NotBetween string `yaml:"notBetween,omitempty" json:"notBetween,omitempty"`

	// In is a shorthand for operator: in. Comma-separated list.
	In string `yaml:"in,omitempty" json:"in,omitempty"`

	// NotIn is a shorthand for operator: notIn. Comma-separated list.
	NotIn string `yaml:"notIn,omitempty" json:"notIn,omitempty"`

	// ── Time-based fields (or in autoscale conditions) ────────────────────

	// Time — active when the current time is within the declared window.
	// After and Before are both optional; omit one for a half-open range.
	//   or:
	//     - time:
	//         after: "08:00"
	//         before: "20:00"
	Time *TimeWindow `yaml:"time,omitempty" json:"time,omitempty"`

	// DayOfWeek — active on the specified days of the week.
	//   or:
	//     - dayOfWeek:
	//         in: [Monday, Tuesday, Wednesday, Thursday, Friday]
	DayOfWeek *DayOfWeekCondition `yaml:"dayOfWeek,omitempty" json:"dayOfWeek,omitempty"`

	// Cron — a standard cron expression (5-field) that defines when the
	// window opens. Duration defines how long the window stays open.
	// Without Duration, the window stays open until the next fire. Add Duration to close it sooner.
	Cron string `yaml:"cron,omitempty" json:"cron,omitempty"`

	// Duration — how long a cron-opened window remains active.
	Duration Duration `yaml:"duration,omitempty" json:"duration,omitempty"`

	// ── Notification ─────────────────────────────────────────────────────────

	// Notify declares teams to alert when this condition is true.
	Notify *NotifyBlock `yaml:"notify,omitempty" json:"notify,omitempty"`

	// ── Cross-binary metric fallback ─────────────────────────────────────────

	// Negate — when true, the result of this condition is inverted.
	// Applies to all condition types: field comparisons, time windows, dayOfWeek, cron.
	//
	//   when:
	//     - dayOfWeek:
	//         weekday: true
	//       negate: true   # passes on weekends
	Negate bool `yaml:"negate,omitempty" json:"negate,omitempty"`

	// Source is the HTTP fallback for cross-binary metric observation.
	// Only used when field is a cross.<crd>.metrics.* field and the CRD
	// is not registered in GlobalCrossMetricsRegistry (different binary).
	// The endpoint must be the remote operator's /katalog/{crd} URL.
	//
	//   when:
	//     - field: cross.managed-database.metrics.queueDepth
	//       greaterThan: "500"
	//       source:
	//         host: "http://orkestra-database-operator:8080"
	// 		   crd: managed-database
	//   when:
	//     - field: cross.managed-database.metrics.queueDepth
	//       greaterThan: "500"
	//       source:
	//         endpoint: "http://non-orkestra-database-operator:8080/api/managed-database/metrics"
	Source *CrossSource `yaml:"source,omitempty" json:"source,omitempty"`
}

// NotifyBlock declares notification targets and an optional message override
// for a specific condition.
type NotifyBlock struct {
	// Teams is the list of team names (from notification.teams) to alert.
	Teams []string `yaml:"teams" json:"teams"`
	// Message is a Go template expression for the notification body.
	// Overrides the team's own message template.
	// When empty, uses the team's configured message or the system default.
	Message string `yaml:"message,omitempty" json:"message,omitempty"`
}

// ConditionOperator defines how a condition's field is compared to its value.
type ConditionOperator string

const (
	// ConditionEquals — field value exactly equals the condition value (string comparison)
	ConditionEquals ConditionOperator = "equals"

	// ConditionNotEquals — field value does not equal the condition value
	ConditionNotEquals ConditionOperator = "notEquals"

	// ConditionContains — field value contains the condition value as a substring
	ConditionContains ConditionOperator = "contains"

	// ConditionPrefix — field value starts with the condition value
	ConditionPrefix ConditionOperator = "prefix"

	// ConditionNotPrefix — field value does not start with the condition value
	ConditionNotPrefix ConditionOperator = "notPrefix"

	// ConditionSuffix — field value ends with the condition value
	ConditionSuffix ConditionOperator = "suffix"

	// ConditionNotSuffix — field value does not end with the condition value
	ConditionNotSuffix ConditionOperator = "notSuffix"

	// ConditionExists — the field is present and non-empty
	// Value is ignored for this operator.
	ConditionExists ConditionOperator = "exists"

	// ConditionNotExists — the field is absent or empty
	// Value is ignored for this operator.
	// Use for: first-reconcile detection (phase not yet written).
	//   when:
	//     - field: status.phase
	//       operator: notExists
	ConditionNotExists ConditionOperator = "notExists"

	// ConditionGt — field value is numerically greater than condition value
	ConditionGt ConditionOperator = "gt"

	// ConditionLt — field value is numerically less than condition value
	ConditionLt ConditionOperator = "lt"

	// ConditionGte — field value is numerically greater than or equal to condition value
	ConditionGte ConditionOperator = "gte"

	// ConditionLte — field value is numerically less than or equal to condition value
	ConditionLte ConditionOperator = "lte"

	// ConditionIn — field value is one of a comma-separated list.
	// Empty string matches an empty field (for first-reconcile detection).
	//   when:
	//     - field: status.phase
	//       operator: in
	//       value: ",Pending"   # empty or "Pending"
	ConditionIn ConditionOperator = "in"

	// ConditionNotIn — field value is none of a comma-separated list.
	ConditionNotIn ConditionOperator = "notIn"

	// ConditionNotContains — field value does not contain the condition value as a substring
	ConditionNotContains ConditionOperator = "notContains"

	// ConditionRegex — field value matches the condition value as an RE2 regular expression
	// (Go's regexp package — same syntax as re2, not PCRE). An invalid pattern
	// fails the condition rather than erroring, same as a non-numeric gt/lt value.
	ConditionRegex ConditionOperator = "regex"

	// ConditionBetween — field value is numerically within an inclusive range.
	// Value is a comma-separated "min,max" pair.
	//   when:
	//     - field: spec.replicas
	//       operator: between
	//       value: "1,10"
	ConditionBetween ConditionOperator = "between"

	// ConditionNotBetween — field value is numerically outside an inclusive
	// range. Value is a comma-separated "min,max" pair, same as between.
	ConditionNotBetween ConditionOperator = "notBetween"

	// ConditionUnique — field value must not match any other existing
	// instance of this CRD (the CR being evaluated is excluded from its own
	// check). Works the same way in validation.rules and in when:/or: —
	// e.g. gating a template source or mutation rule on whether a field is
	// still available.
	//
	// Enforced at reconcile time via a live List() the reconciler injects
	// (pkg/runtime/reconciler/uniqueness.go) — the authoritative check,
	// immune to cache staleness. Also enforced at admission time, as a
	// fast best-effort early rejection: the gateway queries the runtime's
	// own informer cache over HTTP (pkg/gateway/webhook/uniqueness.go)
	// rather than doing a live List() itself. A stale cache there just
	// means a duplicate slips through admission and gets caught on the
	// next reconcile anyway — the reconcile-time guarantee never depends
	// on admission catching it first.
	//
	//	validation:
	//	  rules:
	//	    - field: spec.domain
	//	      operator: unique
	//	      message: "spec.domain must be unique across all instances"
	//	      action: deny
	//
	//	when:
	//	  - field: spec.domain
	//	    operator: unique
	//
	// Always passes wherever no checker is injected — e2e, simulate without
	// a seeded fixture, and any other context without live CRD access (see
	// UniquenessChecker in validation_eval.go).
	ConditionUnique ConditionOperator = "unique"

	// ConditionTypeOf — check field type
	ConditionTypeOf ConditionOperator = "typeOf"

	// ConditionTypeMap — field value is a map (YAML object)
	ConditionTypeMap ConditionOperator = "typeMap"

	// ConditionTypeList — field value is a slice (YAML array)
	ConditionTypeList ConditionOperator = "typeList"

	// ConditionTypeString — field value is a string
	ConditionTypeString ConditionOperator = "typeString"

	// ConditionTypeNumber — field value is a number (int/float)
	ConditionTypeNumber ConditionOperator = "typeNumber"

	// ConditionTypeBool — field value is a boolean
	ConditionTypeBool ConditionOperator = "typeBool"

	// ConditionTypeNull — field value is null or missing
	ConditionTypeNull ConditionOperator = "typeNull"
)

// knownConditionOperators is the complete set of operator strings that
// EvaluateValidationRule and EvaluateOneCond know how to evaluate.
var knownConditionOperators = map[ConditionOperator]bool{
	ConditionEquals:      true,
	ConditionNotEquals:   true,
	ConditionContains:    true,
	ConditionPrefix:      true,
	ConditionNotPrefix:   true,
	ConditionSuffix:      true,
	ConditionNotSuffix:   true,
	ConditionExists:      true,
	ConditionNotExists:   true,
	ConditionGt:          true,
	ConditionLt:          true,
	ConditionGte:         true,
	ConditionLte:         true,
	ConditionIn:          true,
	ConditionNotIn:       true,
	ConditionNotContains: true,
	ConditionRegex:       true,
	ConditionBetween:     true,
	ConditionNotBetween:  true,
	ConditionUnique:      true,
	ConditionTypeOf:      true,
	ConditionTypeMap:     true,
	ConditionTypeList:    true,
	ConditionTypeString:  true,
	ConditionTypeNumber:  true,
	ConditionTypeBool:    true,
	ConditionTypeNull:    true,
}

// IsValidConditionOperator reports whether op is one of the known operators
// evaluated by EvaluateValidationRule / EvaluateOneCond. An unrecognized
// operator string is silently skipped by both evaluators (the rule always
// passes) rather than erroring — this is what katalog-load-time validation
// should reject instead of letting through. Empty is not valid here; check
// for that separately since it means "no explicit operator", not "unknown
// operator" (a shorthand field or the exists-default may still apply).
func IsValidConditionOperator(op ConditionOperator) bool {
	return knownConditionOperators[op]
}
