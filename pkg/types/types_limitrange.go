// pkg/types/types_limitrange.go
package types

// LimitRangeTemplateSource declares one LimitRange to be managed by Orkestra.
//
// Usage patterns:
//
// 1. Container defaults:
//
//	onCreate:
//	  limitRanges:
//	    - name: "{{ .metadata.name }}-limits"
//	      limits:
//	        - type: Container
//	          default:
//	            cpu: 500m
//	            memory: 512Mi
//	          defaultRequest:
//	            cpu: 250m
//	            memory: 256Mi
//	      reconcile: true
//
// 2. Environment-sized limits with template expressions:
//
//	onCreate:
//	  limitRanges:
//	    - name: "{{ .metadata.name }}-limits"
//	      limits:
//	        - type: Container
//	          default:
//	            cpu: '{{ if eq .spec.env "production" }}500m{{ else }}200m{{ end }}'
//	            memory: '{{ if eq .spec.env "production" }}512Mi{{ else }}256Mi{{ end }}'
//
// 3. Copy from existing LimitRange:
//
//	onCreate:
//	  limitRanges:
//	    - name: team-limits
//	      fromLimitRange: org-default-limits
//	      fromNamespace: platform
//
// 4. Copy to multiple namespaces:
//
//	onCreate:
//	  limitRanges:
//	    - name: team-limits
//	      fromLimitRange: org-default-limits
//	      fromNamespace: platform
//	      toNamespaces:
//	        - "{{ .metadata.namespace }}"
//	        - staging
type LimitRangeTemplateSource struct {
	// Name — LimitRange name.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace — target namespace.
	// Default: "{{ .metadata.namespace }}"
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// ToNamespaces — create one copy in each listed namespace.
	// Each element supports template expressions.
	ToNamespaces []string `yaml:"toNamespaces,omitempty" json:"toNamespaces,omitempty"`

	// FromLimitRange — name of an existing LimitRange to copy from.
	// When set, Orkestra reads this LimitRange at reconcile time and copies its limits.
	FromLimitRange string `yaml:"fromLimitRange,omitempty" json:"fromLimitRange,omitempty"`

	// FromNamespace — namespace where FromLimitRange lives.
	// Default: same namespace as the CR.
	FromNamespace string `yaml:"fromNamespace,omitempty" json:"fromNamespace,omitempty"`

	// Profile — named LimitRange preset. Expands into a Limits list at reconcile time.
	// Mutually exclusive with Limits — set one or the other, not both.
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`

	// Limits — the list of limit range items.
	// Each item applies to one type: Container, Pod, or PersistentVolumeClaim.
	Limits []LimitRangeItem `yaml:"limits,omitempty" json:"limits,omitempty"`

	// Labels — applied to LimitRange metadata.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`

	// Conditions (when:) — all must pass for this resource to be applied.
	Conditions []Condition `yaml:"when,omitempty" json:"when,omitempty"`

	// Or — at least one must pass.
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// Reconcile: true — sync on every reconcile (drift correction).
	Reconcile bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`

	// ForEach — expand this entry once per item in a list or map field.
	ForEach *ForEachSpec `yaml:"forEach,omitempty" json:"forEach,omitempty"`

	// Sleep injects an artificial delay.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string `json:"sleep,omitempty" yaml:"sleep,omitempty"`

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool `yaml:"forceConflict,omitempty" json:"forceConflict,omitempty"`
}

// LimitRangeItem sets constraints on one resource type within a namespace.
type LimitRangeItem struct {
	// Type — the resource type this item applies to.
	// One of: Container, Pod, PersistentVolumeClaim.
	Type string `yaml:"type" json:"type"`

	// Max — maximum amount of compute resources allowed.
	// Keys: cpu, memory, ephemeral-storage.
	Max map[string]string `yaml:"max,omitempty" json:"max,omitempty"`

	// Min — minimum amount of compute resources required.
	Min map[string]string `yaml:"min,omitempty" json:"min,omitempty"`

	// Default — default limit if not specified in the container spec.
	Default map[string]string `yaml:"default,omitempty" json:"default,omitempty"`

	// DefaultRequest — default resource request if not specified in the container spec.
	DefaultRequest map[string]string `yaml:"defaultRequest,omitempty" json:"defaultRequest,omitempty"`

	// MaxLimitRequestRatio — max ratio of limit to request for a resource.
	MaxLimitRequestRatio map[string]string `yaml:"maxLimitRequestRatio,omitempty" json:"maxLimitRequestRatio,omitempty"`
}
