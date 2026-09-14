// pkg/types/types_service.go
package types

// ── Service ───────────────────────────────────────────────────────────────────

// ServiceTemplateSource declares one Service to be managed by Orkestra.
//
// Example:
//
//	onCreate:
//	  services:
//	    - name: "{{ .metadata.name }}-svc"
//	      type: ClusterIP
//	      port: "80"
//	      targetPort: "8080"
//	      namespace: "{{ .metadata.namespace }}"
//	      labels:
//	        app: "{{ .metadata.name }}"
//	      selector:
//	        app: "{{ .metadata.name }}"
type ServiceTemplateSource struct {
	// Name — Service name.
	// Default when omitted: "{{ .metadata.name }}-svc"
	Name string `yaml:"name,omitempty" json:"name,omitempty" validate:"omitempty"`

	// Type — Kubernetes Service type.
	// Accepted values: ClusterIP, NodePort, LoadBalancer.
	// Default: ClusterIP.
	Type string `yaml:"type,omitempty" json:"type,omitempty" validate:"omitempty"`

	// Headless — when true, the Service is created without a clusterIP (clusterIP: None).
	// Used primarily for StatefulSets to enable stable network identities and per‑pod DNS:
	//   <podname>.<service>.<namespace>.svc.cluster.local
	// Set this to true when the Service is meant to back a StatefulSet or provide
	// direct pod‑to‑pod addressing rather than load‑balanced traffic.
	Headless bool `yaml:"headless,omitempty" json:"headless,omitempty" validate:"omitempty"`

	// Protocol defines network protocols supported for things like container ports.
	// "TCP", "UDP", "SCTP"
	Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty" validate:"omitempty"`

	// Port — Service port as a string.
	// Static: "80" or Dynamic: "{{ .spec.servicePort }}"
	Port string `yaml:"port" json:"port" validate:"omitempty"`

	// TargetPort — container port the Service routes traffic to.
	// Static: "8080" or Dynamic: "{{ .spec.containerPort }}"
	TargetPort string `yaml:"targetPort,omitempty" json:"targetPort,omitempty" validate:"omitempty"`

	// Namespace — target namespace.
	// Default when omitted: "{{ .metadata.namespace }}"
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty" validate:"omitempty"`

	// Labels — applied to Service metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty" validate:"omitempty"`

	// Selector filters which pods this service will route traffic to.
	// Useful in forEach situations where the labels would likely be the same.
	//
	//	selector:
	//	  app: "{{ .metadata.name }}"
	Selector Labels `yaml:"selector,omitempty" json:"selector,omitempty" validate:"omitempty"`

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
