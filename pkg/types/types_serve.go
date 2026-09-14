package types

import (
	"fmt"
	"slices"
	"strings"
)

// ServeDenyReason is returned by TokenAllowed to let the caller compose a
// precise 403 message without duplicating the check logic.
type ServeDenyReason int

const (
	ServeDenyReasonNone         ServeDenyReason = iota // allowed
	ServeDenyReasonUnknownToken                        // token not in tokens map
	ServeDenyReasonNamespace                           // namespace not in token's namespaces
	ServeDenyReasonOperation                           // operation not in token's permissions
)

// ServeConfig declares serve exposure settings for a CRD entry.
type ServeConfig struct {
	// Enabled surfaces this CRD in the Control Center as a self-service form.
	// Requires gateway.api.enabled: true on the Katalog.
	// Default: false.
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// Modes controls which apply modes are allowed for this CRD.
	// Both default to true for backward compatibility.
	Modes *ServeModes `yaml:"modes,omitempty" json:"modes,omitempty"`

	// Include is a path (relative to the katalog file) to a YAML file with a
	// "fields:" map and/or a "labels:" or "annotations:" block (same shape as the
	// inline equivalents below). Expanded at load time — the result is merged
	// into Fields, Labels and Annotations respectively, with inline entries
	// taking precedence per key.
	Include string `yaml:"include,omitempty" json:"include,omitempty"`

	// Fields provides presentation hints layered on top of the CRD's OpenAPI
	// schema. Each key matches a field path in spec. Hints are merged with the
	// schema at GET /api/v1/schema/{kind} time — they do not replace the schema.
	Fields map[string]ServeFieldConfig `yaml:"fields,omitempty" json:"fields,omitempty"`

	// Labels exposes label keys as self-service form fields, written to
	// metadata.labels on apply. Declare type explicitly — no CRD schema to infer from.
	Labels map[string]ServeFieldConfig `yaml:"labels,omitempty" json:"labels,omitempty"`

	// Annotations exposes annotation keys as self-service form fields, written to
	// metadata.annotations on apply. Declare type explicitly — no CRD schema to infer from.
	Annotations map[string]ServeFieldConfig `yaml:"annotations,omitempty" json:"annotations,omitempty"`

	// Ignore lists spec field names hidden from the serve form.
	Ignore []string `yaml:"ignore,omitempty" json:"ignore,omitempty"`

	// Title is the human-readable name shown in the Control Center catalog.
	// Defaults to kind when not set.
	Title string `yaml:"title,omitempty" json:"title,omitempty"`

	// Category is a catalog label used when listing available schemas
	// via GET /api/v1/schema/. Example: "Compute", "Data", "Security".
	Category string `yaml:"category,omitempty" json:"category,omitempty"`

	// Description is a short human-readable summary shown in the service catalog.
	// Falls back to the CRD-level description when not set.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Target is the caller-facing identifier for this CRD in the Gateway API.
	// Accepts a plain string shorthand ("myapp") or a named map of target entries.
	// In the map form exactly one entry must have primary: true — that entry's key
	// is the primary target name and all other entries are aliases.
	// Defaults to the lowercased kind when not set.
	Target ServeTargetValue `yaml:"target,omitempty" json:"target,omitempty"`

	// Apply configures apply-time behaviour for all targets (fallback).
	Apply *ServeApplyConfig `yaml:"apply,omitempty" json:"apply,omitempty"`

	// MatchFields is a list of dot-notation field paths that link a full CR
	// to this target. When a CR contains all the specified fields, it is
	// automatically routed to this target.
	//
	// Used to enable target-level controls (tokens, response config, permissions)
	// for CRs submitted in full CR mode. Without matchFields, full CR mode
	// bypasses target-level controls.
	//
	// Each target must have a unique match list. ork validate enforces this.
	// At least one match field is recommended when cr mode is disabled.
	//
	// Maximum: 3 fields
	//
	// Example:
	//   matchFields:
	//     - spec.mealPlan
	//     - spec.kitchenConfig
	MatchFields []string `yaml:"matchFields,omitempty" json:"matchFields,omitempty"`

	// Name is a template expression the Gateway API resolves server-side to
	// decide the CR's metadata.name — e.g. '{{ repoSlug .spec.repository }}'.
	// Once set, it always wins over whatever (if anything) the client sent.
	// Applies regardless of CRD scope (metadata.name exists either way),
	// but is optional, unlike Namespace below: most CRDs still want the
	// caller to choose a name (multiple concurrent instances of one repo —
	// PR previews, ephemeral environments). Set this only when instances are
	// 1:1 with some other identity the caller already supplies (repository,
	// team) and redeploys should update the same CR in place rather than
	// create a new one — a stable environment where only the image tag
	// or few configuration changes between deploys.
	//
	// When unset, the Gateway API requires the caller to supply metadata.name
	// and rejects the request with a structured violation if it's empty,
	// rather than leaving it to the Kubernetes API server's own generic
	// rejection.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace is a template expression the Gateway API resolves server-side
	// to decide which namespace a new CR is created in — e.g. '{{ teamName }}'.
	// Same resolution mechanics as Name above, but only applies to namespaced
	// CRDs, and — unlike Name — is required rather than optional: the
	// Control Center form and any Gateway API caller never need to render or
	// submit namespace themselves. A plain literal (no template) is valid
	// too, for a CRD whose instances always land in one fixed namespace.
	//
	// Required when the CRD is namespaced (the default) and serve.enabled is
	// true — see Katalog.validateServeNamespace. Meaningless, and rejected at
	// load time, on a cluster-scoped CRD (namespaced: false) — there's no
	// namespace to resolve into.
	//
	// This only affects the Gateway API (POST /api/v1/apply). Raw kubectl
	// callers are unaffected: kubectl always resolves some namespace
	// client-side before a request reaches the API server (typically
	// "default"), so there is never a genuinely empty namespace for a
	// webhook to fill in the way an omitted JSON field lets the Gateway API
	// detect intent — deliberately not implemented as a mutating webhook
	// default for that reason.
	//
	// Resolves into an existing namespace; it does not create one. The
	// platform team provisions whatever namespace(s) this can resolve to
	// ahead of time (setup.apply in e2e, real onboarding in production) —
	// same as a cluster-scoped CRD's onCreate provisioning a namespace as a
	// child resource is a different, complementary answer to the same
	// "developer shouldn't have to pick a namespace" problem.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Config declares optional response shaping applied by the gateway when
	// returning CR data to callers. Evaluated at request time against the
	// fetched CR — no additional Kubernetes API calls are made.
	// Nil config returns the CR unchanged.
	Config *ServeConfigSettings `yaml:"config,omitempty" json:"config,omitempty"`

	// Tokens maps gateway token names to the operations they may perform on this
	// CRD and, optionally, the namespaces they may access.
	// When empty, any valid gateway token may perform any operation on this CRD.
	// ork validate confirms every token name here matches an entry in gateway.api.auth.tokens.
	Tokens map[string]ServeTokenPermissions `yaml:"tokens,omitempty" json:"tokens,omitempty"`

	// Clusters is the list of cluster names this CRD is allowed to route to.
	// Each entry is either a static name from gateway.clusters or a template
	// expression evaluated against the intent at apply time.
	// When absent, intents apply to the local cluster only.
	// Every apply fans out to all declared clusters unless a target entry narrows it.
	Clusters []string `yaml:"clusters,omitempty" json:"clusters,omitempty"`
}

// HasClusters reports whether any cluster routing is declared.
func (s *ServeConfig) HasClusters() bool {
	return s != nil && len(s.Clusters) > 0
}

// ClusterAllowed reports whether name is in the declared clusters list.
// Static comparison only — template entries are not evaluated here.
func (s *ServeConfig) ClusterAllowed(name string) bool {
	for _, c := range s.Clusters {
		if c == name {
			return true
		}
	}
	return false
}

type ServeModes struct {
	// Target mode — submit fields with a target identifier.
	// Default: true.
	Target *bool `yaml:"target,omitempty" json:"target,omitempty"`

	// CR mode — submit a full Kubernetes CR (apiVersion + kind).
	// Default: true.
	CR *bool `yaml:"cr,omitempty" json:"cr,omitempty"`
}

// TargetModeEnabled returns true if target mode is allowed.
// Default: true
func (s *ServeModes) TargetModeEnabled() bool {
	if s == nil || s.Target == nil {
		return true // default
	}
	return *s.Target
}

// CRModeEnabled returns true if CR mode is allowed.
// Default: true
func (s *ServeModes) CRModeEnabled() bool {
	if s == nil || s.CR == nil {
		return true // default
	}
	return *s.CR
}

// TargetModeEnabled returns true if target mode is allowed.
func (s *ServeConfig) TargetModeEnabled() bool {
	if s == nil || s.Modes == nil || s.Modes.Target == nil {
		return true // default
	}
	return *s.Modes.Target
}

// FullCRModeEnabled returns true if full CR mode is allowed.
func (s *ServeConfig) FullCRModeEnabled() bool {
	if s == nil || s.Modes == nil || s.Modes.CR == nil {
		return true // default
	}
	return *s.Modes.CR
}

// ServeAliasConfigSettings is the config block on a target entry.
// Mirrors ServeConfigSettings but kept separate so alias-only settings can be
// added in future without touching the CRD-level type.
type ServeAliasConfigSettings struct {
	// Response controls what callers see when the gateway returns CR data via this alias.
	Response *ServeResponseConfig `yaml:"response,omitempty" json:"response,omitempty"`
}

// ServeFieldConfig holds display hints for one spec field in the serve form.
type ServeFieldConfig struct {
	// Label overrides the field name in the rendered form.
	Label string `yaml:"label,omitempty" json:"label,omitempty"`

	// Placeholder is the input placeholder text.
	Placeholder string `yaml:"placeholder,omitempty" json:"placeholder,omitempty"`

	// Hint is descriptive text rendered below the field.
	Hint string `yaml:"hint,omitempty" json:"hint,omitempty"`

	// Order controls position in the rendered form. Lower values appear first.
	// Fields with no order (0) appear after all explicitly ordered fields —
	// any number of fields may leave it unset, since 0 means "no preference,"
	// not a real position.
	//
	// Not just form layout: allServeFieldRefs sorts by Order to decide
	// synthesized validation-rule priority too (see RuleViolation.Field /
	// ValidationResult.DenialMessage — only the first violation is reported
	// as the headline denial reason), so the field a developer sees first is
	// also the one whose error they see first when several fail at once.
	// Two fields on the same CRD sharing a non-zero Order is a load-time
	// error (see Katalog.validateServeFieldOrder) for exactly this reason.
	Order int `yaml:"order,omitempty" json:"order,omitempty"`

	// Category is a section heading for visual grouping. Fields sharing a category
	// are rendered under the same heading. Works with When — if all fields in
	// a category are hidden, the heading is also hidden.
	Category string `yaml:"category,omitempty" json:"category,omitempty"`

	// When is a list of conditions that must ALL be true for this field to be
	// visible. Evaluated client-side as the user fills the form. An empty When
	// means the field is always visible.
	// Supports: equals, notEquals, time, dayOfWeek, cron, negate — same as
	// template source when: blocks.
	When []Condition `yaml:"when,omitempty" json:"when,omitempty"`

	// Or is a list of conditions where at least ONE must be true for the
	// field to be visible. OR counterpart to When (AND).
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// Required, when true, marks the field as mandatory in the serve form —
	// the browser enforces this natively (asterisk on the label, form cannot
	// be submitted while Empty() — and is also enforced server-side: an
	// implicit exists validation rule is synthesized automatically at
	// katalog load time (see CRDEntry.RequiredServeFieldRules), covering every
	// client of the Gateway API, not just the Control Center form. No matching
	// validation.rules entry needs to be hand-written.
	Required bool `yaml:"required,omitempty" json:"required,omitempty"`

	// Disabled, when non-empty, renders the field greyed out with this string
	// as the reason. The field is excluded from form submission.
	// Use for maintenance windows or temporarily locked fields.
	Disabled string `yaml:"disabled,omitempty" json:"disabled,omitempty"`

	// Type is required for labels/annotations entries which have
	// no CRD schema to infer type from. Ignored for Fields entries, which
	// always infer type from the CRD's OpenAPI schema.
	// Supported: string (default), integer, number, boolean, enum.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// Enum lists valid values when Type == "enum". Required in that case.
	Enum []string `yaml:"enum,omitempty" json:"enum,omitempty"`

	// Default — set only if the field is absent or empty.
	// Supports template expressions. This is used to synthesize mutation rules for this field
	//  Ignored for Fields entries, which always infer default from the CRD's OpenAPI schema.
	// Used by `ServeFieldMutationRules`
	Default interface{} `yaml:"default,omitempty"` // accepts int, bool, string from YAML

	// Override — always set, regardless of current value.
	// Supports template expressions. This is used to synthesize mutation rules for this field
	// Ignored for Fields entries, which always infer override from the CRD's OpenAPI schema.
	// Used by `ServeFieldMutationRules`
	Override interface{} `yaml:"override,omitempty"` // accepts int, bool, string from YAML

	// Path is the dot-notation path in the CRD spec where this field belongs.
	// Example: "app.repository", "scaling.minReplicas"
	// When set, the field is mapped to this nested path.
	// When empty, the field name is used as the path (flat).
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// Value is a template expression that transforms the submitted field value
	// before writing it to the spec path. The expression has access to:
	//   .value   — the raw submitted value for this field
	//   .request — the full raw intent payload (cross-field reads)
	//
	// When Value is present, its result is written to Path (or the field name
	// if Path is absent). When absent, the raw submitted value is written as-is.
	//
	// Value and Values are mutually exclusive.
	Value string `yaml:"value,omitempty" json:"value,omitempty"`

	// Values is a fanout map from dot-notation spec paths to template expressions.
	// Use when one submitted field must be split into multiple CR spec fields.
	//
	// Example — caller submits "image: ghcr.io/myorg/app:v1.2.3"; katalog fans
	// it out to image.registry, image.repository, image.tag:
	//
	//   values:
	//     image.registry:   '{{ imageRegistry   .value }}'
	//     image.repository: '{{ imageRepository .value }}'
	//     image.tag:        '{{ imageTag        .value }}'
	//
	// All map keys must be dot-notation (contain at least one dot).
	// Path is ignored when Values is present.
	// Value and Values are mutually exclusive.
	Values map[string]string `yaml:"values,omitempty" json:"values,omitempty"`
}

// ServeConfigSettings is the container for gateway-level CRD configuration.
// Named with the parent prefix to avoid colliding with the package-level
// Config types while keeping the YAML key simply "config:".
type ServeConfigSettings struct {
	// Response controls what callers see when the gateway returns CR data.
	// See ServeResponseConfig for full documentation.
	Response *ServeResponseConfig `yaml:"response,omitempty" json:"response,omitempty"`
}

// IsValidServeFieldType reports whether t is a valid ServeFieldConfig.Type value.
// "" (omitted) is valid — it means the default, string.
func IsValidServeFieldType(t string) bool {
	switch t {
	case "", "string", "integer", "number", "boolean", "enum":
		return true
	default:
		return false
	}
}

// FieldType returns the configured type for this field or default to "string".
func (f ServeFieldConfig) FieldType() string {
	if f.Type != "" {
		return f.Type
	}
	return "string"
}

// SpecPath returns the dot-notation path to use in the CRD spec.
// If Path is set, use Path. Otherwise, use the field name.
func (f ServeFieldConfig) SpecPath(name string) string {
	if f.Path != "" {
		return f.Path
	}
	return name
}

// IsNested returns true if the spec path contains a dot.
func (f ServeFieldConfig) IsNested(name string) bool {
	return strings.Contains(f.SpecPath(name), ".")
}

// HasValue reports whether a single-destination transform expression is declared.
func (f ServeFieldConfig) HasValue() bool {
	return f.Value != ""
}

// HasValues reports whether a multi-destination fanout map is declared.
func (f ServeFieldConfig) HasValues() bool {
	return len(f.Values) > 0
}

// IsTranslated reports whether this field has any translation expression
// (either value or values). Plain fields with neither are written as-is.
func (f ServeFieldConfig) IsTranslated() bool {
	return f.HasValue() || f.HasValues()
}

// IsGateOnly reports whether this field is declared purely for intent gating —
// it has no path, no value, and no values, so it is never written to the CR.
// The field is still exposed in the serve form and accessible via .request in
// validation rules.
func (f ServeFieldConfig) IsGateOnly() bool {
	return f.Path == "" && f.Value == "" && len(f.Values) == 0
}

// HasSpecPath returns true if the spec path is set.
func (f ServeFieldConfig) HasSpecPath() bool {
	return f.Path != ""
}

// HasDefaultAndOverride reports whether both default and override are defined for this field
func (f ServeFieldConfig) HasDefaultAndOverride() bool {
	return f.Override != nil && f.Default != nil
}

// HasDefault returns true when default is set
func (f ServeFieldConfig) HasDefault() bool {
	return f.Default != nil
}

// HasOverride returns true when 0verride is set
func (f ServeFieldConfig) HasOverride() bool {
	return f.Override != nil
}

// HasTokenRestrictions reports whether any per-token access rules are declared.
// When false, any valid gateway token may access this CRD — backward-compatible
// with the previous model where tokens were only checked for existence.
func (i *ServeConfig) HasTokenRestrictions() bool {
	if i == nil {
		return false
	}
	return len(i.Tokens) > 0
}

// AllowedServeTokens returns a list of token names allowed for this serve configuration
func (i *ServeConfig) AllowedServeTokens() []string {
	if i == nil {
		return nil
	}
	var tokens []string
	for token := range i.Tokens {
		tokens = append(tokens, token)
	}
	return tokens
}

// TokenAllowed reports whether tokenName may perform op in namespace on this
// CRD for the given endpoint class.
//
// Returns (true, ServeDenyReasonNone) when allowed.
// Returns (false, reason) when denied; reason carries the specific cause so
// callers can compose precise error messages without re-implementing the logic.
// The check is intentionally three-stage for clarity in error messages:
//  1. Is the token listed at all?   → 403 unknown token
//  2. Is the namespace permitted?   → 403 namespace not allowed
//  3. Is the operation permitted?   → 403 operation not allowed
func (c *ServeConfig) TokenAllowed(
	tokenName, op, namespace string,
	class ServeEndpointClass,
) (bool, ServeDenyReason) {
	if !c.HasTokenRestrictions() {
		return true, ServeDenyReasonNone
	}

	perms, ok := c.Tokens[tokenName]
	if !ok {
		return false, ServeDenyReasonUnknownToken
	}

	// For schema endpoints, skip namespace check entirely.
	// Schema endpoints are cluster-scoped and don't have a namespace.
	if class != ServeClassSchema {
		if len(perms.Namespaces) > 0 && !slices.Contains(perms.Namespaces, namespace) {
			return false, ServeDenyReasonNamespace
		}
	}

	active := perms.activePerms(class)
	if len(active) == 0 {
		return false, ServeDenyReasonOperation
	}

	for _, p := range active {
		if p == ServeOpAll || p == op {
			return true, ServeDenyReasonNone
		}
	}
	return false, ServeDenyReasonOperation
}

// Message returns a human-readable denial message for use in HTTP responses
// and ork validate output.
func (r ServeDenyReason) Message(tokenName, op, kind, namespace string) string {
	switch r {
	case ServeDenyReasonUnknownToken:
		return fmt.Sprintf(
			"token %q is not allowed to access %q — not listed in serve.tokens",
			tokenName, kind,
		)
	case ServeDenyReasonNamespace:
		return fmt.Sprintf(
			"token %q is not allowed to access %q in namespace %q",
			tokenName, kind, namespace,
		)
	case ServeDenyReasonOperation:
		return fmt.Sprintf(
			"token %q lacks %q permission on %q",
			tokenName, op, kind,
		)
	default:
		return ""
	}
}
