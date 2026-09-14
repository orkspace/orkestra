// pkg/resources/pvcs/types.go
package pvcs

// ResolvedPVCSpec is the fully resolved PersistentVolumeClaim specification.
type ResolvedPVCSpec struct {
	Name             string
	Namespace        string
	StorageClassName string
	AccessModes      []string
	Storage          string
	VolumeMode       string
	VolumeName       string
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
