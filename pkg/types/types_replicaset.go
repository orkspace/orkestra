// pkg/types/types_replicaset.go
package types

// ── ReplicaSet ────────────────────────────────────────────────────────────────

// ReplicaSetTemplateSource declares one ReplicaSet to be managed by Orkestra.
//
// Minimal example — static values only:
//
//	onCreate:
//	  replicasets:
//	    - image: nginx:1.25
//	      replicas: "3"
//	      port: "8080"
//
// Full example — dynamic values from the CR:
//
//	onCreate:
//	  replicasets:
//	    - name: "{{ .metadata.name }}-app"
//	      image: "{{ .spec.image }}"
//	      replicas: "{{ .spec.replicas }}"
//	      port: "{{ .spec.port }}"
//	      namespace: "{{ .metadata.namespace }}"
//	      labels:
//	        - key: app
//	          value: "{{ .metadata.name }}"
//	        - key: managed-by
//	          value: orkestra
//	      resources:
//	        requests:
//	          cpu: 100m
//	          memory: 128Mi
//	        limits:
//	          cpu: 500m
//	          memory: 512Mi
type ReplicaSetTemplateSource struct {
	// Name — ReplicaSet and primary container name.
	// Supports template expressions.
	// Default when omitted: "{{ .metadata.name }}-replicaset"
	Name string `yaml:"name,omitempty" json:"name,omitempty" validate:"omitempty"`

	// Image — container image. Required (must be declared here or resolvable from CR).
	// Static:  "nginx:1.25"
	// Dynamic: "{{ .spec.image }}"
	Image string `yaml:"image" json:"image" validate:"omitempty"`

	// ImagePullSecrets is an optional list of references to secrets in the same namespace to use
	// for pulling any of the images used by this PodSpec.
	// If specified, these secrets will be passed to individual puller implementations for them to use.
	ImagePullSecrets []string `yaml:"imagePullSecrets,omitempty" json:"imagePullSecrets,omitempty" validate:"omitempty"`

	// Replicas — number of pod replicas as a string.
	// Static:  "3"
	// Dynamic: "{{ .spec.replicas }}"
	// Default: "1"
	Replicas string `yaml:"replicas,omitempty" json:"replicas,omitempty" validate:"omitempty"`

	// Port — primary container port as a string.
	// Static:  "8080"
	// Dynamic: "{{ .spec.port }}"
	// Omit to expose no port.
	Port string `yaml:"port,omitempty" json:"port,omitempty" validate:"omitempty"`

	// Protocol — network protocol for the container port.
	// Accepted values: TCP (default), UDP, SCTP. Omit to use TCP.
	Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty" validate:"omitempty"`

	// Namespace — target namespace for the ReplicaSet.
	// Default when omitted: "{{ .metadata.namespace }}" (same namespace as the CR).
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty" validate:"omitempty"`

	// Labels — applied to the ReplicaSet ObjectMeta and the pod template.
	// Label values support template expressions.
	// Orkestra always adds: managed-by=orkestra, orkestra-owner=<cr-name>
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty" validate:"omitempty"`

	// Annotations — applied to the ReplicaSet ObjectMeta only.
	// Annotation values support template expressions.
	Annotations Labels `yaml:"annotations,omitempty" json:"annotations,omitempty" validate:"omitempty"`

	// Resources — CPU and memory requests/limits for the primary container.
	// Set resources.profile for a named preset, or resources.requests/limits for
	// explicit values. Profile and explicit values are mutually exclusive.
	//
	//	resources:
	//	  profile: burst
	Resources *ResourceRequirements `yaml:"resources,omitempty" json:"resources,omitempty" validate:"omitempty"`

	// Env — environment variables for the primary container, in Kubernetes-native list format.
	// Each entry has a name and either a value or a valueFrom source.
	// If omitted, no environment variables are added.
	//
	//	env:
	//	  - name: LOG_LEVEL
	//	    value: info
	//	  - name: API_KEY
	//	    valueFrom:
	//	      secretKeyRef:
	//	        name: myapp-secrets
	//	        key: api-key
	Env EnvVarList `yaml:"env,omitempty" json:"env,omitempty"`

	// EnvFrom — bulk-load environment variables from Secrets and/or ConfigMaps
	// into the primary container, in addition to any individual entries in env.
	// Each secretRef/configMapRef entry names an existing Secret or ConfigMap;
	// every key in it becomes an environment variable.
	//
	//	envFrom:
	//	  secretRef:
	//	    - name: myapp-secrets
	//	  configMapRef:
	//	    - name: myapp-config
	EnvFrom *EnvFrom `yaml:"envFrom,omitempty" json:"envFrom,omitempty"`

	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// Selector which must match a node's labels for the pod to be scheduled on that node.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +optional
	// +mapType=atomic
	NodeSelector map[string]string `yaml:"nodeSelector,omitempty" json:"nodeSelector,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run this pod.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/
	// +optional
	ServiceAccountName string `yaml:"serviceAccountName,omitempty" json:"serviceAccountName,omitempty"`

	// Reconcile: true — also apply this declaration as drift correction on every
	// reconcile. Equivalent to declaring the same entry under both onCreate and
	// onReconcile. When false (default), only runs on onCreate (idempotent create).
	Reconcile bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty" validate:"omitempty"`

	// Conditions (when) — AND semantics.
	Conditions []Condition `yaml:"when,omitempty" json:"when,omitempty"`

	// ForEach declares dynamic expansion over a list field.
	ForEach *ForEachSpec `yaml:"forEach,omitempty" json:"forEach,omitempty"`

	// Autoscale declares workload autoscaling behaviour for this ReplicaSet.
	Autoscale *WorkloadAutoscale `yaml:"autoscale,omitempty" json:"autoscale,omitempty"`

	// Or holds OR conditions — at least one must pass for this resource.
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// WorkingDirectory sets the container's working directory (container.WorkingDir).
	WorkingDirectory string `yaml:"workingDirectory,omitempty" json:"workingDirectory,omitempty"`

	// Probes — startup, liveness, and readiness probe configuration.
	Probes *ProbesConfig `yaml:"probes,omitempty" json:"probes,omitempty"`

	// SecurityContext — container-level security settings.
	// Set securityContext.profile for a named preset (baseline, restricted, hardened)
	// or declare individual fields. Profile and explicit fields are mutually exclusive.
	SecurityContext *ContainerSecurityContext `yaml:"securityContext,omitempty" json:"securityContext,omitempty"`

	// PodSecurity — pod-level security settings applied to the pod spec.
	// Set podSecurity.profile for a named preset or declare individual fields.
	PodSecurity *PodSecurityContext `yaml:"podSecurity,omitempty" json:"podSecurity,omitempty"`

	// RollingUpdate — rolling update strategy for this ReplicaSet.
	// Set rollingUpdate.profile for a named preset (safe, fast, blue-green),
	// or declare maxSurge/maxUnavailable explicitly.
	RollingUpdate *RollingUpdateBehavior `yaml:"rollingUpdate,omitempty" json:"rollingUpdate,omitempty"`

	// Volumes — pod volumes available for mounting into the container.
	Volumes []VolumeSource `yaml:"volumes,omitempty" json:"volumes,omitempty"`

	// VolumeMounts — mounts for the primary container.
	VolumeMounts []VolumeMount `yaml:"volumeMounts,omitempty" json:"volumeMounts,omitempty"`

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string `json:"sleep,omitempty" yaml:"sleep,omitempty"`

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool `yaml:"forceConflict,omitempty" json:"forceConflict,omitempty"`
}
