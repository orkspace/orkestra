// pkg/types/types_crd_entry.go
package types

import (
	"sort"

	"github.com/orkspace/orkestra/domain"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ── CRDEntry ──────────────────────────────────────────────────────────────────
// One entry per CRD in the Katalog.
//
// YAML fields are populated by the YAML parser when running in YAML mode,
// or set directly in BuildKatalogFromGo() when running in Go mode.
//
// Fields tagged yaml:"-" are populated at runtime during Katalog validation
// and wiring — they are never parsed from YAML and never set manually.

type CRDEntry struct {
	// ── Identity ──────────────────────────────────────────────────────────────

	// Name — unique CRD identifier within the Katalog. Must be lowercase.
	// Injected from the map key during loading — never set from YAML.
	Name string `yaml:"-" json:"name" validate:"required,hostname_rfc1123"`

	// KatalogName — unique identifier for the katalog in the runtime.
	KatalogName string `yaml:"katalogName,omitempty" json:"katalogName,omitempty"`

	// KatalogNamespace — the namespace this CRD's Katalog belongs to.
	// Defaults to "default" when not declared. Used by the Control Center to
	// group CRDs by team/tenant within a single runtime.
	KatalogNamespace string `yaml:"katalogNamespace,omitempty" json:"katalogNamespace,omitempty"`

	// KatalogDescription — the description from the source Katalog's metadata.
	// Falls back to the Komposer's description when the sub-Katalog has none.
	KatalogDescription string `yaml:"katalogDescription,omitempty" json:"katalogDescription,omitempty"`

	// KatalogVersion — the version from the source Katalog's metadata.
	// Falls back to the Komposer's version when the sub-Katalog has none.
	KatalogVersion string `yaml:"katalogVersion,omitempty" json:"katalogVersion,omitempty"`

	// CrossAccess controls whether other Katalogs can read this CRD's CR state
	// via the cross: block. Defaults to true (readable). Set to false to opt
	// this CRD out of cross reads — the reconciler returns empty for any
	// cross: reference that targets an opted-out CRD.
	CrossAccess *bool `yaml:"crossAccess,omitempty" json:"crossAccess,omitempty"`

	// Enabled — include this CRD in the runtime. false = skipped entirely.
	// WARNING: only set to false after stripping Orkestra finalizers from all
	// live CRs — disabled CRDs with live finalizers will cause stuck objects.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// Critical — if true, Orkestra marks the entire controller as degraded when
	// this CRD's health state transitions to degraded.
	// Use for CRDs that are fundamental to the platform's correctness.
	// Critical *bool `yaml:"critical,omitempty" json:"critical,omitempty"`

	// Description — human-readable description. Shown in /katalog API responses.
	Description string `yaml:"description,omitempty" json:"description,omitempty" validate:"omitempty"`

	// Mode — see CRDMode for full documentation.
	// Auto-detected when omitted based on whether apiTypes.location is set.
	Mode CRDMode `yaml:"mode,omitempty" json:"mode,omitempty" validate:"omitempty,oneof=typed dynamic"`

	// ── API Types ─────────────────────────────────────────────────────────────
	// See APITypes for full field documentation.
	APITypes APITypes `yaml:"apiTypes" json:"apiTypes" validate:"required"`

	// CRDFile is the path to the CRD YAML file to apply before operator start.
	// Supports relative (resolved from katalog file location), absolute, or
	// remote (https://…) paths.
	//
	// Only applied when running outside the cluster (dev mode via ork run).
	// In production, CRDs must be pre-applied by the platform operator.
	//
	// During ork validate, the file is read and its group/kind are checked
	// against apiTypes to catch mismatches before deployment.
	CRDFile string `yaml:"crdFile,omitempty" json:"crdFile,omitempty"`

	// CRFiles is an ordered list of CR YAML files to apply before the runtime
	// starts. Applied in declaration order after the CRD is registered.
	// Same path resolution as CRDFile. Dev mode only.
	CRFiles []string `yaml:"crFiles,omitempty" json:"crFiles,omitempty"`

	// Setup declares prerequisite resources to apply before Orkestra starts.
	// Shorthand: a plain list of strings applies each file (backward compatible).
	Setup *SetupConfig `yaml:"setup,omitempty" json:"setup,omitempty"`

	// ── Runtime objects ───────────────────────────────────────────────────────
	// Set by addRuntimeObjects() during Katalog validation. Never set from YAML.
	//
	// Typed mode:        DynamicModeObject and ListDynamicModeObject are factory functions
	//                    from ObjectRegistry and ListRegistry respectively.
	//                    TypedModeObject and ListTypedModeObject are set in BuildKatalogFromGo().
	//
	// Dynamic mode: DynamicModeObject and ListDynamicModeObject are factory functions
	//                    that return *unstructured.Unstructured and *unstructured.UnstructuredList.
	//                    These are always set by addRuntimeObjects() — never nil after validation.
	TypedModeObject       runtime.Object        `yaml:"-" json:"-"`
	ListTypedModeObject   runtime.Object        `yaml:"-" json:"-"`
	DynamicModeObject     func() runtime.Object `yaml:"-" json:"-"`
	ListDynamicModeObject func() runtime.Object `yaml:"-" json:"-"`

	// Scheme — AddToScheme function generated by controller-gen for this API type.
	// Required for typed mode so the REST client can decode API server responses.
	// Not needed for dynamic mode — the dynamic client bypasses scheme decoding.
	// Set in BuildKatalogFromGo() for Go mode. Handled by RegisterScheme() for YAML mode.
	Scheme func(s *runtime.Scheme) error `yaml:"-" json:"-"`

	// ── Computed GVK/GVR ─────────────────────────────────────────────────────
	// Set by setGroupVersionKind() during Katalog validation.
	// Derived from APITypes fields. Never set manually.
	GroupVersion         *schema.GroupVersion        `yaml:"-" json:"-"`
	GroupVersionKind     schema.GroupVersionKind     `yaml:"-" json:"-"`
	GroupVersionResource schema.GroupVersionResource `yaml:"-" json:"-"`

	// ── Scope ─────────────────────────────────────────────────────────────────

	// Namespaced — true if this CRD is namespace-scoped, false if cluster-scoped.
	// Default is true
	Namespaced *bool `yaml:"namespaced,omitempty" json:"namespaced,omitempty"`

	// Namespace — target namespace for namespace-scoped CRDs.
	// Informer watches this namespace only. Empty = all namespaces.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty" validate:"omitempty"`

	// ── Runtime behaviour ─────────────────────────────────────────────────────

	// WorkersActive — records number of active concurrent reconcile workers for this CRD.
	WorkersActive int `yaml:"workersActive,omitempty" json:"workersActive,omitempty" validate:"omitempty,gte=1,lte=50"`

	// DependsOn — names of other CRDs that must reach a condition before this one starts.
	// Orkestra resolves the dependency graph and starts CRDs in topological order.
	// Cycle detection runs at validation time — cycles fail fast with a clear error.
	// Supports three YAML formats (list, key-value, full map) — see DependsOnMap.
	DependsOn DependsOnMap `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`

	// ── Enrichment ─────────────────────────────────────────────────────────────
	// Controls which secondary data is fetched and embedded into child resource
	// maps before they are available in template context.
	//
	// Only one of EnrichAll or Enrich may be set — setting both is a validation error.
	//
	//	enrichAll: true    — enrich all supported resource types
	//	enrich: [pods]     — enrich only the listed targets (shorthand)
	//	enrich:            — conditional enrichment (struct form)
	//	  - events:
	//	      when:
	//	        - field: "{{ replicasReady .children.deployment }}"
	//	          equals: "false"
	EnrichAll bool           `yaml:"enrichAll,omitempty" json:"enrichAll,omitempty"`
	Enrich    []EnrichTarget `yaml:"enrich,omitempty"    json:"enrich,omitempty"`

	// ── OperatorBox ────────────────────────────────────────────────────
	OperatorBox OperatorBoxConfig `yaml:"operatorBox,omitempty" json:"operatorBox,omitempty"`

	// Labels specifies additional metadata labels to attach to the CR of this CRD entry.
	// These labels can be used for organization, watch filtering, or identification purposes.
	// They are not used for reconciliation filtering (see LabelSelector for that).
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty" validate:"omitempty"`

	// LabelSelector filters which resources this CRD entry reconciles.
	// Only resources whose labels match ALL declared key-value pairs are watched.
	// Required for built-in types (ConfigMap, Pod, etc.) — without a selector,
	// Orkestra would reconcile every instance in the cluster.
	// For custom CRDs this is optional — can narrow scope within a CRD.
	LabelSelector Labels `yaml:"labelSelector,omitempty"`

	// FieldSelector filters which resources this CRD entry reconciles.
	// Only resources whose *fields* match ALL declared key-value expressions
	// are listed or watched. Field selectors operate on the server side and
	// support exact-match comparisons on well-known metadata paths
	// (e.g. "metadata.name", "metadata.namespace").
	//
	// Unlike label selectors, field selectors cannot match arbitrary user-defined
	// keys — only fields exposed by the Kubernetes API server. They are evaluated
	// before any client-side filtering, reducing load on the informer pipeline.
	//
	// Common use cases:
	//   - Restricting reconciliation to a specific namespace:
	//       {key: "metadata.namespace", value: "default"}
	//   - Targeting a single object by name:
	//       {key: "metadata.name", value: "my-config"}
	//
	// Field selectors are optional for all types. When omitted, Orkestra will
	// watch all objects permitted by LabelSelector and namespace restrictions.
	FieldSelector Labels `yaml:"fieldSelector,omitempty"`

	// RegistryRef is the OCI or Git reference this CRD entry was loaded from.
	// Set by the merger after loadRegistrySource resolves the pattern.
	// Empty for CRDs loaded from local file sources.
	RegistryRef string `yaml:"-" json:"-"`

	// IsBuiltIn is set to true when this CRD entry was enriched from the
	// built-in Kubernetes resource registry. Used for ork validate output
	// and informational logging only — does not affect runtime behavior.
	IsBuiltIn bool `yaml:"-" json:"-"` // never serialized — runtime state only

	// IgnoreStatusPatch reports whether or not to patch the status of this CRD
	IgnoreStatusPatch bool `yaml:"ignoreStatusPatch,omitempty" json:"ignoreStatusPatch,omitempty"`

	// IgnoreObservedGeneration reports whether or not to ignore the observedGeneration field for this CRD.
	IgnoreObservedGeneration bool `yaml:"ignoreObservedGeneration,omitempty" json:"ignoreObservedGeneration,omitempty"`

	// IsStatusless reports whether this CRD has no meaningful readiness semantics.
	// These resources become "Ready" immediately upon creation.
	IsStatusless bool `yaml:"-" json:"IsStatusless,omitempty"`

	// BuiltInGroup is the display name of the API group for built-in resources.
	// "core" for resources in the core group (empty string group).
	// Only set when IsBuiltIn is true.
	BuiltInGroup string `yaml:"-" json:"-"` // never serialized

	// EnrichmentOutcome records the result of the API type enrichment phase.
	// During validation, built‑in Kubernetes kinds (e.g., Pod, Deployment, Secret)
	// are automatically enriched with their full API metadata — group, version,
	// plural, API path, and namespaced scope. This allows users to specify only:
	//
	//	apiTypes:
	//	  kind: Pod
	//
	// and rely on Orkestra to resolve all remaining fields based on the
	// Kubernetes discovery API. Custom resources are enriched using their declared
	// group/version/kind. This field is never serialized and is used internally to
	// report enrichment status and drive downstream runtime behavior.
	EnrichmentOutcome EnrichmentOutcome `yaml:"-" json:"-"` // never serialized

	// Endpoints defines which operator HTTP endpoints are enabled for this CRD.
	Endpoints EndpointsConfig `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`

	// Restricted Namespaces
	RestrictedNamespaces RestrictedNamespaces `yaml:"restrictedNamespaces,omitempty" json:"restrictedNamespaces,omitempty"`

	// Allowed Namespaces
	AllowedNamespaces AllowedNamespaces `yaml:"allowedNamespaces,omitempty" json:"allowedNamespaces,omitempty"`

	// Conversion is useful for handling multi-version crd
	Conversion *CRDConversion `yaml:"conversion,omitempty" json:"conversion,omitempty"`

	// Validation is a list of rules
	Validation *ValidationConfig `yaml:"validation,omitempty" json:"validation,omitempty"`

	// Mutation is a list of rules
	Mutation *MutationConfig `yaml:"mutation,omitempty" json:"mutation,omitempty"`

	// Webhooks controls per-CRD admission webhook behaviour.
	// Only meaningful when ENABLE_ADMISSION_WEBHOOK=true.
	// By default, any CRD with Validation or Mutation rules is included
	// in the corresponding webhook configuration automatically.
	// Set validation: false or mutation: false to opt a specific CRD out of
	// admission-time interception while keeping its reconcile-time enforcement.
	Webhooks AdmissionWebhookConfig `yaml:"webhooks,omitempty" json:"webhooks,omitempty"`

	// Normalize Spec fields before rendering
	Normalize *NormalizeConfig `yaml:"normalize,omitempty"`

	// NotificationEnabled returns whether this CRD belongs to katalog with notification access
	NotificationEnabled *bool `yaml:"-" json:"-"`

	// TargetHookFactories — per-target hook factories, keyed by target name.
	// Populated by addTargetHooks() from TargetHookRegistry.
	// Only set for targets that declare a distinct hook binary from the CRD-level.
	// Targets that share the CRD-level binary (only overriding args) are absent —
	// mergeReconcilerConfig handles them at reconcile time via EffectiveOperatorBox.
	TargetHookFactories map[string]func() domain.AnyReconcileHooks `yaml:"-" json:"-"`

	// TargetReconcilerFactories — per-target constructor factories, keyed by target name.
	// Populated by addTargetConstructors() from TargetReconcilerRegistry.
	TargetReconcilerFactories map[string]NewReconcilerFunc `yaml:"-" json:"-"`

	// RemoveFinalizers -> testing
	RemoveFinalizers bool `yaml:"removeFinalizers,omitempty" json:"removeFinalizers,omitempty"`

	// DeletionProtection overrides the global deletion protection policy
	// for this specific CRD. If nil, both ProtectCRD and ProtectCRs default to true.
	DeletionProtection *DeletionProtectionOverride `yaml:"deletionProtection,omitempty" json:"deletionProtection,omitempty"`

	// Warnings collects non‑fatal validation messages for this CRD.
	Warnings Warnings `json:"-"` // not serialized

	// Imports declares Motif imports for this operatorBox.
	// Each import references a Motif by OCI reference, file path, or short name,
	// and binds its inputs via with:. Resources from imported Motifs are merged
	// into OnReconcile at Katalog load time.
	// Required inputs not provided in with: are a validation error.
	Imports []MotifImport `yaml:"imports,omitempty" json:"imports,omitempty"`

	// Serve exposes this CRD through the Gateway API as a stable delivery surface.
	// When enabled, the Control Center renders a [+ Create] button for this CRD
	// and serves its schema via GET /api/v1/schema/{kind}.
	Serve *ServeConfig `yaml:"serve,omitempty" json:"serve,omitempty"`

	// ForceConflict, when true, sets Force: true on every server-side apply
	// for the resources created on onCreate/onReconcile CRD, taking
	// ownership of conflicting fields instead of returning a conflict error.
	// Default: true.
	ForceConflict *bool `yaml:"forceConflict,omitempty" json:"forceConflict,omitempty"`
}

// EffectiveOperatorBox returns the operatorBox for a given target.
// When a target entry declares its own operatorBox, non-template fields
// (preReconcile, status) fall back to the CRD-level values when absent
// on the target — so a CRD-level gate or status config applies to all
// surfaces unless a target explicitly overrides it.
//
// Reconciler merge: target args override CRD-level args, but identity fields
// (location, function, alias, resources) and tuning fields (workers, resync,
// queue) always come from the CRD-level when the target omits them.
// HookFactory is runtime-registered on the CRD-level box only and is always
// propagated so the hook binary is reachable from every target surface.
func (c *CRDEntry) EffectiveOperatorBox(target string) *OperatorBoxConfig {
	if target == "" {
		return &c.OperatorBox
	}
	if c.Serve != nil && c.Serve.Target.Entries != nil {
		if cfg, ok := c.Serve.Target.Entries[target]; ok && cfg.OperatorBox != nil {
			box := *cfg.OperatorBox
			if box.PreReconcile == nil {
				box.PreReconcile = c.OperatorBox.PreReconcile
			}
			if box.Status == nil {
				box.Status = c.OperatorBox.Status
			}
			box.Reconciler = mergeReconcilerConfig(c.OperatorBox.Reconciler, box.Reconciler)
			// HookFactory is set at load time on the CRD-level box only.
			box.HookFactory = c.OperatorBox.HookFactory
			return &box
		}
	}
	return &c.OperatorBox
}

// mergeReconcilerConfig merges a per-target reconciler on top of the CRD-level one.
// Workers, resync, queue, and profile are always taken from the CRD-level (they stay
// fixed). Hook identity fields (location, function, alias, resources) come from the
// CRD-level when the target omits them. Args are merged key-by-key with target winning.
func mergeReconcilerConfig(base, target *ReconcilerConfig) *ReconcilerConfig {
	if target == nil {
		return base
	}
	if base == nil {
		return target
	}
	// Start from base so workers/resync/queue/profile/default/constructor are inherited.
	merged := *base
	if target.Hooks != nil {
		if merged.Hooks == nil {
			merged.Hooks = target.Hooks
		} else {
			h := *merged.Hooks
			// Identity fields: target wins only when explicitly set.
			if target.Hooks.Location != "" {
				h.Location = target.Hooks.Location
			}
			if target.Hooks.Function != "" {
				h.Function = target.Hooks.Function
			}
			if target.Hooks.Alias != "" {
				h.Alias = target.Hooks.Alias
			}
			if len(target.Hooks.ManagedResources) > 0 {
				h.ManagedResources = target.Hooks.ManagedResources
			}
			// Args: merge key-by-key; target overrides CRD-level per key.
			if len(target.Hooks.Args) > 0 {
				args := make(map[string]interface{}, len(h.Args)+len(target.Hooks.Args))
				for k, v := range h.Args {
					args[k] = v
				}
				for k, v := range target.Hooks.Args {
					args[k] = v
				}
				h.Args = args
			}
			merged.Hooks = &h
		}
	}
	return &merged
}

type ConversionVersionSpec struct {
	Version string                 `json:"version" yaml:"version"`
	Spec    map[string]interface{} `json:"spec" yaml:"spec"`
}

// EndpointsConfig controls which HTTP endpoints are exposed by the operator.
//
// This allows users to selectively enable/disable endpoints while keeping
// the configuration minimal and declarative.
type EndpointsConfig struct {
	// Enabled if false disables all endpoints for this CRD
	// Default is true
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// Health controls whether the /health endpoint is served.
	Health *bool `yaml:"health,omitempty" json:"health,omitempty"`

	// Info controls whether the /info endpoint is served.
	Info *bool `yaml:"info,omitempty" json:"info,omitempty"`
}

// ServeEnabled reports whether serve is configured and enabled for this CRD.
func (c *CRDEntry) ServeEnabled() bool {
	return c.Serve != nil && c.Serve.Enabled
}

// HasServeName reports whether serve.name is declared — the Gateway API only
// resolves and applies a name override when this is true.
func (c *CRDEntry) HasServeName() bool {
	return c.Serve != nil && c.Serve.Name != ""
}

// TargetModeEnabled reports whether target mode is enabled for this CRD.
// Defaults to true when serve.modes is omitted or when serve.modes.target is nil.
func (c *CRDEntry) TargetModeEnabled() bool {
	if !c.ServeEnabled() {
		return false
	}
	if c.Serve.Modes == nil || c.Serve.Modes.Target == nil {
		return true
	}
	return *c.Serve.Modes.Target
}

// FullCRModeEnabled reports whether full CR mode is enabled for this CRD.
// Defaults to true when serve.modes is omitted or when serve.modes.cr is nil.
func (c *CRDEntry) FullCRModeEnabled() bool {
	if !c.ServeEnabled() {
		return false
	}
	if c.Serve.Modes == nil || c.Serve.Modes.CR == nil {
		return true
	}
	return *c.Serve.Modes.CR
}

// HasServeModes reports whether serve.modes is explicitly configured.
func (c *CRDEntry) HasServeModes() bool {
	return c.ServeEnabled() && c.Serve.Modes != nil
}

// effectiveServeModes returns the effective modes for a target,
// merging target-level and CRD-level settings.
// Resolution order:
//  1. Target-level (serve.targets[<name>].modes)
//  2. CRD-level (serve.modes)
//  3. Default (both true)
func (c *CRDEntry) effectiveServeModes(target string) *ServeModes {
	result := &ServeModes{
		Target: boolPtr(true),
		CR:     boolPtr(true),
	}

	if !c.ServeEnabled() {
		return result
	}

	// 1. Start with CRD-level
	if c.Serve.Modes != nil {
		if c.Serve.Modes.Target != nil {
			result.Target = c.Serve.Modes.Target
		}
		if c.Serve.Modes.CR != nil {
			result.CR = c.Serve.Modes.CR
		}
	}

	// 2. Override with target-level (if set)
	if c.Serve.Target.Entries != nil {
		if cfg, ok := c.Serve.Target.Entries[target]; ok && cfg.Modes != nil {
			if cfg.Modes.Target != nil {
				result.Target = cfg.Modes.Target
			}
			if cfg.Modes.CR != nil {
				result.CR = cfg.Modes.CR
			}
		}
	}

	return result
}

// TargetModeEnabledFor returns whether target mode is enabled for the given target.
func (c *CRDEntry) TargetModeEnabledFor(target string) bool {
	if !c.ServeEnabled() {
		return false
	}
	return *c.effectiveServeModes(target).Target
}

// FullCRModeEnabledFor returns whether CR mode is enabled for the given target.
func (c *CRDEntry) FullCRModeEnabledFor(target string) bool {
	if !c.ServeEnabled() {
		return false
	}
	return *c.effectiveServeModes(target).CR
}

func boolPtr(b bool) *bool { return &b }

// effectiveServeApplyOverrides returns the effective override
// for a target, merging target-level and CRD-level settings.
func (c *CRDEntry) effectiveServeApplyOverrides(target string) *ServeApplyOverrides {
	result := &ServeApplyOverrides{
		TargetConflict:   boolPtr(true), // default: allow
		ResourceConflict: boolPtr(true), // default: allow
	}

	// 1. Start with CRD-level
	if c.Serve.Apply != nil && c.Serve.Apply.Overrides != nil {
		if c.Serve.Apply.Overrides.TargetConflict != nil {
			result.TargetConflict = c.Serve.Apply.Overrides.TargetConflict
		}
		if c.Serve.Apply.Overrides.ResourceConflict != nil {
			result.ResourceConflict = c.Serve.Apply.Overrides.ResourceConflict
		}
	}

	// 2. Override with target-level (if set)
	if c.Serve.Target.Entries != nil {
		if cfg, ok := c.Serve.Target.Entries[target]; ok && cfg.Apply != nil && cfg.Apply.Overrides != nil {
			if cfg.Apply.Overrides.TargetConflict != nil {
				result.TargetConflict = cfg.Apply.Overrides.TargetConflict
			}
			if cfg.Apply.Overrides.ResourceConflict != nil {
				result.ResourceConflict = cfg.Apply.Overrides.ResourceConflict
			}
		}
	}

	return result
}

func (c *CRDEntry) ServeForceConflictEnabledFor(target string) bool {
	if !c.ServeEnabled() {
		return false
	}
	override := c.effectiveServeApplyOverrides(target)
	return *override.ResourceConflict
}

func (c *CRDEntry) ServeTargetOverrideEnabledFor(target string) bool {
	if !c.ServeEnabled() {
		return false
	}
	override := c.effectiveServeApplyOverrides(target)
	return *override.TargetConflict
}

// HasServeFields reports whether this CRD declares any serve.fields.
func (c *CRDEntry) HasServeFields() bool {
	return c.ServeEnabled() && c.Serve.Fields != nil && len(c.Serve.Fields) > 0
}

// HasServeTokenRestrictions reports whether this CRD declares any token restrictions.
func (c *CRDEntry) HasServeTokenRestrictions() bool {
	if c.Serve == nil {
		return false
	}
	return c.ServeEnabled() && c.Serve.HasTokenRestrictions()
}

// RequireServeName reports whether an Gateway API caller (and the Control Center
// form) must supply metadata.name themselves — true unless serve.name is
// declared, in which case the name is resolved server-side instead.
func (c *CRDEntry) RequireServeName() bool {
	return !c.HasServeName()
}

// IsServeRequiredField reports whether the given field name is declared as a
// required field in the serve configuration.
//
// A field is considered required if:
//   - It exists in serve.fields with required: true, or
//   - It exists in serve.labels with required: true, or
//   - It exists in serve.annotations with required: true
//
// Used by the Control Center to mark form fields as required and by the
// gateway to validate target-mode requests before building the CR.
func (c *CRDEntry) IsServeRequiredField(field string) bool {
	if c.Serve == nil {
		return false
	}
	if _, ok := c.Serve.Fields[field]; ok {
		return true
	}
	if _, ok := c.Serve.Labels[field]; ok {
		return true
	}
	if _, ok := c.Serve.Annotations[field]; ok {
		return true
	}
	return false
}

// HasServeNamespace reports whether serve.namespace is declared — the Gateway API
// only resolves and applies a namespace override when this is true.
func (c *CRDEntry) HasServeNamespace() bool {
	return c.Serve != nil && c.Serve.Namespace != ""
}

// ServeFields returns serve.fields. Nil-safe.
func (c *CRDEntry) ServeFields() map[string]ServeFieldConfig {
	if c.Serve == nil {
		return nil
	}
	return c.Serve.Fields
}

// ServeLabels returns serve.labels. Nil-safe.
func (c *CRDEntry) ServeLabels() map[string]ServeFieldConfig {
	if c.Serve == nil {
		return nil
	}
	return c.Serve.Labels
}

// ServeAnnotations returns serve.annotations. Nil-safe.
func (c *CRDEntry) ServeAnnotations() map[string]ServeFieldConfig {
	if c.Serve == nil {
		return nil
	}
	return c.Serve.Annotations
}

// HasServeLabelsOrAnnotations reports whether this CRD declares any serve labels or annotations.
func (c *CRDEntry) HasServeLabelsOrAnnotations() bool {
	return len(c.ServeLabels()) > 0 || len(c.ServeAnnotations()) > 0
}

// ─── Serve Response Config Methods ────────────────────────────────────────────

// ServeAliases returns all enabled non-primary target entries.
// Disabled entries and the primary entry are excluded.
// Returns nil when no qualifying entries exist.
func (c *CRDEntry) ServeAliases() map[string]*ServeTargetConfig {
	if !c.ServeEnabled() || c.Serve == nil {
		return nil
	}
	result := make(map[string]*ServeTargetConfig)
	for name, cfg := range c.Serve.Target.Entries {
		if cfg == nil || cfg.Primary || !cfg.IsEnabled() {
			continue
		}
		result[name] = cfg
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// HasServeAliases reports whether this CRD has any enabled non-primary target entries.
func (c *CRDEntry) HasServeAliases() bool {
	return len(c.ServeAliases()) > 0
}

// HasServeTargetEntries reports whether any named target entries are declared
// under serve.target. Used by addTargetHooks and addTargetConstructors to skip
// CRDs that have no per-target operatorBox declarations.
func (c *CRDEntry) HasServeTargetEntries() bool {
	return c.ServeEnabled() && c.Serve.Target.Entries != nil
}

// AliasNames returns a sorted slice of alias names for this CRD, or nil if none.
func (c *CRDEntry) AliasNames() []string {
	aliases := c.ServeAliases()
	if len(aliases) == 0 {
		return nil
	}
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// HasServeResponseConfig reports whether the CRD has an serve response configuration.
func (c *CRDEntry) HasServeResponseConfig() bool {
	return c.Serve != nil && c.Serve.Config != nil && c.Serve.Config.Response != nil
}

// GetServeResponseConfig returns the serve response configuration, or nil if not set.
func (c *CRDEntry) GetServeResponseConfig() *ServeResponseConfig {
	if c.Serve == nil || c.Serve.Config == nil {
		return nil
	}
	return c.Serve.Config.Response
}

// ServeResponseUseDefault reports whether the full CR should be the starting point.
// Returns true when Default is nil (omitted) or explicitly true.
func (c *CRDEntry) ServeResponseUseDefault() bool {
	cfg := c.GetServeResponseConfig()
	if cfg == nil {
		return true
	}
	return cfg.UseDefault()
}

// ServeResponseHasPayload reports whether any payload expressions are declared.
func (c *CRDEntry) ServeResponseHasPayload() bool {
	cfg := c.GetServeResponseConfig()
	if cfg == nil {
		return false
	}
	return cfg.HasPayload()
}

// ServeResponseHasExclude reports whether an exclude expression is declared.
func (c *CRDEntry) ServeResponseHasExclude() bool {
	cfg := c.GetServeResponseConfig()
	if cfg == nil {
		return false
	}
	return cfg.HasExclude()
}

// ServeResponseExclude returns the exclude list, or nil if not set.
func (c *CRDEntry) ServeResponseExclude() []string {
	cfg := c.GetServeResponseConfig()
	if cfg == nil {
		return nil
	}
	return cfg.Exclude
}

// ServeResponsePayload returns the payload map, or nil if not set.
func (c *CRDEntry) ServeResponsePayload() map[string]string {
	cfg := c.GetServeResponseConfig()
	if cfg == nil {
		return nil
	}
	return cfg.Payload
}

// ServeResponseConfigFor returns the response configuration that applies to a
// caller using the given alias. Resolution:
//  1. Alias declares its own response config → alias config.
//  2. CRD-level serve.config.response.
//  3. nil (no config declared — caller receives full CR unchanged).
func (c *CRDEntry) ServeResponseConfigFor(alias string) *ServeResponseConfig {
	if alias != "" && c.Serve != nil {
		if entryCfg, ok := c.Serve.Target.Entries[alias]; ok {
			if cfg := entryCfg.ResponseConfig(); cfg != nil {
				return cfg
			}
		}
	}
	return c.GetServeResponseConfig()
}

// ServeTokensFor returns the token permissions map that applies to a caller
// using the given alias. Falls back to CRD-level tokens when the entry
// declares none or when alias is empty.
func (c *CRDEntry) ServeTokensFor(alias string) map[string]ServeTokenPermissions {
	if alias != "" && c.Serve != nil {
		if entryCfg, ok := c.Serve.Target.Entries[alias]; ok && entryCfg.HasTokenRestrictions() {
			return entryCfg.Tokens
		}
	}
	if c.Serve != nil {
		return c.Serve.Tokens
	}
	return nil
}

// TokenAllowedFor reports whether tokenName may perform op in namespace on this
// CRD using the token permissions resolved for the given alias.
// Alias-specific tokens take precedence over CRD-level tokens when declared.
// Delegates to ServeConfig.TokenAllowed with the resolved token set.
func (c *CRDEntry) TokenAllowedFor(
	alias, tokenName, op, namespace string,
	class ServeEndpointClass,
) (bool, ServeDenyReason) {
	cfg := &ServeConfig{Tokens: c.ServeTokensFor(alias)}
	return cfg.TokenAllowed(tokenName, op, namespace, class)
}

func (e EndpointsConfig) IsHealthEnabled() bool {
	if e.Enabled != nil && !*e.Enabled {
		return false
	}
	return e.Health == nil || *e.Health
}

func (e EndpointsConfig) IsInfoEnabled() bool {
	if e.Enabled != nil && !*e.Enabled {
		return false
	}
	return e.Info == nil || *e.Info
}
