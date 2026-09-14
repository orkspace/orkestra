// pkg/types/types_networkpolicy.go
package types

// NetworkPolicyTemplateSource declares one NetworkPolicy to be managed by Orkestra.
//
// Usage patterns:
//
// 1. Inline spec — deny-all ingress:
//
//	onCreate:
//	  networkPolicies:
//	    - name: "{{ .metadata.name }}-deny-all"
//	      podSelector: {}
//	      ingress: []
//	      reconcile: true
//
// 2. Allow same-namespace ingress:
//
//	onCreate:
//	  networkPolicies:
//	    - name: "{{ .metadata.name }}-allow-same-ns"
//	      podSelector: {}
//	      ingress:
//	        - from:
//	            - podSelector: {}
//
// 3. Copy from existing NetworkPolicy:
//
//	onCreate:
//	  networkPolicies:
//	    - name: baseline-policy
//	      fromNetworkPolicy: org-baseline-policy
//	      fromNamespace: platform
//
// 4. Copy to multiple namespaces:
//
//	onCreate:
//	  networkPolicies:
//	    - name: deny-all
//	      fromNetworkPolicy: org-deny-all
//	      fromNamespace: platform
//	      toNamespaces:
//	        - "{{ .metadata.namespace }}"
//	        - staging
type NetworkPolicyTemplateSource struct {
	// Name — NetworkPolicy name.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace — target namespace.
	// Default: "{{ .metadata.namespace }}"
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// ToNamespaces — create one copy in each listed namespace.
	// Each element supports template expressions.
	ToNamespaces []string `yaml:"toNamespaces,omitempty" json:"toNamespaces,omitempty"`

	// FromNetworkPolicy — name of an existing NetworkPolicy to copy spec from.
	// When set, Orkestra reads this NetworkPolicy at reconcile time and copies its spec.
	FromNetworkPolicy string `yaml:"fromNetworkPolicy,omitempty" json:"fromNetworkPolicy,omitempty"`

	// FromNamespace — namespace where FromNetworkPolicy lives.
	// Default: same namespace as the CR.
	FromNamespace string `yaml:"fromNamespace,omitempty" json:"fromNamespace,omitempty"`

	// PodSelector — selects the pods this policy applies to.
	// Empty map ({}) selects all pods in the namespace.
	// Values support template expressions.
	PodSelector map[string]string `yaml:"podSelector,omitempty" json:"podSelector,omitempty"`

	// Ingress — list of ingress rules. Empty slice denies all ingress traffic.
	Ingress []NetworkPolicyIngressRule `yaml:"ingress,omitempty" json:"ingress,omitempty"`

	// Egress — list of egress rules. Omit to leave egress unmanaged by this policy.
	Egress []NetworkPolicyEgressRule `yaml:"egress,omitempty" json:"egress,omitempty"`

	// PolicyTypes — which policy types to enforce. Auto-derived when empty:
	// "Ingress" added when Ingress field is present; "Egress" added when Egress is present.
	PolicyTypes []string `yaml:"policyTypes,omitempty" json:"policyTypes,omitempty"`

	// Labels — applied to NetworkPolicy metadata.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`

	// Conditions (when:) — all must pass for this resource to be applied.
	Conditions []Condition `yaml:"when,omitempty" json:"when,omitempty"`

	// Or — at least one must pass.
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// Profile — named NetworkPolicy preset. Expands into ingress/egress rules and policy types.
	// Allowed values: deny-all, deny-all-ingress, deny-all-egress, allow-same-namespace, allow-dns-egress.
	// Mutually exclusive with Ingress/Egress/PolicyTypes — set profile or explicit rules, not both.
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`

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

// NetworkPolicyIngressRule describes one ingress rule.
type NetworkPolicyIngressRule struct {
	From  []NetworkPolicyPeer `yaml:"from,omitempty" json:"from,omitempty"`
	Ports []NetworkPolicyPort `yaml:"ports,omitempty" json:"ports,omitempty"`
}

// NetworkPolicyEgressRule describes one egress rule.
type NetworkPolicyEgressRule struct {
	To    []NetworkPolicyPeer `yaml:"to,omitempty" json:"to,omitempty"`
	Ports []NetworkPolicyPort `yaml:"ports,omitempty" json:"ports,omitempty"`
}

// NetworkPolicyPeer selects a set of pods or namespaces.
type NetworkPolicyPeer struct {
	// PodSelector selects pods within the namespace. Empty = all pods.
	PodSelector map[string]string `yaml:"podSelector,omitempty" json:"podSelector,omitempty"`
	// NamespaceSelector selects namespaces. Empty = all namespaces.
	NamespaceSelector map[string]string `yaml:"namespaceSelector,omitempty" json:"namespaceSelector,omitempty"`
	// IPBlock selects a CIDR range.
	IPBlock *NetworkPolicyIPBlock `yaml:"ipBlock,omitempty" json:"ipBlock,omitempty"`
}

// NetworkPolicyIPBlock describes an IP CIDR range.
type NetworkPolicyIPBlock struct {
	CIDR   string   `yaml:"cidr" json:"cidr"`
	Except []string `yaml:"except,omitempty" json:"except,omitempty"`
}

// NetworkPolicyPort describes a port allowed by a rule.
type NetworkPolicyPort struct {
	// Protocol — TCP (default), UDP, or SCTP.
	Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	// Port — port number or named port. Supports template expressions.
	Port string `yaml:"port,omitempty" json:"port,omitempty"`
}
