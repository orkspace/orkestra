// pkg/types/types_resourcequota.go
package types

// ResourceQuotaTemplateSource declares one ResourceQuota to be managed by Orkestra.
//
// Usage patterns:
//
// 1. Inline quota:
//
//	onCreate:
//	  resourceQuotas:
//	    - name: "{{ .metadata.name }}-quota"
//	      hard:
//	        cpu: "4"
//	        memory: 8Gi
//	        pods: "20"
//	      reconcile: true
//
// 2. Environment-sized quota with template expressions:
//
//	onCreate:
//	  resourceQuotas:
//	    - name: "{{ .metadata.name }}-quota"
//	      hard:
//	        cpu: '{{ if eq .spec.env "production" }}16{{ else }}2{{ end }}'
//	        memory: '{{ if eq .spec.env "production" }}32Gi{{ else }}4Gi{{ end }}'
//
// 3. Copy from existing ResourceQuota:
//
//	onCreate:
//	  resourceQuotas:
//	    - name: team-quota
//	      fromResourceQuota: org-default-quota
//	      fromNamespace: platform
//
// 4. Copy to multiple namespaces:
//
//	onCreate:
//	  resourceQuotas:
//	    - name: team-quota
//	      fromResourceQuota: org-default-quota
//	      fromNamespace: platform
//	      toNamespaces:
//	        - "{{ .metadata.namespace }}"
//	        - staging
type ResourceQuotaTemplateSource struct {
	// Name — ResourceQuota name.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace — target namespace.
	// Default: "{{ .metadata.namespace }}"
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// ToNamespaces — create one copy in each listed namespace.
	// Each element supports template expressions.
	ToNamespaces []string `yaml:"toNamespaces,omitempty" json:"toNamespaces,omitempty"`

	// FromResourceQuota — name of an existing ResourceQuota to copy from.
	// When set, Orkestra reads this ResourceQuota at reconcile time and copies its hard limits.
	FromResourceQuota string `yaml:"fromResourceQuota,omitempty" json:"fromResourceQuota,omitempty"`

	// FromNamespace — namespace where FromResourceQuota lives.
	// Default: same namespace as the CR.
	FromNamespace string `yaml:"fromNamespace,omitempty" json:"fromNamespace,omitempty"`

	// Hard — resource limits. Keys are Kubernetes resource names (cpu, memory, pods, etc.).
	// Values are resource quantities. Supports template expressions.
	// See: https://kubernetes.io/docs/concepts/policy/resource-quotas/#compute-resource-quota
	Hard map[string]string `yaml:"hard,omitempty" json:"hard,omitempty"`

	// Labels — applied to ResourceQuota metadata.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`

	// Conditions (when:) — all must pass for this resource to be applied.
	Conditions []Condition `yaml:"when,omitempty" json:"when,omitempty"`

	// Or — at least one must pass.
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// Reconcile: true — sync on every reconcile (drift correction).
	Reconcile bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`

	// ForEach — expand this entry once per item in a list or map field.
	ForEach *ForEachSpec `yaml:"forEach,omitempty" json:"forEach,omitempty"`

	// Profile — named resource quota preset. Expands into hard limits.
	// Allowed values: small, medium, large, xlarge.
	// Mutually exclusive with Hard — set one or the other, not both.
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`

	// Sleep injects an artificial delay.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string `json:"sleep,omitempty" yaml:"sleep,omitempty"`

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool `yaml:"forceConflict,omitempty" json:"forceConflict,omitempty"`
}
