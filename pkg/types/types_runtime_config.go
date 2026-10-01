// pkg/types/types_runtime_config.go
package types

// RuntimeConfig governs the operatorBox as a long-lived entity, not a single reconcile cycle.
// Covers autoscaling the worker pool, finalizer lifecycle, namespace guards, and deletion protection.
type RuntimeConfig struct {
	// Finalizers is the per-CRD finalizer list. Falls back to the Katalog-level finalizer.
	Finalizers []string `yaml:"finalizers,omitempty" json:"finalizers,omitempty" validate:"omitempty"`

	// RemoveFinalizers strips all Orkestra finalizers from this CRD's CRs. Testing only.
	RemoveFinalizers bool `yaml:"removeFinalizers,omitempty" json:"removeFinalizers,omitempty"`

	// DeletionProtection overrides the global deletion protection policy for this CRD.
	DeletionProtection *DeletionProtectionOverride `yaml:"deletionProtection,omitempty" json:"deletionProtection,omitempty"`

	// RestrictedNamespaces blocks reconciliation for CRs in the named namespaces.
	RestrictedNamespaces RestrictedNamespaces `yaml:"restrictedNamespaces,omitempty" json:"restrictedNamespaces,omitempty"`

	// AllowedNamespaces restricts reconciliation to CRs in the named namespaces only.
	AllowedNamespaces AllowedNamespaces `yaml:"allowedNamespaces,omitempty" json:"allowedNamespaces,omitempty"`

	// IgnoreStatusPatch disables the runtime's automatic status patch for this CRD.
	IgnoreStatusPatch bool `yaml:"ignoreStatusPatch,omitempty" json:"ignoreStatusPatch,omitempty"`

	// IgnoreObservedGeneration disables generation-based reconcile skipping for this CRD.
	IgnoreObservedGeneration bool `yaml:"ignoreObservedGeneration,omitempty" json:"ignoreObservedGeneration,omitempty"`

	// Autoscale declares runtime worker/queue/resync overrides driven by conditions.
	Autoscale *AutoscaleSpec `yaml:"autoscale,omitempty" json:"autoscale,omitempty"`

	// Cleanup declares when to delete the CR after it reaches a terminal state.
	// Evaluated at the pre-reconcile gate — conditions met before the reconciler is
	// called cause the CR to be deleted immediately (or after deleteAfter elapses).
	// Works for all reconciler types: remote, generic, and typed.
	Cleanup *CleanupConfig `yaml:"cleanup,omitempty" json:"cleanup,omitempty"`
}

// CleanupConfig declares when to delete a CR.
// When declares AND conditions; Or declares OR conditions. Both must pass when both
// are declared (same semantics as GateConditions). The most common pattern is
// Or-only: delete when phase is Completed OR Failed.
type CleanupConfig struct {
	// When declares AND conditions — all must be true.
	When []Condition `yaml:"when,omitempty" json:"when,omitempty"`

	// Or declares OR conditions — at least one must be true.
	// When both When and Or are declared, both must pass.
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// DeleteAfter is an optional grace period between the condition being met and
	// the actual deletion. Orkestra annotates the CR with the first-met timestamp
	// and re-evaluates on the next reconcile cycle. Zero means delete immediately.
	DeleteAfter Duration `yaml:"deleteAfter,omitempty" json:"deleteAfter,omitempty"`
}

// HasCleanup reports whether a cleanup block with at least one condition is declared.
func (r *RuntimeConfig) HasCleanup() bool {
	return r != nil && r.Cleanup != nil && (len(r.Cleanup.When) > 0 || len(r.Cleanup.Or) > 0)
}

// EffectiveCleanup returns the cleanup config or nil when absent.
func (r *RuntimeConfig) EffectiveCleanup() *CleanupConfig {
	if r == nil {
		return nil
	}
	return r.Cleanup
}

func (r *RuntimeConfig) Empty() bool { return r == nil }
