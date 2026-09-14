// pkg/resources/pvs/types.go
package pvs

// ResolvedPVSpec is the fully resolved PersistentVolume specification.
// PersistentVolumes are cluster-scoped — Namespace is not used.
type ResolvedPVSpec struct {
	Name             string
	StorageClassName string
	Capacity         string
	AccessModes      []string
	ReclaimPolicy    string
	HostPath         string
	CSIDriver        string
	CSIVolumeHandle  string
	Labels           map[string]string

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}
