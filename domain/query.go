package domain

// RuntimeQuery is the query interface for the runtime component — the contract through
// which other Orkestra components read state from pkg/runtime without importing it directly.
// "Runtime" is the component topic, not a generic qualifier; a future GatewayQuery would
// serve the same role for the gateway component.
//
// Implemented by pkg/utils/common/query.runtimeQuery.
type RuntimeQuery interface {
	// IsUnique reports whether no other CR of this kind currently has field == value.
	// selfNamespace/selfName identify the CR under evaluation so it is excluded from
	// the comparison — a CR is never a duplicate of its own already-stored value.
	// Best-effort: reads from the runtime's informer cache. Two concurrent requests
	// can both pass; authoritative enforcement is in pkg/runtime/reconciler.
	IsUnique(field, value, selfNamespace, selfName string) (bool, error)

	// ForHealth returns the CRD's health summary as reported by the runtime's
	// /katalog/{crd}/health endpoint. Keys match the fields observable via .health.*
	// in preReconcile gate conditions.
	ForHealth() map[string]interface{}

	// ForMetrics returns the CRD's metrics as reported by the runtime's
	// /katalog/{crd} endpoint (metrics key). Keys match the fields observable via
	// .metrics.* in preReconcile gate conditions.
	ForMetrics() map[string]interface{}
}
