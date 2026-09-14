// pkg/types/types_secret.go
package types

// ── Secret ─────────────────────────────────────────────────────────────────────

// SecretTemplateSource declares one Secret to be managed by Orkestra.
//
// Secret data values are static — template expressions are not evaluated
// in Secret data entries. For dynamic configuration, use a custom Go hook.
//
// Example:
//
//	onCreate:
//	  secrets:
//	    - name: "{{ .metadata.name }}-credentials"
//	      type: Opaque
//	      data:
//	        USERNAME: admin
//	        PASSWORD: "supersecret"
//
// You may also copy from an existing Secret using FromSecret.
type SecretTemplateSource struct {
	// Name — Secret name.
	// Default when omitted: "{{ .metadata.name }}-secret"
	Name string `yaml:"name,omitempty" json:"name,omitempty" validate:"omitempty"`

	// Namespace — target namespace.
	// Default when omitted: "{{ .metadata.namespace }}"
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty" validate:"omitempty"`

	// Type — Kubernetes Secret type.
	// Default: "Opaque"
	Type string `yaml:"type,omitempty" json:"type,omitempty" validate:"omitempty"`

	// Data — static key-value entries.
	// Values are plain strings — template expressions are not supported here.
	// If you need templated or dynamic values, use a custom Go hook.
	Data map[string]string `yaml:"data,omitempty" json:"data,omitempty" validate:"omitempty"`

	// Labels — applied to Secret metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty" validate:"omitempty"`

	// Annotations — applied to Secret metadata.
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`

	// FromSecret — name of an existing Secret to copy data from.
	// Orkestra reads this at reconcile time — copies stay in sync with the source.
	FromSecret string `yaml:"fromSecret,omitempty" json:"fromSecret,omitempty" validate:"omitempty"`

	// FromNamespace — namespace where FromSecret lives.
	// Default: same namespace as the CR.
	FromNamespace string `yaml:"fromNamespace,omitempty" json:"fromNamespace,omitempty" validate:"omitempty"`

	// ToNamespaces - a list of target namespaces
	// Default when omitted: "{{ .metadata.namespace }}"
	ToNamespaces []string `yaml:"toNamespaces,omitempty" json:"toNamespaces,omitempty" validate:"omitempty"`

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

	// ForEach declares dynamic expansion (same as other resource types)
	ForEach *ForEachSpec `yaml:"forEach,omitempty" json:"forEach,omitempty"`

	// Or holds OR conditions (same as other resource types)
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// Once — when true, evaluates templates and creates the Secret only if it
	// does not already exist; every subsequent reconcile is a no-op. Use this
	// for generated credentials that must stay stable across reconciles, in
	// combination with random notes such as randomAlphanumeric, randomHex,
	// and randomBase64. Has no effect when reconcile: true is also set on
	// this entry, or when the entry is declared under onReconcile —
	// continuous reconciliation takes precedence over once. Default: false
	// (standard create/update behavior).
	//
	//	secrets:
	//	  - name: "{{ .metadata.name }}-credentials"
	//	    once: true
	//	    data:
	//	      password: "{{ randomAlphanumeric 32 }}"
	Once bool `yaml:"once,omitempty" json:"once,omitempty"`

	// RotateAfter declares a time-based rotation threshold.
	// When set alongside once: true, the Secret is recreated when its age
	// exceeds this duration. The creation time is tracked via the annotation:
	//   orkestra.orkspace.io/generated-at: "2026-04-06T08:00:00Z"
	//
	// Supported formats: 30s, 5m, 12h, 90d, 1y
	// Days (d) and years (y) are extensions beyond Go's standard duration format.
	//
	// Example:
	//   secrets:
	//     - name: "{{ .metadata.name }}-credentials"
	//       once: true
	//       rotateAfter: 90d
	//       data:
	//         password: "{{ randomAlphanumeric 32 }}"
	RotateAfter string `yaml:"rotateAfter,omitempty" json:"rotateAfter,omitempty"`

	// TLS declares self-signed CA and server certificate generation.
	// When set, the data: block is ignored — the Secret is created as type
	// kubernetes.io/tls with fields: tls.crt, tls.key, ca.crt
	//
	// Default Secret name when name is empty: "orkestra-tls"
	// Default validFor when empty: same as rotateAfter, or "1y"
	//
	// Example:
	//   secrets:
	//     - name: "{{ .metadata.name }}-tls"
	//       once: true
	//       rotateAfter: 1y
	//       tls:
	//         commonName: "{{ .metadata.name }}.{{ .metadata.namespace }}.svc"
	//         dnsNames:
	//           - "{{ .metadata.name }}"
	//           - "{{ .metadata.name }}.{{ .metadata.namespace }}"
	//           - "{{ .metadata.name }}.{{ .metadata.namespace }}.svc"
	//           - "{{ .metadata.name }}.{{ .metadata.namespace }}.svc.cluster.local"
	//         validFor: 1y
	TLS *TLSSpec `yaml:"tls,omitempty" json:"tls,omitempty"`

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string `json:"sleep,omitempty" yaml:"sleep,omitempty"`

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool `yaml:"forceConflict,omitempty" json:"forceConflict,omitempty"`
}
