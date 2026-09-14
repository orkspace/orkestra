// pkg/types/types_ingress.go
package types

// ── Ingress ───────────────────────────────────────────────────────────────────

// IngressTemplateSource declares one Ingress to be managed by Orkestra.
//
// Example:
//
//	onReconcile:
//	  ingresses:
//	    - name: "{{ .metadata.name }}-ingress"
//	      host: "{{ .spec.hostname }}"
//	      serviceName: "{{ .metadata.name }}-svc"
//	      servicePort: "{{ .spec.port }}"
//	      path: /
//	      pathType: Prefix
//	      className: nginx
//	      tls:
//	        create: true
//	        secretName: "{{ .metadata.name }}-tls"
//	        hosts:
//	          - "{{ .spec.hostname }}"
type IngressTemplateSource struct {
	// Name — Ingress resource name. Default: "{{ .metadata.name }}-ingress"
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace — target namespace. Default: CR namespace.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Host — virtual host name for the Ingress rule.
	Host string `yaml:"host,omitempty" json:"host,omitempty"`

	// ServiceName — backend Service name this Ingress routes to.
	ServiceName string `yaml:"serviceName,omitempty" json:"serviceName,omitempty"`

	// ServicePort — backend Service port as a string. Supports template expressions.
	ServicePort string `yaml:"servicePort,omitempty" json:"servicePort,omitempty"`

	// Path — HTTP path prefix. Default: "/"
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// PathType — Kubernetes IngressPathType: Prefix, Exact, ImplementationSpecific.
	// Default: Prefix.
	PathType string `yaml:"pathType,omitempty" json:"pathType,omitempty"`

	// IngressClass — Ingress class name (nginx, traefik, etc.). Optional.
	IngressClass string `yaml:"className,omitempty" json:"className,omitempty"`

	// Labels applied to Ingress metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`

	// Annotations applied to Ingress metadata. Values support template expressions.
	Annotations Labels `yaml:"annotations,omitempty" json:"annotations,omitempty"`

	// TLS — optional TLS configuration. When tls.create is true, Orkestra
	// generates a self-signed TLS Secret before creating the Ingress.
	//
	//	tls:
	//	  create: true
	//	  secretName: "{{ .metadata.name }}-tls"
	//	  hosts:
	//	    - "{{ .spec.hostname }}"
	TLS *IngressTLSSpec `yaml:"tls,omitempty" json:"tls,omitempty"`

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

// IngressTLSSpec configures TLS for an Ingress resource.
// When Create is true, Orkestra generates a kubernetes.io/tls Secret before
// the Ingress is applied so the Ingress can reference it immediately.
type IngressTLSSpec struct {
	// Create — when true, create a TLS secret and populate ingress.spec.tls.
	Create bool `yaml:"create,omitempty" json:"create,omitempty"`

	// SecretName — name of the TLS secret. Supports template expressions.
	// Default: "{{ .metadata.name }}-tls"
	SecretName string `yaml:"secretName,omitempty" json:"secretName,omitempty"`

	// Hosts — list of hostnames to include in the TLS certificate SANs.
	// Each element supports template expressions.
	Hosts []string `yaml:"hosts,omitempty" json:"hosts,omitempty"`

	// ValidFor — certificate validity duration (e.g. "1y", "90d"). Default: "1y".
	ValidFor string `yaml:"validFor,omitempty" json:"validFor,omitempty"`
}
