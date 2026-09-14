// pkg/types/types_hpa.go
package types

// ── HorizontalPodAutoscaler ───────────────────────────────────────────────────

type ScaleTargetRef struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Name       string `yaml:"name" json:"name"`
}

// HPATemplateSource declares one HorizontalPodAutoscaler to be managed by Orkestra.
//
// Example:
//
//	onReconcile:
//	  hpa:
//	    - name: "{{ .metadata.name }}-hpa"
//	      deploymentRef: "{{ .metadata.name }}"
//	      minReplicas: "{{ .spec.minReplicas }}"
//	      maxReplicas: "{{ .spec.maxReplicas }}"
//	      targetCPUUtilizationPercentage: "80"
//	      forEach:
//	        field: spec.services
//	        as: item
type HPATemplateSource struct {
	// Name — HPA resource name. Default: "{{ .metadata.name }}-hpa"
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace — target namespace. Default: CR namespace.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// ScaleTargetRef — the target workload this HPA scales.
	// Supports Deployment, ReplicaSet, StatefulSet, or any scalable resource.
	// Supports template expressions: "{{ .metadata.name }}"
	//
	//	scaleTargetRef:
	//	  apiVersion: apps/v1
	//	  kind: Deployment
	//	  name: "{{ .metadata.name }}-deployment"
	ScaleTargetRef ScaleTargetRef `yaml:"scaleTargetRef,omitempty" json:"scaleTargetRef,omitempty"`

	// MinReplicas — minimum replica count as a string. Supports template expressions.
	MinReplicas string `yaml:"minReplicas,omitempty" json:"minReplicas,omitempty"`

	// MaxReplicas — maximum replica count as a string. Supports template expressions.
	MaxReplicas string `yaml:"maxReplicas,omitempty" json:"maxReplicas,omitempty"`

	// TargetCPUUtilizationPercentage — CPU utilization target (0-100). Supports templates.
	// When behavior.profile is set and this field is empty, the profile provides the default.
	TargetCPUUtilizationPercentage string `yaml:"targetCPUUtilizationPercentage,omitempty" json:"targetCPUUtilizationPercentage,omitempty"`

	// Behavior — scale-up and scale-down behavior configuration.
	// Set profile for a complete preset, or configure scaleUp/scaleDown explicitly.
	// Profile and explicit fields are mutually exclusive.
	//
	//	behavior:
	//	  profile: web
	//
	//	behavior:
	//	  scaleUp:
	//	    stabilizationWindowSeconds: 0
	//	  scaleDown:
	//	    stabilizationWindowSeconds: 300
	Behavior *HPABehavior `yaml:"behavior,omitempty" json:"behavior,omitempty"`

	// Labels applied to HPA metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`

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
