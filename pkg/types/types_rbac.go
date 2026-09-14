// pkg/types/types_rbac.go
package types

// ── Role / RoleBinding ────────────────────────────────────────────────────────

// PolicyRuleSpec declares one RBAC policy rule.
// String values within slices support template expressions.
type PolicyRuleSpec struct {
	APIGroups     []string `yaml:"apiGroups,omitempty" json:"apiGroups,omitempty"`
	Resources     []string `yaml:"resources,omitempty" json:"resources,omitempty"`
	Verbs         []string `yaml:"verbs,omitempty" json:"verbs,omitempty"`
	ResourceNames []string `yaml:"resourceNames,omitempty" json:"resourceNames,omitempty"`
}

// SubjectSpec declares one RBAC subject for a RoleBinding.
// Name and Namespace support template expressions.
type SubjectSpec struct {
	Kind      string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Name      string `yaml:"name,omitempty" json:"name,omitempty"`
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

// RoleRefSpec names the Role (or ClusterRole) being bound.
// Name supports template expressions. Kind defaults to "Role" on a
// RoleBinding and "ClusterRole" on a ClusterRoleBinding when omitted.
type RoleRefSpec struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"` // Role | ClusterRole
}

// RoleTemplateSource declares one namespaced Role to be managed by Orkestra.
//
// Example:
//
//	onCreate:
//	  roles:
//	    - name: "{{ .metadata.name }}-role"
//	      namespace: "{{ .metadata.name }}-ns"
//	      rules:
//	        - apiGroups: ["apps"]
//	          resources: ["deployments"]
//	          verbs: ["get", "list", "watch", "update", "patch"]
//	          resourceNames: ["{{ .metadata.name }}"]
type RoleTemplateSource struct {
	// Name — Role name.
	// Default when omitted: "{{ .metadata.name }}-role"
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace — target namespace.
	// Default when omitted: "{{ .metadata.namespace }}"
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Labels — applied to Role metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`

	// Rules — the permissions granted by this Role. Required: at least one rule.
	//
	//	rules:
	//	  - apiGroups: ["apps"]
	//	    resources: ["deployments"]
	//	    verbs: ["get", "list", "watch", "update", "patch"]
	//	    resourceNames: ["{{ .metadata.name }}"]
	Rules []PolicyRuleSpec `yaml:"rules,omitempty" json:"rules,omitempty"`

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

	// Reconcile: true — also apply this declaration as drift correction on every
	// reconcile. Equivalent to declaring the same entry under both onCreate and
	// onReconcile. When false (default), only runs on onCreate (idempotent create).
	Reconcile bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`

	// ForEach declares dynamic expansion over a list field.
	// When set, one source declaration becomes N declarations — one per list element.
	// .item and .<as> are available in template expressions within this declaration.
	//
	//	forEach:
	//	  field: spec.regions
	//	  as: region
	ForEach *ForEachSpec `yaml:"forEach,omitempty" json:"forEach,omitempty"`

	// Or holds OR conditions — at least one must pass for this resource to be created.
	// Works alongside the existing Conditions (when:) field which uses AND semantics.
	//
	//	or:
	//	  - field: spec.tier
	//	    equals: pro
	//	  - field: spec.tier
	//	    equals: enterprise
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

// RoleBindingTemplateSource declares one RoleBinding to be managed by Orkestra.
//
// Example:
//
//	onCreate:
//	  roleBindings:
//	    - name: "{{ .metadata.name }}-rolebinding"
//	      namespace: "{{ .metadata.name }}-ns"
//	      roleRef:
//	        name: "{{ .metadata.name }}-role"
//	      subjects:
//	        - kind: ServiceAccount
//	          name: "{{ .metadata.name }}-sa"
//	          namespace: "{{ .metadata.name }}-ns"
type RoleBindingTemplateSource struct {
	// Name — RoleBinding name.
	// Default when omitted: "{{ .metadata.name }}-rolebinding"
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace — target namespace.
	// Default when omitted: "{{ .metadata.namespace }}"
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Labels — applied to RoleBinding metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`

	// RoleRef — the Role or ClusterRole this binding grants. Required.
	// kind defaults to "Role" when omitted; set it to "ClusterRole" to bind a
	// cluster-scoped role instead.
	//
	//	roleRef:
	//	  name: "{{ .metadata.name }}-role"
	//	  kind: Role
	RoleRef RoleRefSpec `yaml:"roleRef,omitempty" json:"roleRef,omitempty"`

	// Subjects — the users, groups, or ServiceAccounts this binding grants
	// the role to. Required: at least one subject.
	//
	//	subjects:
	//	  - kind: ServiceAccount
	//	    name: "{{ .metadata.name }}-sa"
	//	    namespace: "{{ .metadata.namespace }}"
	Subjects []SubjectSpec `yaml:"subjects,omitempty" json:"subjects,omitempty"`

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

	// Reconcile: true — also apply this declaration as drift correction on every
	// reconcile. Equivalent to declaring the same entry under both onCreate and
	// onReconcile. When false (default), only runs on onCreate (idempotent create).
	Reconcile bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`

	// ForEach declares dynamic expansion over a list field.
	// When set, one source declaration becomes N declarations — one per list element.
	// .item and .<as> are available in template expressions within this declaration.
	//
	//	forEach:
	//	  field: spec.regions
	//	  as: region
	ForEach *ForEachSpec `yaml:"forEach,omitempty" json:"forEach,omitempty"`

	// Or holds OR conditions — at least one must pass for this resource to be created.
	// Works alongside the existing Conditions (when:) field which uses AND semantics.
	//
	//	or:
	//	  - field: spec.tier
	//	    equals: pro
	//	  - field: spec.tier
	//	    equals: enterprise
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

// ── ClusterRole / ClusterRoleBinding ─────────────────────────────────────────

// ClusterRoleTemplateSource declares one cluster-scoped ClusterRole to be managed by Orkestra.
//
// ClusterRoles are cluster-scoped — no namespace field. Because Kubernetes cannot
// auto-GC cluster-scoped resources owned by namespace-scoped CRs, ownership is
// tracked via the orkestra.io/owner label. Declare cleanup in onDelete if needed.
//
// Example:
//
//	onCreate:
//	  clusterRoles:
//	    - name: "{{ .metadata.name }}-cluster-role"
//	      rules:
//	        - apiGroups: [""]
//	          resources: ["namespaces"]
//	          verbs: ["get", "list", "watch"]
type ClusterRoleTemplateSource struct {
	// Name — ClusterRole name.
	// Default when omitted: "{{ .metadata.name }}-cluster-role"
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Labels — applied to ClusterRole metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`

	// Rules — the permissions granted by this ClusterRole. Required: at least one rule.
	//
	//	rules:
	//	  - apiGroups: [""]
	//	    resources: ["namespaces"]
	//	    verbs: ["get", "list", "watch"]
	Rules []PolicyRuleSpec `yaml:"rules,omitempty" json:"rules,omitempty"`

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

	// Reconcile: true — also apply this declaration as drift correction on every
	// reconcile. Equivalent to declaring the same entry under both onCreate and
	// onReconcile. When false (default), only runs on onCreate (idempotent create).
	Reconcile bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`

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
	//	  field: spec.regions
	//	  as: region
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

// ClusterRoleBindingTemplateSource declares one cluster-scoped ClusterRoleBinding to be managed by Orkestra.
//
// ClusterRoleBindings are cluster-scoped — no namespace field.
// Ownership is tracked via the orkestra.io/owner label.
//
// Example:
//
//	onCreate:
//	  clusterRoleBindings:
//	    - name: "{{ .metadata.name }}-crb"
//	      roleRef:
//	        name: "{{ .metadata.name }}-cluster-role"
//	        kind: ClusterRole
//	      subjects:
//	        - kind: ServiceAccount
//	          name: "{{ .metadata.name }}-sa"
//	          namespace: "{{ .metadata.namespace }}"
type ClusterRoleBindingTemplateSource struct {
	// Name — ClusterRoleBinding name.
	// Default when omitted: "{{ .metadata.name }}-crb"
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Labels — applied to ClusterRoleBinding metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`

	// RoleRef — the ClusterRole this binding grants. Required.
	// kind defaults to "ClusterRole" when omitted (the only valid target for a
	// ClusterRoleBinding).
	//
	//	roleRef:
	//	  name: "{{ .metadata.name }}-cluster-role"
	RoleRef RoleRefSpec `yaml:"roleRef,omitempty" json:"roleRef,omitempty"`

	// Subjects — the users, groups, or ServiceAccounts this binding grants
	// the role to. Required: at least one subject.
	//
	//	subjects:
	//	  - kind: ServiceAccount
	//	    name: "{{ .metadata.name }}-sa"
	//	    namespace: "{{ .metadata.namespace }}"
	Subjects []SubjectSpec `yaml:"subjects,omitempty" json:"subjects,omitempty"`

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

	// Reconcile: true — also apply this declaration as drift correction on every
	// reconcile. Equivalent to declaring the same entry under both onCreate and
	// onReconcile. When false (default), only runs on onCreate (idempotent create).
	Reconcile bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`

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
	//	  field: spec.regions
	//	  as: region
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
