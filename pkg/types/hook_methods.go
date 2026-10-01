package types

// Empty reports whether this HookTemplates has no resource declarations.
func (h HookTemplates) Empty() bool {
	return len(h.Deployments) == 0 &&
		len(h.ReplicaSets) == 0 &&
		len(h.StatefulSets) == 0 &&
		len(h.Services) == 0 &&
		len(h.Pods) == 0 &&
		len(h.Jobs) == 0 &&
		len(h.CronJobs) == 0 &&
		len(h.Secrets) == 0 &&
		len(h.ConfigMaps) == 0 &&
		len(h.ServiceAccounts) == 0 &&
		len(h.Ingresses) == 0 &&
		len(h.PersistentVolumes) == 0 &&
		len(h.PersistentVolumeClaims) == 0 &&
		len(h.HorizontalPodAutoscalers) == 0 &&
		len(h.PodDisruptionBudgets) == 0 &&
		len(h.Namespaces) == 0 &&
		len(h.Roles) == 0 &&
		len(h.RoleBindings) == 0 &&
		len(h.ClusterRoles) == 0 &&
		len(h.ClusterRoleBindings) == 0 &&
		len(h.NetworkPolicies) == 0 &&
		len(h.ResourceQuotas) == 0 &&
		len(h.LimitRanges) == 0 &&
		len(h.External) == 0 &&
		len(h.CustomResource) == 0
}

// ExternalCalls returns the external call specs declared in this hook phase.
// Returns nil when the receiver is nil or has no external declarations.
func (h *HookTemplates) ExternalCalls() []ExternalCallSpec {
	if h == nil {
		return nil
	}
	return h.External
}

// HasAnyHooks reports whether this CRD declares any onCreate, onReconcile, or onDelete hooks.
func (c *CRDEntry) HasAnyHookTemplates() bool {
	return c.HasOnCreate() || c.HasOnReconcile() || c.HasOnDelete()
}

// HasAnyDeployments reports whether this CRD defines any Deployments
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyDeployments() bool {
	if c.HasOnCreate() {
		return c.Box().EffectiveOnCreate().Deployments != nil
	}
	if c.HasOnReconcile() {
		return c.Box().EffectiveOnReconcile().Deployments != nil
	}

	return false
}

// HasAnyStatefulSets reports whether this CRD defines any StatefulSets
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyStatefulSets() bool {
	if c.HasOnCreate() {
		return c.Box().EffectiveOnCreate().StatefulSets != nil
	}
	if c.HasOnReconcile() {
		return c.Box().EffectiveOnReconcile().StatefulSets != nil
	}

	return false
}

// HasAnyReplicaSets reports whether this CRD defines any ReplicaSets
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyReplicaSets() bool {
	if c.HasOnCreate() {
		return c.Box().EffectiveOnCreate().ReplicaSets != nil
	}
	if c.HasOnReconcile() {
		return c.Box().EffectiveOnReconcile().ReplicaSets != nil
	}

	return false
}

// HasAnySecrets reports whether this CRD defines any secrets
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnySecrets() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().Secrets) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().Secrets) > 0
	}

	return false
}

// HasAnyTLSSecrets reports whether any secret in either phase
// defines a TLS configuration.
func (c *CRDEntry) HasAnyTLSSecrets() bool {
	if c.HasOnCreate() {
		for _, s := range c.Box().EffectiveOnCreate().Secrets {
			if s.TLS != nil {
				return true
			}
		}
	}

	if c.HasOnReconcile() {
		for _, s := range c.Box().EffectiveOnReconcile().Secrets {
			if s.TLS != nil {
				return true
			}
		}
	}

	return false
}

// HasAnyHPA reports whether this CRD defines any HPA defined
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyHPA() bool {
	if c.HasOnCreate() {
		return c.Box().EffectiveOnCreate().HorizontalPodAutoscalers != nil
	}
	if c.HasOnReconcile() {
		return c.Box().EffectiveOnReconcile().HorizontalPodAutoscalers != nil
	}

	return false
}

// HasAnyServices reports whether this CRD defines any Services
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyServices() bool {
	if c.HasOnCreate() {
		return c.Box().EffectiveOnCreate().Services != nil
	}
	if c.HasOnReconcile() {
		return c.Box().EffectiveOnReconcile().Services != nil
	}

	return false
}

// HasAnyPods reports whether this CRD defines any Pods
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyPods() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().Pods) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().Pods) > 0
	}
	return false
}

// HasAnyConfigMaps reports whether this CRD defines any ConfigMaps
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyConfigMaps() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().ConfigMaps) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().ConfigMaps) > 0
	}
	return false
}

// HasAnyServiceAccounts reports whether this CRD defines any ServiceAccounts
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyServiceAccounts() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().ServiceAccounts) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().ServiceAccounts) > 0
	}
	return false
}

// HasAnyIngresses reports whether this CRD defines any Ingresses
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyIngresses() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().Ingresses) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().Ingresses) > 0
	}
	return false
}

// HasAnyPersistentVolumes reports whether this CRD defines any PersistentVolumes
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyPersistentVolumes() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().PersistentVolumes) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().PersistentVolumes) > 0
	}
	return false
}

// HasAnyPersistentVolumeClaims reports whether this CRD defines any PVCs
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyPersistentVolumeClaims() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().PersistentVolumeClaims) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().PersistentVolumeClaims) > 0
	}
	return false
}

// HasAnyPodDisruptionBudgets reports whether this CRD defines any PDBs
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyPodDisruptionBudgets() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().PodDisruptionBudgets) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().PodDisruptionBudgets) > 0
	}
	return false
}

// HasAnyNamespaces reports whether this CRD defines any Namespaces
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyNamespaces() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().Namespaces) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().Namespaces) > 0
	}
	return false
}

// HasAnyRoles reports whether this CRD defines any Roles
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyRoles() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().Roles) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().Roles) > 0
	}
	return false
}

// HasAnyRoleBindings reports whether this CRD defines any RoleBindings
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyRoleBindings() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().RoleBindings) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().RoleBindings) > 0
	}
	return false
}

// HasAnyVolumes reports whether this CRD defines any Volumes (placeholder)
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyVolumes() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().Volumes) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().Volumes) > 0
	}
	return false
}

// HasAnyVolumeMounts reports whether this CRD defines any VolumeMounts (placeholder)
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyVolumeMounts() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().VolumeMounts) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().VolumeMounts) > 0
	}
	return false
}

// HasAnyClusterRoles reports whether this CRD defines any ClusterRoles
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyClusterRoles() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().ClusterRoles) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().ClusterRoles) > 0
	}
	return false
}

// HasAnyClusterRoleBindings reports whether this CRD defines any ClusterRoleBindings
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyClusterRoleBindings() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().ClusterRoleBindings) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().ClusterRoleBindings) > 0
	}
	return false
}

// HasAnyServiceMonitors reports whether this CRD defines any ServiceMonitors
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyServiceMonitors() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().ServiceMonitors) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().ServiceMonitors) > 0
	}
	return false
}

// HasAnyPodSecurityPolicies reports whether this CRD defines any PSPs
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyPodSecurityPolicies() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().PodSecurityPolicies) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().PodSecurityPolicies) > 0
	}
	return false
}

// HasAnyPriorityClasses reports whether this CRD defines any PriorityClasses
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyPriorityClasses() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().PriorityClasses) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().PriorityClasses) > 0
	}
	return false
}

// HasAnyLimitRanges reports whether this CRD defines any LimitRanges
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyLimitRanges() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().LimitRanges) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().LimitRanges) > 0
	}
	return false
}

// HasAnyResourceQuotas reports whether this CRD defines any ResourceQuotas
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyResourceQuotas() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().ResourceQuotas) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().ResourceQuotas) > 0
	}
	return false
}

// HasAnyRuntimeClasses reports whether this CRD defines any RuntimeClasses
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyRuntimeClasses() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().RuntimeClasses) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().RuntimeClasses) > 0
	}
	return false
}

// HasAnyPriorityLevelConfigurations reports whether this CRD defines any PL configs
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyPriorityLevelConfigurations() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().PriorityLevelConfigurations) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().PriorityLevelConfigurations) > 0
	}
	return false
}

// HasAnyPodTemplates reports whether this CRD defines any PodTemplates
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyPodTemplates() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().PodTemplates) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().PodTemplates) > 0
	}
	return false
}

// HasAnyDaemonSets reports whether this CRD defines any DaemonSets
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyDaemonSets() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().DaemonSets) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().DaemonSets) > 0
	}
	return false
}

// HasAnyNetworkPolicies reports whether this CRD defines any NetworkPolicies
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyNetworkPolicies() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().NetworkPolicies) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().NetworkPolicies) > 0
	}
	return false
}

// HasAnyStorageClasses reports whether this CRD defines any StorageClasses
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyStorageClasses() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().StorageClasses) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().StorageClasses) > 0
	}
	return false
}

// HasAnyStorageLocations reports whether this CRD defines any StorageLocations
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyStorageLocations() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().StorageLocations) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().StorageLocations) > 0
	}
	return false
}

// HasAnyStoragePools reports whether this CRD defines any StoragePools
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyStoragePools() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().StoragePools) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().StoragePools) > 0
	}
	return false
}

// HasAnyStorageBackups reports whether this CRD defines any StorageBackups
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyStorageBackups() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().StorageBackups) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().StorageBackups) > 0
	}
	return false
}

// HasAnyStorageSnapshots reports whether this CRD defines any StorageSnapshots
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyStorageSnapshots() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().StorageSnapshots) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().StorageSnapshots) > 0
	}
	return false
}

// HasAnyStorageVolumes reports whether this CRD defines any StorageVolumes
// in either OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyStorageVolumes() bool {
	if c.HasOnCreate() {
		return len(c.Box().EffectiveOnCreate().StorageVolumes) > 0
	}
	if c.HasOnReconcile() {
		return len(c.Box().EffectiveOnReconcile().StorageVolumes) > 0
	}
	return false
}

// HasAnyCustomResources reports whether this CRD entry declares any Custom
// resources in either the OnCreate or OnReconcile phases.
func (c *CRDEntry) HasAnyCustomResources() bool {
	// Check OnCreate first (common fast path)
	if c.HasOnCreate() {
		if len(c.Box().EffectiveOnCreate().CustomResource) > 0 {
			return true
		}
	}

	// Then check OnReconcile
	if c.HasOnReconcile() {
		if len(c.Box().EffectiveOnReconcile().CustomResource) > 0 {
			return true
		}
	}

	return false
}

// NeedsResourceDecl reports whether this CRD defines any workload resources
// (Deployments, StatefulSets, or ReplicaSets) in either OnCreate or OnReconcile.
func (c *CRDEntry) NeedsResourceDecl() bool {
	return c.HasAnyDeployments() ||
		c.HasAnyReplicaSets() ||
		c.HasAnyStatefulSets()
}

// ResourceDecl returns the first ResourceRequirements defined for this CRD.
// It checks OnCreate first, then OnReconcile, and searches Deployments,
// StatefulSets, and ReplicaSets in that order. Returns nil if none exist.
func (c *CRDEntry) ResourceDecl() *ResourceRequirements {
	// OnCreate phase takes precedence
	if c.HasOnCreate() {
		if req := findResourceDeclInPhase(c.Box().EffectiveOnCreate()); req != nil {
			return req
		}
	}

	// OnReconcile fallback
	if c.HasOnReconcile() {
		if req := findResourceDeclInPhase(c.Box().EffectiveOnReconcile()); req != nil {
			return req
		}
	}

	return nil
}

// findResourceDeclInPhase searches Deployments, StatefulSets, and ReplicaSets
// inside a single OperatorPhase and returns the first non-nil ResourceRequirements.
func findResourceDeclInPhase(tmpl *HookTemplates) *ResourceRequirements {
	if tmpl == nil {
		return nil
	}

	// Deployments
	if tmpl.Deployments != nil {
		for _, d := range tmpl.Deployments {
			if d.Resources != nil {
				return d.Resources
			}
		}
	}

	// StatefulSets
	if tmpl.StatefulSets != nil {
		for _, s := range tmpl.StatefulSets {
			if s.Resources != nil {
				return s.Resources
			}
		}
	}

	// ReplicaSets
	if tmpl.ReplicaSets != nil {
		for _, r := range tmpl.ReplicaSets {
			if r.Resources != nil {
				return r.Resources
			}
		}
	}

	return nil
}
