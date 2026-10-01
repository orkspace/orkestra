// pkg/types/types_hook_templates.go
package types

// HookTemplates declares the child resources Orkestra manages at a lifecycle event.
// Resources receive owner references — Kubernetes GC handles deletion automatically.
// All slices are optional; undeclared resources are invisible to the reconciler.
type HookTemplates struct {
	Deployments              []DeploymentTemplateSource         `yaml:"deployments,omitempty" json:"deployments,omitempty" validate:"omitempty"`
	ReplicaSets              []ReplicaSetTemplateSource         `yaml:"replicaSets,omitempty" json:"replicaSets,omitempty" validate:"omitempty"`
	Services                 []ServiceTemplateSource            `yaml:"services,omitempty" json:"services,omitempty" validate:"omitempty"`
	Pods                     []PodTemplateSource                `yaml:"pods,omitempty" json:"pods,omitempty" validate:"omitempty"`
	Jobs                     []JobTemplateSource                `yaml:"jobs,omitempty" json:"jobs,omitempty" validate:"omitempty"`
	CronJobs                 []CronJobTemplateSource            `yaml:"cronJobs,omitempty" json:"cronJobs,omitempty" validate:"omitempty"`
	Secrets                  []SecretTemplateSource             `yaml:"secrets,omitempty" json:"secrets,omitempty" validate:"omitempty"`
	ConfigMaps               []ConfigMapTemplateSource          `yaml:"configMaps,omitempty" json:"configMaps,omitempty" validate:"omitempty"`
	ServiceAccounts          []ServiceAccountTemplateSource     `yaml:"serviceAccounts,omitempty" json:"serviceAccounts,omitempty" validate:"omitempty"`
	StatefulSets             []StatefulSetTemplateSource        `yaml:"statefulSets,omitempty" json:"statefulSets,omitempty" validate:"omitempty"`
	Ingresses                []IngressTemplateSource            `yaml:"ingresses,omitempty" json:"ingresses,omitempty" validate:"omitempty"`
	PersistentVolumes        []PVTemplateSource                 `yaml:"persistentVolumes,omitempty" json:"persistentVolumes,omitempty" validate:"omitempty"`
	PersistentVolumeClaims   []PVCTemplateSource                `yaml:"persistentVolumeClaims,omitempty" json:"persistentVolumeClaims,omitempty" validate:"omitempty"`
	HorizontalPodAutoscalers []HPATemplateSource                `yaml:"hpa,omitempty" json:"hpa,omitempty" validate:"omitempty"`
	PodDisruptionBudgets     []PDBTemplateSource                `yaml:"pdb,omitempty" json:"pdb,omitempty" validate:"omitempty"`
	Namespaces               []NamespaceTemplateSource          `yaml:"namespaces,omitempty" json:"namespaces,omitempty" validate:"omitempty"`
	Roles                    []RoleTemplateSource               `yaml:"roles,omitempty" json:"roles,omitempty" validate:"omitempty"`
	RoleBindings             []RoleBindingTemplateSource        `yaml:"roleBindings,omitempty" json:"roleBindings,omitempty" validate:"omitempty"`
	CustomResource           []CustomResourceTemplateSource     `yaml:"custom,omitempty" json:"custom,omitempty" validate:"omitempty"`
	ClusterRoles             []ClusterRoleTemplateSource        `yaml:"clusterRoles,omitempty" json:"clusterRoles,omitempty" validate:"omitempty"`
	ClusterRoleBindings      []ClusterRoleBindingTemplateSource `yaml:"clusterRoleBindings,omitempty" json:"clusterRoleBindings,omitempty" validate:"omitempty"`
	LimitRanges              []LimitRangeTemplateSource         `yaml:"limitRanges,omitempty" json:"limitRanges,omitempty" validate:"omitempty"`
	ResourceQuotas           []ResourceQuotaTemplateSource      `yaml:"resourceQuotas,omitempty" json:"resourceQuotas,omitempty" validate:"omitempty"`
	NetworkPolicies          []NetworkPolicyTemplateSource      `yaml:"networkPolicies,omitempty" json:"networkPolicies,omitempty" validate:"omitempty"`

	// External declares HTTP calls to make before resource creation.
	// Results available as .external.<n>.status, .body, .error
	External []ExternalCallSpec `yaml:"external,omitempty" json:"external,omitempty"`

	// When declares conditions that must all be true for this block to execute.
	// AND semantics — all conditions must pass. Evaluated before any resource in the block runs.
	When []Condition `yaml:"when,omitempty" json:"when,omitempty"`

	// Or declares conditions where at least one must be true for this block to execute.
	// Works alongside When: block runs if When passes OR any Or condition passes.
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// Ordered controls whether deletion happens sequentially with verification.
	// true  — delete groups in order, verify each is gone before proceeding
	// false — delete all resources via owner references (default, parallel)
	Ordered bool `yaml:"ordered,omitempty" json:"ordered,omitempty"`

	// Name is an optional identifier for this block, used in logs and events.
	// When set on groups inside ordered deletion, names must be unique across the group list.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Groups declares sequential deletion stages for ordered deletes.
	// Each element is a full HookTemplates block whose resources are deleted
	// as a unit. Orkestra deletes stage N, waits until all resources are gone,
	// then deletes stage N+1. Omit when Ordered is false.
	// When Ordered is true and Groups is empty, the flat resource fields above
	// (Jobs, Deployments, …) are treated as a single implicit group.
	Groups []HookTemplates `yaml:"groups,omitempty" json:"groups,omitempty"`

	// Timeout is the maximum time to wait for each deletion group to complete.
	// Defaults to 5m when Ordered is true. Ignored when Ordered is false.
	Timeout *Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	Volumes                     []PlaceholderSource `yaml:"volumes,omitempty" json:"volumes,omitempty" validate:"omitempty"`
	VolumeMounts                []PlaceholderSource `yaml:"volumeMounts,omitempty" json:"volumeMounts,omitempty" validate:"omitempty"`
	ServiceMonitors             []PlaceholderSource `yaml:"serviceMonitors,omitempty" json:"serviceMonitors,omitempty" validate:"omitempty"`
	PodSecurityPolicies         []PlaceholderSource `yaml:"podSecurityPolicies,omitempty" json:"podSecurityPolicies,omitempty" validate:"omitempty"`
	PriorityClasses             []PlaceholderSource `yaml:"priorityClasses,omitempty" json:"priorityClasses,omitempty" validate:"omitempty"`
	RuntimeClasses              []PlaceholderSource `yaml:"runtimeClasses,omitempty" json:"runtimeClasses,omitempty" validate:"omitempty"`
	PriorityLevelConfigurations []PlaceholderSource `yaml:"priorityLevelConfigurations,omitempty" json:"priorityLevelConfigurations,omitempty" validate:"omitempty"`
	PodTemplates                []PlaceholderSource `yaml:"podTemplates,omitempty" json:"podTemplates,omitempty" validate:"omitempty"`
	DaemonSets                  []PlaceholderSource `yaml:"daemonSets,omitempty" json:"daemonSets,omitempty" validate:"omitempty"`

	StorageClasses   []PlaceholderSource `yaml:"storageClasses,omitempty" json:"storageClasses,omitempty" validate:"omitempty"`
	StorageLocations []PlaceholderSource `yaml:"storageLocations,omitempty" json:"storageLocations,omitempty" validate:"omitempty"`
	StoragePools     []PlaceholderSource `yaml:"storagePools,omitempty" json:"storagePools,omitempty" validate:"omitempty"`
	StorageBackups   []PlaceholderSource `yaml:"storageBackups,omitempty" json:"storageBackups,omitempty" validate:"omitempty"`
	StorageSnapshots []PlaceholderSource `yaml:"storageSnapshots,omitempty" json:"storageSnapshots,omitempty" validate:"omitempty"`
	StorageVolumes   []PlaceholderSource `yaml:"storageVolumes,omitempty" json:"storageVolumes,omitempty" validate:"omitempty"`
}

// PlaceholderSource is a stub for resource types not yet wired into pkg/resources.
type PlaceholderSource struct{}
