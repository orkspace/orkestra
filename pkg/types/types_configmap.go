// pkg/types/types_configmap.go
package types

// ── ConfigMap ─────────────────────────────────────────────────────────────────

// ConfigMapTemplateSource declares one ConfigMap to be managed by Orkestra.
//
// ConfigMap data values are static — template expressions are not evaluated
// in ConfigMap data entries. For dynamic configuration, use a custom Go hook.
//
// Example:
//
//	onCreate:
//	  configMaps:
//	    - name: "{{ .metadata.name }}-config"
//	      data:
//	        LOG_LEVEL: info
//	        MAX_CONNECTIONS: "100"
type ConfigMapTemplateSource struct {
	// Name — ConfigMap name.
	// Default when omitted: "{{ .metadata.name }}-config"
	Name string `yaml:"name,omitempty" json:"name,omitempty" validate:"omitempty"`

	// Namespace — target namespace.
	// Default when omitted: "{{ .metadata.namespace }}"
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty" validate:"omitempty"`

	// ToNamespaces - a list of target namespaces
	// Default when omitted: "{{ .metadata.namespace }}"
	ToNamespaces []string `yaml:"toNamespaces,omitempty" json:"toNamespaces,omitempty" validate:"omitempty"`

	// Data — static key-value configuration entries.
	// Values are plain strings — template expressions are not supported here.
	Data map[string]string `yaml:"data,omitempty" json:"data,omitempty" validate:"omitempty"`

	// Labels — applied to ConfigMap metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty" validate:"omitempty"`
	// FromConfigMap — name of an existing ConfigMap to copy data from.
	// Orkestra reads this at reconcile time — copies stay in sync with the source.
	FromConfigMap string `yaml:"fromConfigMap,omitempty" json:"fromConfigMap,omitempty" validate:"omitempty"`

	// FromNamespace — namespace where FromConfigMap lives.
	// Default: same namespace as the CR.
	FromNamespace string `yaml:"fromNamespace,omitempty" json:"fromNamespace,omitempty" validate:"omitempty"`

	// Reconcile: true — also apply this declaration as drift correction on every
	// reconcile. Equivalent to declaring the same entry under both onCreate and
	// onReconcile. When false (default), only runs on onCreate (idempotent create).
	Reconcile bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty" validate:"omitempty"`

	// Conditions declares the set of runtime predicates that must all evaluate to
	// true for this resource template to be applied during reconciliation.
	//
	// Each condition inspects a field on the live Custom Resource using dot-notation
	// (e.g. "spec.enabled", "metadata.labels.tier") and compares it against a value
	// using the chosen operator. All conditions in the list are AND‑ed together.
	//
	// If any condition fails, the resource is skipped for that reconcile cycle.
	// This is not an error — it simply means "do not create/update this resource
	// right now". This enables expressive, data‑driven orchestration such as:
	//
	//   when:
	//     - field: spec.exposePublicly
	//       equals: "true"
	//     - field: spec.environment
	//       prefix: "prod"
	//
	// Conditions allow templates to be selectively activated based on the CR's
	// state, enabling dynamic topologies, feature flags, environment‑specific
	// behavior, and conditional provisioning without writing Go code.
	Conditions []Condition `yaml:"when,omitempty" json:"when,omitempty"`
	// ForEach declares dynamic expansion over a list field.
	// When set, one source declaration becomes N declarations — one per list element.
	// .item and .<as> are available in template expressions within this declaration.
	ForEach *ForEachSpec `yaml:"forEach,omitempty" json:"forEach,omitempty"`

	// Or holds OR conditions — at least one must pass for this resource to be created.
	// Works alongside the existing Conditions (when:) field which uses AND semantics.
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string `json:"sleep,omitempty" yaml:"sleep,omitempty"`

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool `yaml:"forceConflict,omitempty" json:"forceConflict,omitempty"`
}
