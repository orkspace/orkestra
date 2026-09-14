// pkg/resources/ingresses/types.go
package ingresses

// ResolvedIngressSpec is the fully resolved Ingress specification.
// All template expressions have been evaluated before this struct is populated.
// Passed directly to Create, Update, and Delete.
type ResolvedIngressSpec struct {
	// Name — Ingress resource name. Required.
	Name string

	// Namespace — target namespace.
	Namespace string

	// Host — virtual host for the Ingress rule.
	Host string

	// ServiceName — backend Service name.
	ServiceName string

	// ServicePort — backend Service port.
	ServicePort int32

	// Path — HTTP path prefix. Default: "/"
	Path string

	// PathType — Prefix | Exact | ImplementationSpecific. Default: Prefix.
	PathType string

	// IngressClass — Ingress class name. Empty means cluster default.
	IngressClass string

	// Labels applied to Ingress metadata.
	// Orkestra always adds: managed-by=orkestra, orkestra-owner=<cr-name>
	Labels map[string]string

	// Annotations applied to Ingress metadata.
	Annotations map[string]string

	// TLS — optional TLS configuration. nil means no TLS.
	TLS *ResolvedIngressTLS

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}

// ResolvedIngressTLS holds the fully resolved TLS configuration for an Ingress.
type ResolvedIngressTLS struct {
	// Create — whether to configure TLS on this Ingress.
	Create bool

	// SecretName — name of the kubernetes.io/tls Secret.
	SecretName string

	// Hosts — hostnames covered by the TLS certificate.
	Hosts []string

	// ValidFor — certificate validity duration string passed to GenerateTLSBundle.
	ValidFor string
}
