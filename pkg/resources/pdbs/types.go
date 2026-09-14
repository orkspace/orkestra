// pkg/resources/pdbs/types.go
package pdbs

// ResolvedPDBSpec is the fully resolved PodDisruptionBudget specification.
// All template expressions have been evaluated before this struct is populated.
// Passed directly to Create, Update, and Delete.
type ResolvedPDBSpec struct {
	// Name — PDB resource name. Required.
	Name string

	// Namespace — target namespace.
	Namespace string

	// Selector — label selector identifying the pods this PDB protects.
	Selector map[string]string

	// MinAvailable — minimum pods that must remain available.
	// Accepts integer string ("1") or percentage string ("50%").
	// Empty means not set; MaxUnavailable is used instead.
	MinAvailable string

	// MaxUnavailable — maximum pods that may be unavailable.
	// Accepts integer string ("1") or percentage string ("25%").
	// Empty means not set; MinAvailable is used instead.
	MaxUnavailable string

	// Labels applied to PDB metadata.
	// Orkestra always adds: managed-by=orkestra, orkestra-owner=<cr-name>
	Labels map[string]string

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}
