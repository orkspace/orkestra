package types

import "fmt"

// ProfileRegistry holds all user-defined profiles declared in a Katalog or Motif.
// Profiles are resolved before built-ins at validate and reconcile time.
// Template expressions in profile field values are allowed and resolved at reconcile time.
type ProfileRegistry struct {
	Include           string                        `yaml:"include,omitempty"            json:"include,omitempty"`
	NetworkPolicies   []NetworkPolicyProfileDef     `yaml:"networkPolicies,omitempty"    json:"networkPolicies,omitempty"`
	ResourceQuotas    []ResourceQuotaProfileDef     `yaml:"resourceQuotas,omitempty"     json:"resourceQuotas,omitempty"`
	LimitRanges       []LimitRangeProfileDef        `yaml:"limitRanges,omitempty"        json:"limitRanges,omitempty"`
	HPA               []HPAProfileDef               `yaml:"hpa,omitempty"                json:"hpa,omitempty"`
	PDB               []PDBProfileDef               `yaml:"pdb,omitempty"                json:"pdb,omitempty"`
	RollingUpdate     []RollingUpdateProfileDef     `yaml:"rollingUpdate,omitempty"      json:"rollingUpdate,omitempty"`
	Reconciler        []ReconcilerProfileDef        `yaml:"reconciler,omitempty"         json:"reconciler,omitempty"`
	Resources         []ResourceProfileDef          `yaml:"resources,omitempty"          json:"resources,omitempty"`
	Probes            []ProbeProfileDef             `yaml:"probes,omitempty"             json:"probes,omitempty"`
	ContainerSecurity []ContainerSecurityProfileDef `yaml:"containerSecurity,omitempty" json:"containerSecurity,omitempty"`
	PodSecurity       []PodSecurityProfileDef       `yaml:"podSecurity,omitempty"        json:"podSecurity,omitempty"`
}

func (r ProfileRegistry) Empty() bool {
	return len(r.NetworkPolicies) == 0 &&
		len(r.ResourceQuotas) == 0 &&
		len(r.LimitRanges) == 0 &&
		len(r.HPA) == 0 &&
		len(r.PDB) == 0 &&
		len(r.RollingUpdate) == 0 &&
		len(r.Reconciler) == 0 &&
		len(r.Resources) == 0 &&
		len(r.Probes) == 0 &&
		len(r.ContainerSecurity) == 0 &&
		len(r.PodSecurity) == 0
}

func (r ProfileRegistry) LookupNetworkPolicy(name string) (NetworkPolicyProfileDef, bool) {
	for _, e := range r.NetworkPolicies {
		if e.Name == name {
			return e, true
		}
	}
	return NetworkPolicyProfileDef{}, false
}

func (r ProfileRegistry) LookupResourceQuota(name string) (ResourceQuotaProfileDef, bool) {
	for _, e := range r.ResourceQuotas {
		if e.Name == name {
			return e, true
		}
	}
	return ResourceQuotaProfileDef{}, false
}

func (r ProfileRegistry) LookupLimitRange(name string) (LimitRangeProfileDef, bool) {
	for _, e := range r.LimitRanges {
		if e.Name == name {
			return e, true
		}
	}
	return LimitRangeProfileDef{}, false
}

func (r ProfileRegistry) LookupHPA(name string) (HPAProfileDef, bool) {
	for _, e := range r.HPA {
		if e.Name == name {
			return e, true
		}
	}
	return HPAProfileDef{}, false
}

func (r ProfileRegistry) LookupPDB(name string) (PDBProfileDef, bool) {
	for _, e := range r.PDB {
		if e.Name == name {
			return e, true
		}
	}
	return PDBProfileDef{}, false
}

func (r ProfileRegistry) LookupRollingUpdate(name string) (RollingUpdateProfileDef, bool) {
	for _, e := range r.RollingUpdate {
		if e.Name == name {
			return e, true
		}
	}
	return RollingUpdateProfileDef{}, false
}

func (r ProfileRegistry) LookupReconciler(name string) (ReconcilerProfileDef, bool) {
	for _, e := range r.Reconciler {
		if e.Name == name {
			return e, true
		}
	}
	return ReconcilerProfileDef{}, false
}

func (r ProfileRegistry) LookupResource(name string) (ResourceProfileDef, bool) {
	for _, e := range r.Resources {
		if e.Name == name {
			return e, true
		}
	}
	return ResourceProfileDef{}, false
}

func (r ProfileRegistry) LookupProbe(name string) (ProbeProfileDef, bool) {
	for _, e := range r.Probes {
		if e.Name == name {
			return e, true
		}
	}
	return ProbeProfileDef{}, false
}

func (r ProfileRegistry) LookupContainerSecurity(name string) (ContainerSecurityProfileDef, bool) {
	for _, e := range r.ContainerSecurity {
		if e.Name == name {
			return e, true
		}
	}
	return ContainerSecurityProfileDef{}, false
}

func (r ProfileRegistry) LookupPodSecurity(name string) (PodSecurityProfileDef, bool) {
	for _, e := range r.PodSecurity {
		if e.Name == name {
			return e, true
		}
	}
	return PodSecurityProfileDef{}, false
}

// Merge combines other into r, returning a conflict error if the same name
// appears in the same class in both registries.
func (r ProfileRegistry) Merge(other ProfileRegistry, otherSource string) (ProfileRegistry, error) {
	merged := r
	for _, e := range other.NetworkPolicies {
		if _, found := r.LookupNetworkPolicy(e.Name); found {
			return ProfileRegistry{}, profileConflictError("networkPolicies", e.Name, otherSource)
		}
		merged.NetworkPolicies = append(merged.NetworkPolicies, e)
	}
	for _, e := range other.ResourceQuotas {
		if _, found := r.LookupResourceQuota(e.Name); found {
			return ProfileRegistry{}, profileConflictError("resourceQuotas", e.Name, otherSource)
		}
		merged.ResourceQuotas = append(merged.ResourceQuotas, e)
	}
	for _, e := range other.LimitRanges {
		if _, found := r.LookupLimitRange(e.Name); found {
			return ProfileRegistry{}, profileConflictError("limitRanges", e.Name, otherSource)
		}
		merged.LimitRanges = append(merged.LimitRanges, e)
	}
	for _, e := range other.HPA {
		if _, found := r.LookupHPA(e.Name); found {
			return ProfileRegistry{}, profileConflictError("hpa", e.Name, otherSource)
		}
		merged.HPA = append(merged.HPA, e)
	}
	for _, e := range other.PDB {
		if _, found := r.LookupPDB(e.Name); found {
			return ProfileRegistry{}, profileConflictError("pdb", e.Name, otherSource)
		}
		merged.PDB = append(merged.PDB, e)
	}
	for _, e := range other.RollingUpdate {
		if _, found := r.LookupRollingUpdate(e.Name); found {
			return ProfileRegistry{}, profileConflictError("rollingUpdate", e.Name, otherSource)
		}
		merged.RollingUpdate = append(merged.RollingUpdate, e)
	}
	for _, e := range other.Reconciler {
		if _, found := r.LookupReconciler(e.Name); found {
			return ProfileRegistry{}, profileConflictError("reconciler", e.Name, otherSource)
		}
		merged.Reconciler = append(merged.Reconciler, e)
	}
	for _, e := range other.Resources {
		if _, found := r.LookupResource(e.Name); found {
			return ProfileRegistry{}, profileConflictError("resources", e.Name, otherSource)
		}
		merged.Resources = append(merged.Resources, e)
	}
	for _, e := range other.Probes {
		if _, found := r.LookupProbe(e.Name); found {
			return ProfileRegistry{}, profileConflictError("probes", e.Name, otherSource)
		}
		merged.Probes = append(merged.Probes, e)
	}
	for _, e := range other.ContainerSecurity {
		if _, found := r.LookupContainerSecurity(e.Name); found {
			return ProfileRegistry{}, profileConflictError("containerSecurity", e.Name, otherSource)
		}
		merged.ContainerSecurity = append(merged.ContainerSecurity, e)
	}
	for _, e := range other.PodSecurity {
		if _, found := r.LookupPodSecurity(e.Name); found {
			return ProfileRegistry{}, profileConflictError("podSecurity", e.Name, otherSource)
		}
		merged.PodSecurity = append(merged.PodSecurity, e)
	}
	return merged, nil
}

func profileConflictError(class, name, source string) error {
	return fmt.Errorf("profile conflict: %s %q defined in both %s and the katalog", class, name, source)
}

// NetworkPolicyProfileDef defines a named NetworkPolicy profile.
// Fields mirror NetworkPolicyTemplateSource minus declaration-level concerns.
// Template expressions are allowed and resolved at reconcile time.
type NetworkPolicyProfileDef struct {
	Name        string                     `yaml:"name" json:"name"`
	Description string                     `yaml:"description,omitempty" json:"description,omitempty"`
	PodSelector map[string]interface{}     `yaml:"podSelector,omitempty" json:"podSelector,omitempty"`
	Ingress     []NetworkPolicyIngressRule `yaml:"ingress,omitempty" json:"ingress,omitempty"`
	Egress      []NetworkPolicyEgressRule  `yaml:"egress,omitempty" json:"egress,omitempty"`
	PolicyTypes []string                   `yaml:"policyTypes,omitempty" json:"policyTypes,omitempty"`
}

// ResourceQuotaProfileDef defines a named ResourceQuota profile.
type ResourceQuotaProfileDef struct {
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Hard        map[string]string `yaml:"hard" json:"hard"`
}

// LimitRangeProfileDef defines a named LimitRange profile.
type LimitRangeProfileDef struct {
	Name        string           `yaml:"name" json:"name"`
	Description string           `yaml:"description,omitempty" json:"description,omitempty"`
	Limits      []LimitRangeItem `yaml:"limits" json:"limits"`
}

// HPAProfileDef defines a named HPA profile.
// Template expressions in MinReplicas, MaxReplicas, and TargetCPUUtilizationPercentage
// are resolved at reconcile time.
type HPAProfileDef struct {
	Name                           string       `yaml:"name" json:"name"`
	Description                    string       `yaml:"description,omitempty" json:"description,omitempty"`
	MinReplicas                    string       `yaml:"minReplicas,omitempty" json:"minReplicas,omitempty"`
	MaxReplicas                    string       `yaml:"maxReplicas,omitempty" json:"maxReplicas,omitempty"`
	TargetCPUUtilizationPercentage string       `yaml:"targetCPUUtilizationPercentage,omitempty" json:"targetCPUUtilizationPercentage,omitempty"`
	Behavior                       *HPABehavior `yaml:"behavior,omitempty" json:"behavior,omitempty"`
}

// PDBProfileDef defines a named PodDisruptionBudget profile.
type PDBProfileDef struct {
	Name           string `yaml:"name" json:"name"`
	Description    string `yaml:"description,omitempty" json:"description,omitempty"`
	MinAvailable   string `yaml:"minAvailable,omitempty" json:"minAvailable,omitempty"`
	MaxUnavailable string `yaml:"maxUnavailable,omitempty" json:"maxUnavailable,omitempty"`
}

// RollingUpdateProfileDef defines a named rolling update profile.
type RollingUpdateProfileDef struct {
	Name           string `yaml:"name" json:"name"`
	Description    string `yaml:"description,omitempty" json:"description,omitempty"`
	MaxSurge       string `yaml:"maxSurge,omitempty" json:"maxSurge,omitempty"`
	MaxUnavailable string `yaml:"maxUnavailable,omitempty" json:"maxUnavailable,omitempty"`
}

// ReconcilerProfileDef defines a named reconciler tuning profile.
// All fields mirror the inline fields under operatorBox.reconciler.
// Built-in profiles: high-throughput, conservative, development.
type ReconcilerProfileDef struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Workers     int      `yaml:"workers,omitempty" json:"workers,omitempty"`
	Resync      Duration `yaml:"resync,omitempty" json:"resync,omitempty"`
	Queue       Queue    `yaml:"queue,omitempty" json:"queue,omitempty"`
}

// ResourceProfileDef defines a named CPU/memory resource preset.
// Built-ins: tiny, small, medium, large, burst, steady, compute-heavy, memory-heavy.
type ResourceProfileDef struct {
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Requests    map[string]string `yaml:"requests,omitempty" json:"requests,omitempty"`
	Limits      map[string]string `yaml:"limits,omitempty" json:"limits,omitempty"`
}

// ProbeProfileDef defines a named probe timing preset.
// Built-ins: fast, standard, patient, slow-start.
type ProbeProfileDef struct {
	Name                string `yaml:"name" json:"name"`
	Description         string `yaml:"description,omitempty" json:"description,omitempty"`
	InitialDelaySeconds int32  `yaml:"initialDelaySeconds,omitempty" json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int32  `yaml:"periodSeconds,omitempty" json:"periodSeconds,omitempty"`
	FailureThreshold    int32  `yaml:"failureThreshold,omitempty" json:"failureThreshold,omitempty"`
	SuccessThreshold    int32  `yaml:"successThreshold,omitempty" json:"successThreshold,omitempty"`
	TimeoutSeconds      int32  `yaml:"timeoutSeconds,omitempty" json:"timeoutSeconds,omitempty"`
}

// ContainerSecurityProfileDef defines a named container security context preset.
// Built-ins: baseline, restricted, hardened.
type ContainerSecurityProfileDef struct {
	Name                     string              `yaml:"name" json:"name"`
	Description              string              `yaml:"description,omitempty" json:"description,omitempty"`
	AllowPrivilegeEscalation *bool               `yaml:"allowPrivilegeEscalation,omitempty" json:"allowPrivilegeEscalation,omitempty"`
	ReadOnlyRootFilesystem   *bool               `yaml:"readOnlyRootFilesystem,omitempty" json:"readOnlyRootFilesystem,omitempty"`
	RunAsNonRoot             *bool               `yaml:"runAsNonRoot,omitempty" json:"runAsNonRoot,omitempty"`
	RunAsUser                *int64              `yaml:"runAsUser,omitempty" json:"runAsUser,omitempty"`
	RunAsGroup               *int64              `yaml:"runAsGroup,omitempty" json:"runAsGroup,omitempty"`
	Capabilities             *CapabilitiesConfig `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

// PodSecurityProfileDef defines a named pod security context preset.
// Built-ins: baseline, restricted, hardened.
type PodSecurityProfileDef struct {
	Name         string `yaml:"name" json:"name"`
	Description  string `yaml:"description,omitempty" json:"description,omitempty"`
	RunAsNonRoot *bool  `yaml:"runAsNonRoot,omitempty" json:"runAsNonRoot,omitempty"`
	RunAsUser    *int64 `yaml:"runAsUser,omitempty" json:"runAsUser,omitempty"`
	RunAsGroup   *int64 `yaml:"runAsGroup,omitempty" json:"runAsGroup,omitempty"`
	FSGroup      *int64 `yaml:"fsGroup,omitempty" json:"fsGroup,omitempty"`
}
