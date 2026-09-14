// pkg/types/types_pdb.go
package types

// ── PodDisruptionBudget ───────────────────────────────────────────────────────

// PDBTemplateSource declares one PodDisruptionBudget to be managed by Orkestra.
//
// Example:
//
//	onReconcile:
//	  pdb:
//	    - name: "{{ .metadata.name }}-pdb"
//	      minAvailable: "1"
//	      selector:
//	        app: "{{ .metadata.name }}"
//	      forEach:
//	        field: spec.services
//	        as: item
type PDBTemplateSource struct {
	// Name — PDB resource name. Default: "{{ .metadata.name }}-pdb"
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace — target namespace. Default: CR namespace.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Selector — label selector identifying the pods this PDB protects.
	// Keys are static; values support template expressions.
	Selector Labels `yaml:"selector,omitempty" json:"selector,omitempty"`

	// MinAvailable — minimum number of pods that must remain available.
	// Accepts integer strings ("1") or percentage strings ("50%").
	// Mutually exclusive with MaxUnavailable.
	MinAvailable string `yaml:"minAvailable,omitempty" json:"minAvailable,omitempty"`

	// MaxUnavailable — maximum number of pods that may be unavailable.
	// Accepts integer strings ("1") or percentage strings ("25%").
	// Mutually exclusive with MinAvailable.
	MaxUnavailable string `yaml:"maxUnavailable,omitempty" json:"maxUnavailable,omitempty"`

	// Labels applied to PDB metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`

	// Behavior — disruption limit configuration.
	// Set behavior.profile for a named preset, or declare minAvailable/maxUnavailable explicitly.
	// Profile and explicit fields are mutually exclusive.
	//
	//	behavior:
	//	  profile: zero-downtime
	Behavior *PDBBehavior `yaml:"behavior,omitempty" json:"behavior,omitempty"`

	// Reconcile: true — also apply this declaration as drift correction on every
	// reconcile. Equivalent to declaring the same entry under both onCreate and
	// onReconcile. When false (default), only runs on onCreate (idempotent create).
	Reconcile bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`

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

	// Or holds OR conditions — at least one must pass for this resource to be created.
	// Works alongside the existing Conditions (when:) field which uses AND semantics.
	//
	//	or:
	//	  - field: spec.tier
	//	    equals: pro
	//	  - field: spec.tier
	//	    equals: enterprise
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// ForEach declares dynamic expansion over a list field.
	// When set, one source declaration becomes N declarations — one per list element.
	// .item and .<as> are available in template expressions within this declaration.
	//
	//	forEach:
	//	  field: spec.services
	//	  as: item
	ForEach *ForEachSpec `yaml:"forEach,omitempty" json:"forEach,omitempty"`

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string `json:"sleep,omitempty" yaml:"sleep,omitempty"`

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool `yaml:"forceConflict,omitempty" json:"forceConflict,omitempty"`
}
