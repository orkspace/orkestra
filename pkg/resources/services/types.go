// pkg/resources/services/types.go
package services

// ResolvedServiceSpec is the fully resolved Service specification.
// Produced by resolving template expressions and merging static values.
// Passed directly to Create, Update, and Delete.
type ResolvedServiceSpec struct {
	// Name — Service name. Required.
	Name string

	// Namespace — target namespace. Required.
	Namespace string

	// Type — Kubernetes Service type: ClusterIP, NodePort, LoadBalancer.
	// Default: ClusterIP.
	Type string

	// Headless — when true, the Service is created without a clusterIP (clusterIP: None).
	// Used primarily for StatefulSets to enable stable network identities and per‑pod DNS:
	//   <podname>.<service>.<namespace>.svc.cluster.local
	// Set this to true when the Service is meant to back a StatefulSet or provide
	// direct pod‑to‑pod addressing rather than load‑balanced traffic.
	Headless bool

	// Protocol defines network protocols supported for things like container ports.
	// "TCP", "UDP", "SCTP"
	Protocol string

	// Port — Service port.
	Port int32

	// TargetPort — container port the Service routes to.
	TargetPort int32

	// Labels — applied to Service metadata.
	// Orkestra always adds: managed-by=orkestra, orkestra-owner=<cr-name>
	Labels map[string]string

	// Selector —> service selector to route traffic to pods.
	Selector map[string]string

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}
