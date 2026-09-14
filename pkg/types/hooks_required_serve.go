package types

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// serveFieldRef is one serve.fields / serve.labels and serve.annotations entry, resolved to
// the field expression a synthesized validation rule would target.
type serveFieldRef struct {
	name  string // the plain serve.fields / serve.labels and serve.annotations key, for sorting
	field string // "spec.<name>" dot-path, or a getLabel/getAnnotation template
	link  string // same as name, for ValidationRule.Link — set only when field isn't already "spec.<name>"
	label string
	cfg   ServeFieldConfig
}

// allServeFieldRefs walks serve.fields, serve.labels, and
// serve.annotations, yielding one serveFieldRef per entry —
// the shared source both RequiredServeFieldRules and
// EnumServeFieldRules synthesize from.
//
// serve.labels and serve.annotations entries resolve through
// the "getLabel/getAnnotation" notes rather than a raw
// "metadata.labels.<name>" dot-path. This is because an annotation key
// following the Kubernetes-recommended prefix/name shape might contains dots
// itself, which the Resolver would misparse as extra path segments.
//
// Also, the notes (template expressions) are not valid display names either,
// which is what serveFieldRef.link is for — see ValidationRule.Link.
//
// Returned sorted by cfg.Order (0 = unset, sorts last),
// then name — the same rule the Control Center uses to arrange
// the rendered form. Sorting to match the form means the
// field a developer sees first is also the one whose violation wins.
func (c *CRDEntry) allServeFieldRefs() []serveFieldRef {
	if c == nil || c.Serve == nil {
		return nil
	}

	var refs []serveFieldRef
	for name, cfg := range c.Serve.Fields {
		// "spec." + name is already a clean display name on its own —
		// no link needed, and setting one would trip the "redundant link"
		// validation error.
		refs = append(refs, serveFieldRef{name: name, field: "spec." + name, label: serveFieldLabel(cfg, name), cfg: cfg})
	}
	for name, cfg := range c.ServeLabels() {
		refs = append(refs, serveFieldRef{name: name, field: fmt.Sprintf(`{{ getLabel . %q }}`, name), link: name, label: serveFieldLabel(cfg, name), cfg: cfg})
	}
	for name, cfg := range c.ServeAnnotations() {
		refs = append(refs, serveFieldRef{name: name, field: fmt.Sprintf(`{{ getAnnotation . %q }}`, name), link: name, label: serveFieldLabel(cfg, name), cfg: cfg})
	}

	sort.Slice(refs, func(i, j int) bool {
		oi, oj := refs[i].cfg.Order, refs[j].cfg.Order
		if oi == 0 && oj != 0 {
			return false
		}
		if oi != 0 && oj == 0 {
			return true
		}
		if oi != oj {
			return oi < oj
		}
		return refs[i].name < refs[j].name
	})
	return refs
}

// DuplicateServeFieldOrders reports every explicit (non-zero) serve.fields /
// serve.labels and serve.annotations order: value shared by more than one field on this
// CRD, keyed by the colliding order number, field names sorted for a
// deterministic error message. order: 0 (unset) is never a collision — any
// number of fields can leave it unset at once, see allServeFieldRefs.
//
// order used to be purely cosmetic (form layout). It no longer is:
// allServeFieldRefs sorts by it to decide synthesized validation-rule
// priority, so two fields silently sharing an order value now means an
// arbitrary (name-based) tiebreak decides which one's violation gets
// reported when both fail at once — worth catching at load time instead.
func (c *CRDEntry) DuplicateServeFieldOrders() map[int][]string {
	if c == nil || c.Serve == nil {
		return nil
	}
	byOrder := map[int][]string{}
	for _, ref := range c.allServeFieldRefs() {
		if ref.cfg.Order == 0 {
			continue
		}
		byOrder[ref.cfg.Order] = append(byOrder[ref.cfg.Order], ref.name)
	}
	var dups map[int][]string
	for order, names := range byOrder {
		if len(names) > 1 {
			if dups == nil {
				dups = map[int][]string{}
			}
			slices.Sort(names)
			dups[order] = names
		}
	}
	return dups
}

func serveFieldLabel(cfg ServeFieldConfig, name string) string {
	if cfg.Label != "" {
		return cfg.Label
	}
	return name
}

// RequiredServeFieldRules synthesizes an implicit exists validation rule for
// every serve.fields / serve.labels and serve.annotations entry marked required: true —
// enforced server-side, for every Gateway API client, not just the Control
// Center form. See "Required fields are enforced automatically" at
// https://orkestra.sh/docs/reference/schema/katalog/validation#required-fields-are-enforced-automatically
// for the full rationale, including why inheriting the field's own
// When/Or matters for discriminator-routed CRDs.
func (c *CRDEntry) RequiredServeFieldRules() []ValidationRule {
	var rules []ValidationRule
	for _, ref := range c.allServeFieldRefs() {
		if !ref.cfg.Required {
			continue
		}
		rules = append(rules, ValidationRule{
			Field:    ref.field,
			Link:     ref.link,
			Operator: ConditionExists,
			Message:  ref.label + " is required",
			Action:   ValidationActionDeny,
			When:     ref.cfg.When,
			Or:       ref.cfg.Or,
		})
	}
	return rules
}

// EnumServeFieldRules synthesizes an implicit `in` validation rule for every
// serve.fields / serve.labels and serve.annotations entry declaring type: enum with a
// non-empty enum list. See "Enum fields are validated automatically" at
// https://orkestra.sh/docs/reference/schema/katalog/validation#enum-fields-are-validated-automatically for the full
// rationale.
//
// The exists gate is always added regardless of required: membership and
// presence are independent — RequiredServeFieldRules alone decides whether a
// field is mandatory, this only decides whether its value (if any) is
// valid.
func (c *CRDEntry) EnumServeFieldRules() []ValidationRule {
	var rules []ValidationRule
	for _, ref := range c.allServeFieldRefs() {
		cfgType := strings.ToLower(ref.cfg.Type)
		if cfgType != "enum" || len(ref.cfg.Enum) == 0 {
			continue
		}
		when := append([]Condition{{Field: ref.field, Operator: ConditionExists}}, ref.cfg.When...)
		rules = append(rules, ValidationRule{
			Field:    ref.field,
			Link:     ref.link,
			Operator: ConditionIn,
			Value:    strings.Join(ref.cfg.Enum, ","),
			Message:  ref.label + " must be one of: " + strings.Join(ref.cfg.Enum, ", "),
			Action:   ValidationActionDeny,
			When:     when,
			Or:       ref.cfg.Or,
		})
	}
	return rules
}

// ServeFieldMutationRules synthesizes an implicit mutation rule for every
// serve.fields / serve.labels and serve.annotations entry declaring a default: or
// override: value. See "Default and Override fields are enforced automatically" at
// https://orkestra.sh/docs/reference/schema/katalog/mutation#default-and-override-fields-are-enforced-automatically
// for the full rationale.
//
// For serve.fields, only override: is honored — spec fields carry their defaults
// from the CRD schema. For serve.labels and serve.annotations, both default: and
// override: synthesize a rule.
func (c *CRDEntry) ServeFieldMutationRules() []MutationRule {
	if c == nil || c.Serve == nil {
		return nil
	}

	var rules []MutationRule
	// serve.fields
	for name, cfg := range c.ServeFields() {
		if !cfg.HasOverride() {
			continue
		}
		field := cfg.Path
		if field == "" {
			field = "spec." + name
		}

		// resolve default/override
		if cfg.Override != nil {
			cfg.Default = nil
		}

		rules = append(rules, MutationRule{
			Field:     field,
			Default:   cfg.Default,
			Override:  cfg.Override,
			ValueType: cfg.Type,
			When:      cfg.When,
			Or:        cfg.Or,
		})
	}

	// serve.labels
	for name, cfg := range c.ServeLabels() {
		if !cfg.HasDefault() && !cfg.HasOverride() {
			continue
		}

		// resolve default/override
		if cfg.Override != nil {
			cfg.Default = nil
		}
		rules = append(rules, MutationRule{
			Field:     fmt.Sprintf(`{{ getLabel . %q }}`, name),
			Default:   cfg.Default,
			Override:  cfg.Override,
			ValueType: cfg.Type,
			When:      cfg.When,
			Or:        cfg.Or,
		})
	}

	// serve.anotations
	for name, cfg := range c.ServeAnnotations() {
		if !cfg.HasDefault() && !cfg.HasOverride() {
			continue
		}

		// resolve default/override
		if cfg.Override != nil {
			cfg.Default = nil
		}
		rules = append(rules, MutationRule{
			Field:     fmt.Sprintf(`{{ getAnnotation . %q }}`, name),
			Default:   cfg.Default,
			Override:  cfg.Override,
			ValueType: cfg.Type,
			When:      cfg.When,
			Or:        cfg.Or,
		})
	}

	return rules
}

type SynthDedup struct {
	HasValidation   bool
	HasMutation     bool
	ValidationRules []ValidationRule
	MutationRules   []MutationRule
}

// DeduplicateSynthesizedServeRules removes synthesized rules that are
// already present — either because this is a bundle.yaml that baked them in at
// generate time (ork generate bundle calls SerializeExpanded, which serializes
// the post-synthesis state), or because a prior load pass already ran synthesis.
// Synthesis is deterministic, so an exact match is always a safe duplicate to drop.
func (c *CRDEntry) DeduplicateSynthesizedServeRules(synth SynthDedup) {
	if c == nil {
		return
	}

	switch {
	case synth.HasValidation:
		var toAddVal []ValidationRule
		for _, s := range synth.ValidationRules {
			dup := false
			for _, existing := range c.Validation.Rules {
				if reflect.DeepEqual(s, existing) {
					dup = true
					break
				}
			}
			if !dup {
				toAddVal = append(toAddVal, s)
			}
		}
		if len(toAddVal) > 0 {
			c.Validation.Rules = append(toAddVal, c.Validation.Rules...)
		}
	case synth.HasMutation:
		var toAddMut []MutationRule
		for _, s := range synth.MutationRules {
			dup := false
			for _, existing := range c.Mutation.Rules {
				if reflect.DeepEqual(s, existing) {
					dup = true
					break
				}
			}
			if !dup {
				toAddMut = append(toAddMut, s)
			}
		}
		if len(toAddMut) > 0 {
			c.Mutation.Rules = append(toAddMut, c.Mutation.Rules...)
		}

	}
}
