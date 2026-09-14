// pkg/types/types_pod.go
package types

// ── Pod ───────────────────────────────────────────────────────────────────────

// PodTemplateSource declares one Pod to be managed by Orkestra.
//
// Prefer DeploymentTemplateSource for long-running workloads.
// Deployments manage Pod restarts, rolling updates, and replica sets automatically.
// Use PodTemplateSource only when you need direct, single-instance Pod control.
//
// Example:
//
//	onCreate:
//	  pods:
//	    - name: "{{ .metadata.name }}-worker"
//	      image: "{{ .spec.workerImage }}"
//	      port: "9090"
type PodTemplateSource struct {
	// Name — Pod name.
	// Default when omitted: "{{ .metadata.name }}-pod"
	Name string `yaml:"name,omitempty" json:"name,omitempty" validate:"omitempty"`

	// Image — container image. Required.
	// Static: "busybox:1.35" or Dynamic: "{{ .spec.image }}"
	Image string `yaml:"image" json:"image" validate:"omitempty"`

	// ImagePullSecrets is an optional list of references to secrets in the same namespace to use
	// for pulling any of the images used by this PodSpec.
	// If specified, these secrets will be passed to individual puller implementations for them to use.
	ImagePullSecrets []string `yaml:"imagePullSecrets,omitempty" json:"imagePullSecrets,omitempty" validate:"omitempty"`

	// Port — container port as a string.
	// Static: "8080" or Dynamic: "{{ .spec.port }}"
	Port string `yaml:"port,omitempty" json:"port,omitempty" validate:"omitempty"`

	// Protocol — network protocol for the container port.
	// Accepted values: TCP (default), UDP, SCTP. Omit to use TCP.
	Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty" validate:"omitempty"`

	// Namespace — target namespace.
	// Default when omitted: "{{ .metadata.namespace }}"
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty" validate:"omitempty"`

	// Labels — applied to Pod metadata. Values support template expressions.
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty" validate:"omitempty"`

	// Annotations — applied to Pod metadata. Values support template expressions.
	Annotations Labels `yaml:"annotations,omitempty" json:"annotations,omitempty" validate:"omitempty"`

	// Resources — CPU and memory requests/limits for the primary container.
	// Set resources.profile for a named preset, or resources.requests/limits for
	// explicit values. Profile and explicit values are mutually exclusive.
	//
	//	resources:
	//	  profile: burst
	//
	//	resources:
	//	  requests:
	//	    cpu: 100m
	//	    memory: 128Mi
	//	  limits:
	//	    cpu: 500m
	//	    memory: 512Mi
	Resources *ResourceRequirements `yaml:"resources,omitempty" json:"resources,omitempty" validate:"omitempty"`

	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// Selector which must match a node's labels for the pod to be scheduled on that node.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	NodeSelector map[string]string `yaml:"nodeSelector,omitempty" json:"nodeSelector,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run this pod.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/
	ServiceAccountName string `yaml:"serviceAccountName,omitempty" json:"serviceAccountName,omitempty"`

	// Conditions declares the set of runtime predicates that must all evaluate to
	// true for this resource template to be applied during reconciliation.
	//
	// Each condition inspects a field on the live Custom Resource using dot-notation
	// (e.g. "spec.enabled", "metadata.labels.tier") and compares it against a value
	// using the chosen operator. All conditions in the list are AND‑ed together.
	//
	// If any condition fails, the resource is skipped for that reconcile cycle.
	// This is not an error — it simply means "do not create/update this resource
	// right now". This enables expressive, data‑driven orchestration such as:
	//
	//   when:
	//     - field: spec.exposePublicly
	//       equals: "true"
	//     - field: spec.environment
	//       prefix: "prod"
	//
	// Conditions allow templates to be selectively activated based on the CR's
	// state, enabling dynamic topologies, feature flags, environment‑specific
	// behavior, and conditional provisioning without writing Go code.
	Conditions []Condition `yaml:"when,omitempty" json:"when,omitempty"`

	// ForEach declares dynamic expansion over a list field.
	// When set, one source declaration becomes N declarations — one per list element.
	// .item and .<as> are available in template expressions within this declaration.
	//
	//	forEach:
	//	  field: spec.regions
	//	  as: region
	ForEach *ForEachSpec `yaml:"forEach,omitempty" json:"forEach,omitempty"`

	// Or holds OR conditions — at least one must pass for this resource to be created.
	// Works alongside the existing Conditions (when:) field which uses AND semantics.
	//
	//	or:
	//	  - field: spec.tier
	//	    equals: pro
	//	  - field: spec.tier
	//	    equals: enterprise
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// Probes — startup, liveness, and readiness probe configuration.
	//
	//	probes:
	//	  liveness:
	//	    type: http
	//	    path: /healthz
	//	    profile: standard
	Probes *ProbesConfig `yaml:"probes,omitempty" json:"probes,omitempty"`

	// Reconcile: true — also apply this declaration as drift correction on every
	// reconcile, not just on create. Equivalent to declaring the same entry under
	// onReconcile. When false (default), only runs on onCreate (idempotent create).
	Reconcile bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty" validate:"omitempty"`

	// SecurityContext — container-level security settings.
	// Set securityContext.profile for a named preset (baseline, restricted, hardened)
	// or declare individual fields. Profile and explicit fields are mutually exclusive.
	//
	//	securityContext:
	//	  profile: restricted
	SecurityContext *ContainerSecurityContext `yaml:"securityContext,omitempty" json:"securityContext,omitempty"`

	// PodSecurity — pod-level security settings applied to the pod spec.
	// Set podSecurity.profile for a named preset or declare individual fields.
	//
	//	podSecurity:
	//	  profile: baseline
	PodSecurity *PodSecurityContext `yaml:"podSecurity,omitempty" json:"podSecurity,omitempty"`

	// Volumes — pod volumes available for mounting into the container.
	//
	//	volumes:
	//	  - name: config
	//	    configMap:
	//	      name: myapp-config
	Volumes []VolumeSource `yaml:"volumes,omitempty" json:"volumes,omitempty"`

	// VolumeMounts — mounts for the primary container.
	//
	//	volumeMounts:
	//	  - name: config
	//	    mountPath: /etc/myapp
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
