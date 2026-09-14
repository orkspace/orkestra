package types

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// CustomResourceTemplateSource declares one arbitrary Kubernetes resource —
// any Kind not covered by one of Orkestra's built-in resource types (a
// third-party CRD such as cert-manager's Certificate, or your own CRD).
// It is schema-agnostic: the spec block is written and applied exactly as
// declared in the Katalog, with no built-in knowledge of its structure.
// Kubernetes enforces the resource's own CRD schema at apply time.
//
// Example:
//
//	onCreate:
//	  custom:
//	    - apiVersion: "cert-manager.io/v1"
//	      kind: Certificate
//	      metadata:
//	        name: "{{ .metadata.name }}-tls"
//	        namespace: "{{ .metadata.namespace }}"
//	      spec:
//	        secretName: "{{ .metadata.name }}-tls-secret"
//	        dnsNames:
//	          - "{{ .spec.hostname }}"
//	        issuerRef:
//	          name: letsencrypt-prod
//	          kind: ClusterIssuer
type CustomResourceTemplateSource struct {
	// APIVersion — the group/version of the target resource, e.g. "cert-manager.io/v1".
	// Required.
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`

	// Kind — the target resource's Kind, e.g. "Certificate". Required.
	Kind string `json:"kind" yaml:"kind"`

	// runtimeGVK is a cached GroupVersionKind derived from APIVersion + Kind.
	// It is intentionally not serialized to YAML/JSON.
	runtimeGVK *schema.GroupVersionKind `json:"-" yaml:"-"`

	// runtimeGVR is a cached GroupVersionResource resolved via RESTMapper.
	// It is intentionally not serialized to YAML/JSON.
	runtimeGVR *schema.GroupVersionResource `json:"-" yaml:"-"`

	// Metadata — name, namespace, labels, and annotations for the resource.
	// name is required. namespace is required for namespaced CRDs and must be
	// omitted for cluster-scoped ones; Orkestra determines the correct scope
	// via discovery unless namespaced is set explicitly.
	//
	//	metadata:
	//	  name: "{{ .metadata.name }}-tls"
	//	  namespace: "{{ .metadata.namespace }}"
	//	  labels:
	//	    app: "{{ .metadata.name }}"
	Metadata CustomResourceMetadata `json:"metadata" yaml:"metadata"`

	// Spec is the conventional spec block for CRDs. It is schema-agnostic and
	// may contain templated values. Only template syntax is validated by
	// Orkestra; structural/schema validation is deferred to the API server.
	Spec map[string]any `json:"spec,omitempty" yaml:"spec,omitempty"`

	// Status — an initial status block, useful when bootstrapping a resource
	// that expects one to be present immediately. Orkestra only writes this if
	// hasStatus resolves to true. Prefer letting the resource's own controller
	// populate status rather than setting this.
	Status map[string]any `json:"status,omitempty" yaml:"status,omitempty"`

	// Other captures any top-level fields that are not spec/status/metadata.
	// This supports CRDs that place configuration at the top level instead of
	// under spec. This field is inlined during YAML/JSON unmarshalling.
	Other map[string]any `json:"-" yaml:",inline"`

	// HasStatus is an explicit hint about whether the CRD exposes a status
	// subresource. Three states are useful:
	//   - nil: auto-detect via discovery at runtime
	//   - true: force status writes (patches)
	//   - false: never attempt status writes
	// Use this to avoid API errors for CRDs that do not support status.
	HasStatus *bool `json:"hasStatus,omitempty" yaml:"hasStatus,omitempty"`

	// Reconcile: true — also apply this declaration as drift correction on every
	// reconcile. Equivalent to declaring the same entry under both onCreate and
	// onReconcile. When false (default), only runs on onCreate (idempotent create).
	Reconcile bool `yaml:"reconcile" json:"reconcile,omitempty"`

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

	// Or holds OR conditions — at least one must pass for this resource to be created.
	// Works alongside the existing Conditions (when:) field which uses AND semantics.
	//
	//	or:
	//	  - field: spec.tier
	//	    equals: pro
	//	  - field: spec.tier
	//	    equals: enterprise
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// ForEach declares dynamic expansion over a list field.
	// When set, one source declaration becomes N declarations — one per list element.
	// .item and .<as> are available in template expressions within this declaration.
	//
	//	forEach:
	//	  field: spec.regions
	//	  as: region
	ForEach *ForEachSpec `yaml:"forEach,omitempty" json:"forEach,omitempty"`

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string `json:"sleep,omitempty" yaml:"sleep,omitempty"`

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool `yaml:"forceConflict,omitempty" json:"forceConflict,omitempty"`
}

// CustomResourceMetadata mirrors the small subset of metav1.ObjectMeta that
// Orkestra needs for templating, identity, and children tracking.
//
// Important notes:
//   - Name is required after templating. The reconciler will error if Name is empty.
//   - Namespace is optional in the struct but validation enforces it for namespaced
//     CRDs. For cluster-scoped CRDs Namespace must be empty.
//   - Labels and Annotations must conform to Kubernetes key/value rules. Use the
//     existing label/annotation validators in the codebase.
type CustomResourceMetadata struct {
	// Name is the resource name. Must be a valid Kubernetes name after templating.
	Name string `json:"name" yaml:"name"`

	// Namespace is optional in the struct but required for namespaced CRDs.
	// Validation will enforce presence when the target GVK is namespaced.
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`

	// Namespaced indicates whether the target GVK is intended to be namespaced.
	// Three states are useful and intentionally supported:
	//   - nil: unspecified; reconciler should treat this as "default to namespaced"
	//          and may auto-detect the real scope via discovery at runtime.
	//   - pointer to true: explicitly namespaced; metadata.namespace is required.
	//   - pointer to false: explicitly cluster-scoped; metadata.namespace must be empty.
	//
	// Defaulting/validation contract:
	// - Validation should treat nil as "namespaced by default" for user ergonomics.
	// - At runtime the reconciler should still consult discovery/RESTMapper to
	//   confirm the actual CRD scope and register the GVK with activateMissing if
	//   the CRD is not present yet.
	// - If the user sets Namespaced=false but discovery shows the CRD is namespaced,
	//   the reconciler should surface a clear validation error.
	Namespaced *bool `json:"namespaced,omitempty" yaml:"namespaced,omitempty"`

	// Labels are used for selection, ownership, and children tracking.
	// Keys and values must conform to Kubernetes label syntax.
	Labels Labels `json:"labels,omitempty" yaml:"labels,omitempty"`

	// Annotations are free-form metadata. Keys must conform to Kubernetes
	// annotation key syntax. Values are arbitrary strings.
	Annotations Labels `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

// IsNamespaced returns whether the declaration intends the resource to be
// namespaced. It implements Orkestra's defaulting rule: unspecified (nil)
// defaults to true (namespaced). Callers should still verify actual CRD scope
// via discovery before performing API operations.
func (c *CustomResourceTemplateSource) IsNamespaced() bool {
	if c == nil {
		return true // defensive default
	}

	if c.Metadata.Namespaced == nil {
		return true // default to namespaced for ergonomics
	}
	return *c.Metadata.Namespaced
}

// WithStatus returns whether Orkestra should attempt to write/patch the
// resource's status subresource.
//
// Behaviour:
// - If HasStatus is explicitly set, return that value.
// - If HasStatus is nil and the user provided a non-empty Status block, return true.
// - Otherwise return false to indicate runtime discovery should decide.
//
// Callers should treat false as "do not patch status" and rely on discovery
// to override when HasStatus is nil and the reconciler has discovered the
// CRD supports a status subresource.
func (c *CustomResourceTemplateSource) WithStatus() bool {
	// Explicit override
	if c.HasStatus != nil {
		return *c.HasStatus
	}
	// If the user provided a status block, assume intent to use status.
	if len(c.Status) > 0 {
		return true
	}
	// Otherwise defer to runtime discovery (caller should perform discovery).
	return false
}

// CustomResourceMeta returns the metadata block. It returns the concrete
// metadata struct rather than a pointer to encourage callers to treat the
// returned value as a snapshot. Mutations to the returned value do not affect
// the original CustomResource unless explicitly assigned back.
func (c *CustomResourceTemplateSource) CustomResourceMeta() CustomResourceMetadata {
	return c.Metadata
}

// HasLabels reports whether the resource has any labels after templating.
// Use this to decide whether to merge labels or to skip label-based selection.
func (c *CustomResourceTemplateSource) HasLabels() bool {
	return len(c.Metadata.Labels) > 0
}

// HasAnnotations reports whether the resource has any annotations after templating.
// Use this to decide whether to merge annotations or to skip annotation-based logic.
func (c *CustomResourceTemplateSource) HasAnnotations() bool {
	return len(c.Metadata.Annotations) > 0
}

// BuildGVK parses APIVersion and Kind and returns a GroupVersionKind.
// It caches the result on the receiver.
//
// Behaviour:
// - Uses schema.ParseGroupVersion to correctly handle group/version parsing.
// - Returns an error if APIVersion or Kind are empty or invalid.
func (c *CustomResourceTemplateSource) BuildGVK() (schema.GroupVersionKind, error) {
	if c == nil {
		return schema.GroupVersionKind{}, fmt.Errorf("custom resource is nil")
	}
	if c.APIVersion == "" {
		return schema.GroupVersionKind{}, fmt.Errorf("apiVersion is empty")
	}
	if c.Kind == "" {
		return schema.GroupVersionKind{}, fmt.Errorf("kind is empty")
	}

	gv, err := schema.ParseGroupVersion(c.APIVersion)
	if err != nil {
		return schema.GroupVersionKind{}, fmt.Errorf("invalid apiVersion %q: %w", c.APIVersion, err)
	}

	gvk := gv.WithKind(c.Kind)
	c.runtimeGVK = &gvk
	return gvk, nil
}

// RuntimeGVK returns the cached GVK if present, otherwise builds it.
func (c *CustomResourceTemplateSource) RuntimeGVK() (schema.GroupVersionKind, error) {
	if c == nil {
		return schema.GroupVersionKind{}, fmt.Errorf("custom resource is nil")
	}
	if c.runtimeGVK != nil {
		return *c.runtimeGVK, nil
	}
	return c.BuildGVK()
}

// ResolveGVR resolves the GroupVersionResource for the CR's GVK using the
// provided RESTMapper and caches the result.
//
// Behaviour:
// - Builds the GVK if not already cached.
// - Calls mapper.RESTMapping(gvk.GroupKind(), gvk.Version).
// - Returns the resolved GVR or the underlying error from the mapper.
func (c *CustomResourceTemplateSource) ResolveGVR(mapper meta.RESTMapper) (schema.GroupVersionResource, error) {
	if c == nil {
		return schema.GroupVersionResource{}, fmt.Errorf("custom resource is nil")
	}
	if mapper == nil {
		return schema.GroupVersionResource{}, fmt.Errorf("restmapper is nil")
	}

	gvk, err := c.RuntimeGVK()
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to resolve GVR for %s: %w", gvk.String(), err)
	}

	gvr := mapping.Resource
	c.runtimeGVR = &gvr
	return gvr, nil
}

// RuntimeGVR returns the cached GVR if present. If not cached, instructs the
// caller to call ResolveGVR with a RESTMapper first.
func (c *CustomResourceTemplateSource) RuntimeGVR() (schema.GroupVersionResource, error) {
	if c == nil {
		return schema.GroupVersionResource{}, fmt.Errorf("custom resource is nil")
	}
	if c.runtimeGVR == nil {
		return schema.GroupVersionResource{}, fmt.Errorf("gvr not resolved; call ResolveGVR(restMapper) first")
	}
	return *c.runtimeGVR, nil
}

// GVKString returns a human-friendly GVK string. Falls back to APIVersion/Kind
// if building the GVK fails.
func (c *CustomResourceTemplateSource) GVKString() string {
	if c == nil {
		return "<nil-custom-resource>"
	}
	if c.runtimeGVK != nil {
		return c.runtimeGVK.String()
	}
	// best-effort build
	if gvk, err := c.BuildGVK(); err == nil {
		return gvk.String()
	}
	return fmt.Sprintf("%s %s", c.APIVersion, c.Kind)
}

// GVRString returns a human-friendly GVR string if resolved, otherwise a placeholder.
func (c *CustomResourceTemplateSource) GVRString() string {
	if c == nil {
		return "<nil-custom-resource>"
	}
	if c.runtimeGVR != nil {
		return c.runtimeGVR.String()
	}
	return "<gvr-not-resolved>"
}
