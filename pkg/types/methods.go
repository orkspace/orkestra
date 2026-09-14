package types

import (
	"fmt"
	"strings"
	"time"

	"github.com/orkspace/orkestra/domain"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ── CRDEntry helpers ──────────────────────────────────────────────────────────

// Config returns the OperatorBox configuration for this CRD.
// Safe to call when OperatorBox is the zero value.
func (e CRDEntry) Config() OperatorBoxConfig {
	return e.OperatorBox
}

// ReconcilerConfig returns the reconciler configuration for this CRD.
func (e CRDEntry) ReconcilerConfig() *ReconcilerConfig {
	if e.OperatorBox.Reconciler == nil {
		return nil
	}
	return e.OperatorBox.Reconciler
}

// QueueConfig returns the Queue configuration for this CRD.
func (e CRDEntry) QueueConfig() *Queue {
	if e.OperatorBox.Reconciler == nil {
		return &Queue{}
	}
	return &e.OperatorBox.Reconciler.Queue
}

// PreReconcileCheck returns the gate config for this CRD.
// nil means no gate — the reconciler is always called.
func (e CRDEntry) PreReconcileCheck() *PreReconcileConfig {
	return e.OperatorBox.PreReconcile
}

// HasAnyEnqueueGate reports whether the CRD-level or any per-target operatorBox
// declares an enqueueGate. Used at startup to decide whether to register the
// informer enqueue filter — must register if ANY surface can gate enqueueing.
func (e CRDEntry) HasAnyEnqueueGate() bool {
	if rc := e.OperatorBox.PreReconcile; rc != nil && rc.HasEnqueueGate() {
		return true
	}
	if e.Serve != nil {
		for _, cfg := range e.Serve.Target.Entries {
			if cfg.OperatorBox != nil {
				if rc := cfg.OperatorBox.PreReconcile; rc != nil && rc.HasEnqueueGate() {
					return true
				}
			}
		}
	}
	return false
}

// HasAnyReconcileGate reports whether the CRD-level or any per-target operatorBox
// declares a reconcileGate. Used at dequeue time to decide whether to evaluate
// the gate before calling the reconciler.
func (e CRDEntry) HasAnyReconcileGate() bool {
	if rc := e.OperatorBox.PreReconcile; rc != nil && rc.HasReconcileGate() {
		return true
	}
	if e.Serve != nil {
		for _, cfg := range e.Serve.Target.Entries {
			if cfg.OperatorBox != nil {
				if rc := cfg.OperatorBox.PreReconcile; rc != nil && rc.HasReconcileGate() {
					return true
				}
			}
		}
	}
	return false
}

// IsBuiltInType reports whether this CRD represents a built‑in Kubernetes resource.
// Built‑ins rely on enrichment to populate group, version, plural, and scope.
func (c *CRDEntry) IsBuiltInType() bool {
	return c.IsBuiltIn
}

// SkipStatusSubresource reports whether this CRD belongs to a list to be ignored during status patches.
// This is applied mainly to builtins or if specifically required by the crd through crd.IgnoreStatusPatch
func (c *CRDEntry) SkipStatusSubresource() bool {
	return c.IgnoreStatusPatch
}

// ResolveForceConflict returns the effective force-conflict setting for a resource.
// ForceConflict defaults to true when unset.
func (c *CRDEntry) ResolveForceConflict() *bool {
	defaultForceConflict := true
	if c.ForceConflict == nil {
		return &defaultForceConflict
	}
	return c.ForceConflict
}

// SkipObservedGeneration reports whether this CRD should ignore the
// status.observedGeneration field during readiness checks.
//
// This is applied mainly to built‑in Kubernetes resources or CRDs that do not
// implement observedGeneration semantics. When true, Orkestra will NOT use
// generation-based readiness logic for this CRD.
func (c *CRDEntry) SkipObservedGeneration() bool {
	return c.IgnoreObservedGeneration
}

// ShouldEnrich returns true when the given enrichment target is enabled —
// either via EnrichAll: true or an explicit entry in Enrich.
// Condition gates (when:/or:) are not evaluated here — they are handled
// higher up by ActiveEnrichTargets before the CRDEntry reaches each enricher.
func (c *CRDEntry) ShouldEnrich(target string) bool {
	if c.EnrichAll {
		return true
	}
	for _, t := range c.Enrich {
		if t.Key == target {
			return true
		}
	}
	return false
}

// ActiveEnrichTargets returns the subset of Enrich entries whose when:/or:
// conditions pass for the given data map and evaluator. Unconditional entries
// (no when: or or:) always pass. Called from ReadChildren to pre-filter
// crd.Enrich before dispatching to individual enricher functions.
func (c *CRDEntry) ActiveEnrichTargets(data map[string]interface{}, eval TemplateEvaluator) []EnrichTarget {
	if c.EnrichAll {
		return c.Enrich
	}
	result := make([]EnrichTarget, 0, len(c.Enrich))
	for _, t := range c.Enrich {
		if len(t.When) == 0 && len(t.Or) == 0 {
			result = append(result, t)
			continue
		}
		if EvaluateConditions(data, t.When, t.Or, eval) {
			result = append(result, t)
		}
	}
	return result
}

// UnconditionalEnrichTargets returns entries with no when:/or: conditions.
// These run in phase 1 of ReadChildren so that .children.* is populated before
// conditional gates are evaluated.
func (c *CRDEntry) UnconditionalEnrichTargets() []EnrichTarget {
	result := make([]EnrichTarget, 0, len(c.Enrich))
	for _, t := range c.Enrich {
		if len(t.When) == 0 && len(t.Or) == 0 {
			result = append(result, t)
		}
	}
	return result
}

// ConditionalActiveEnrichTargets returns entries that HAVE conditions and whose
// conditions pass for the given data. Called in phase 2 of ReadChildren after
// children data is available — gates like {{ replicasReady .children.deployment }}
// can only be evaluated once the deployment has been read.
func (c *CRDEntry) ConditionalActiveEnrichTargets(data map[string]interface{}, eval TemplateEvaluator) []EnrichTarget {
	result := make([]EnrichTarget, 0)
	for _, t := range c.Enrich {
		if len(t.When) == 0 && len(t.Or) == 0 {
			continue // already ran in phase 1
		}
		if EvaluateConditions(data, t.When, t.Or, eval) {
			result = append(result, t)
		}
	}
	return result
}

// IsStatusless reports whether this CRD has no meaningful readiness semantics.
// These resources become "Ready" immediately upon creation.
func (c *CRDEntry) IsStatuslessType() bool {
	return c.IsStatusless
}

// GetRuntimeObjects returns the object and list constructors appropriate for the
// current mode (dynamic or typed). Used by the reconciler to instantiate new
// runtime objects for watches, lists, and reconciliation.
func (c *CRDEntry) GetRuntimeObjects() (runtime.Object, runtime.Object) {
	return c.DynamicModeObject(), c.ListDynamicModeObject()
}

// IsDynamic determines whether this CRD should operate in dynamic mode.
// Resolution order (first match wins):
//  1. mode: dynamic explicitly declared → true
//  2. mode: typed explicitly declared   → false
//  3. APITypes.Location is empty        → true  (no compiled types available)
//  4. APITypes.Location is set          → false (compiled types available)
func (c *CRDEntry) IsDynamic() bool {
	switch c.Mode {
	case CRDModeDynamic:
		return true
	case CRDModeTyped:
		return false
	}
	return c.APITypes.Location == ""
}

// WithHooksDecl returns true if the CRD has a hooks declaration (HookDeclaration).
// Used to determine whether to generate registry entries for the HookRegistry.
// Does not imply anything about Default: true/false — a typed CRD can have hooks
// even when Default: true (generic reconciler) or false (custom reconciler).
func (c *CRDEntry) WithHooksDecl() bool {
	r := c.OperatorBox.Reconciler
	return r != nil && r.Hooks != nil && r.Hooks.Location != ""
}

// RunHooksFirst reports whether the hook should run before declarative templates.
// Returns false by default — declared templates run first (the 90/10 hybrid pattern).
// Set reconciler.hooks.runHooksFirst: true in the Katalog to override.
func (c *CRDEntry) RunHooksFirst() bool {
	r := c.OperatorBox.Reconciler
	if r == nil || r.Hooks == nil {
		return false
	}
	return r.Hooks.RunHooksFirst
}

// WithConstructorDecl returns true if the CRD has a constructor declaration.
// Required when reconciler.default: false in the Katalog. The generated registry will
// emit a ReconcilerRegistry entry for this CRD.
func (c *CRDEntry) WithConstructorDecl() bool {
	r := c.OperatorBox.Reconciler
	return r != nil && r.ConstructorDecl != nil && r.ConstructorDecl.Location != ""
}

// WithHookManagedResources reports whether this CRD has hooks that declare
// managed resources for RBAC generation.
func (c *CRDEntry) WithHookManagedResources() bool {
	r := c.OperatorBox.Reconciler
	return c.WithHooksDecl() && r != nil && len(r.Hooks.ManagedResources) > 0
}

// WithConstructorManagedResources reports whether this CRD has a constructor
// that declares managed resources for RBAC generation.
func (c *CRDEntry) WithConstructorManagedResources() bool {
	r := c.OperatorBox.Reconciler
	return c.WithConstructorDecl() && r != nil && len(r.ConstructorDecl.ManagedResources) > 0
}

// WithAnyManagedResources reports whether hooks or constructor declare resources,
// including per-target operatorBox declarations.
func (c *CRDEntry) WithAnyManagedResources() bool {
	if c.WithHookManagedResources() || c.WithConstructorManagedResources() {
		return true
	}
	if c.Serve == nil || c.Serve.Target.Entries == nil {
		return false
	}
	for _, entry := range c.Serve.Target.Entries {
		if len(targetManagedResources(entry.OperatorBox)) > 0 {
			return true
		}
	}
	return false
}

// HookManagedResources returns the list of managed resources declared under
// the hooks block. Returns nil if hooks are not declared or no resources exist.
func (c *CRDEntry) HookManagedResources() []domain.ManagedResource {
	if !c.WithHooksDecl() {
		return nil
	}
	return c.OperatorBox.Reconciler.Hooks.ManagedResources
}

// ConstructorManagedResources returns the list of managed resources declared
// under the constructor block. Returns nil if constructor is not declared or
// no resources exist.
func (c *CRDEntry) ConstructorManagedResources() []domain.ManagedResource {
	if !c.WithConstructorDecl() {
		return nil
	}
	return c.OperatorBox.Reconciler.ConstructorDecl.ManagedResources
}

// AllManagedResources returns the combined list of managed resources from hooks,
// constructor, and per-target operatorBox declarations. Duplicates across targets
// are fine — startWatchInformers deduplicates by GVR via the covered set.
func (c *CRDEntry) AllManagedResources() []domain.ManagedResource {
	hooks := c.HookManagedResources()
	ctor := c.ConstructorManagedResources()
	out := make([]domain.ManagedResource, 0, len(hooks)+len(ctor))
	out = append(out, hooks...)
	out = append(out, ctor...)
	if c.Serve != nil {
		for _, entry := range c.Serve.Target.Entries {
			out = append(out, targetManagedResources(entry.OperatorBox)...)
		}
	}
	return out
}

// targetManagedResources extracts hook + constructor resources from a per-target
// operatorBox pointer. Returns nil when the box is nil or has no resources.
func targetManagedResources(box *OperatorBoxConfig) []domain.ManagedResource {
	if box.Empty() || box.Reconciler.Empty() {
		return nil
	}
	rec := box.Reconciler
	var out []domain.ManagedResource
	if rec.HasHooksDecl() {
		out = append(out, rec.Hooks.ManagedResources...)
	}
	if rec.HasConstructorDecl() {
		out = append(out, rec.ConstructorDecl.ManagedResources...)
	}
	return out
}

// WithWatchEntries reports whether this CRD or any per-target
// operatorBox.observe.watch declaration contains secondary watch entries.
func (c *CRDEntry) WithWatchEntries() bool {
	if c.OperatorBox.Observe != nil && len(c.OperatorBox.Observe.Watch) > 0 {
		return true
	}

	if c.Serve == nil {
		return false
	}

	for _, entry := range c.Serve.Target.Entries {
		if entry.OperatorBox != nil &&
			entry.OperatorBox.Observe != nil &&
			len(entry.OperatorBox.Observe.Watch) > 0 {
			return true
		}
	}

	return false
}

// WithSentinels reports whether this CRD declares any preReconcile sentinels.
func (c *CRDEntry) WithSentinels() bool {
	return len(c.OperatorBox.PreReconcile.DeclaredSentinels()) > 0
}

// WithQueueBehaviours reports whether this CRD declares any queue.behaviour.
func (c *CRDEntry) WithQueueBehaviours() bool {
	return c.QueueConfig().HasBehaviour()
}

// WatchEntries returns the combined secondary watch entries from the base
// operatorBox.observe.watch and all per-target operatorBox.observe.watch
// declarations.
//
// Duplicates across targets are deduplicated by startWatchInformers via the
// covered set.
func (c *CRDEntry) WatchEntries() []WatchEntry {
	var out []WatchEntry

	if c.OperatorBox.Observe != nil {
		out = append(out, c.OperatorBox.Observe.Watch...)
	}

	if c.Serve == nil {
		return out
	}

	for _, entry := range c.Serve.Target.Entries {
		if entry.OperatorBox != nil && entry.OperatorBox.Observe != nil {
			out = append(out, entry.OperatorBox.Observe.Watch...)
		}
	}

	return out
}

// EventEntries returns the combined secondary event entries from the base
// operatorBox.observe.events and all per-target operatorBox.observe.events
// declarations.
//
// Entries are keyed by their event declaration name. Per-target declarations
// with the same name override the base declaration.
func (c *CRDEntry) EventEntries() map[string]EventEntry {
	out := make(map[string]EventEntry)

	if c.OperatorBox.Observe != nil {
		for name, entry := range c.OperatorBox.Observe.Events {
			if entry == nil {
				continue
			}
			out[name] = *entry
		}
	}

	if c.Serve == nil {
		return out
	}

	for _, entry := range c.Serve.Target.Entries {
		if entry.OperatorBox == nil || entry.OperatorBox.Observe == nil {
			continue
		}

		for name, event := range entry.OperatorBox.Observe.Events {
			if event == nil {
				continue
			}
			out[name] = *event
		}
	}

	return out
}

// WithEventEntries reports whether this CRD or any per-target
// operatorBox.observe.events declaration contains event entries.
func (c *CRDEntry) WithEventEntries() bool {
	if c.OperatorBox.Observe != nil && len(c.OperatorBox.Observe.Events) > 0 {
		return true
	}

	if c.Serve == nil {
		return false
	}

	for _, entry := range c.Serve.Target.Entries {
		if entry.OperatorBox != nil &&
			entry.OperatorBox.Observe != nil &&
			len(entry.OperatorBox.Observe.Events) > 0 {
			return true
		}
	}

	return false
}

// HasTemplates reports whether this CRD declares any declarative hook templates.
// Used by `ork generate` to determine whether to emit generated runtime hooks.
func (c *CRDEntry) HasTemplates() bool {
	rc := c.OperatorBox
	return rc.OnCreate != nil || rc.OnReconcile != nil || rc.OnDelete != nil
}

// GVK returns the fully resolved GroupVersionKind for this CRD. Used for logging,
// routing, and dynamic client operations.
func (c *CRDEntry) GVK() schema.GroupVersionKind {
	return c.GroupVersionKind
}

// APIVersion returns the API version string (group/version) for this CRD.
func (c *CRDEntry) APIVersion() string {
	return c.GroupVersionKind.GroupVersion().String()
}

// Kind returns the kind string for this CRD.
func (c *CRDEntry) Kind() string {
	return c.GroupVersionKind.Kind
}

// GVKString returns the fully resolved GroupVersionKind for this CRD as a string.
func (c *CRDEntry) GVKString() string {
	return c.GroupVersionKind.String()
}

// GVR returns the fully resolved GroupVersionResource for this CRD. Used for
// dynamic client list/watch operations.
func (c *CRDEntry) GVR() schema.GroupVersionResource {
	return c.GroupVersionResource
}

// GVRString returns the fully resolved GroupVersionResource for this CRD as a string.
func (c *CRDEntry) GVRString() string {
	return c.GroupVersionResource.String()
}

// IsEnabled reports whether this CRD is enabled. Defaults to true when omitted.
func (c *CRDEntry) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// IsNamespaced reports whether this CRD is namespaced. Defaults to true unless
// explicitly overridden or determined by enrichment.
func (c *CRDEntry) IsNamespaced() bool {
	if c.Namespaced == nil {
		return true
	}
	return *c.Namespaced
}

// DefaultReconcile reports whether this CRD uses the default reconciler (GenericReconciler).
// True when reconciler: is absent or reconciler.default: is omitted or true.
func (c *CRDEntry) DefaultReconcile() bool {
	r := c.OperatorBox.Reconciler
	if r == nil || r.Default == nil {
		return true
	}
	return *r.Default
}

// CustomHooksEnabled reports whether the reconcile behaviour uses custom hooks.
// Defaults to false when omitted.
func (c *CRDEntry) CustomHooksEnabled() bool {
	r := c.OperatorBox.Reconciler
	return r != nil && r.Hooks != nil
}

// ConstructorEnabled reports whether the reconcile behaviour uses a constructor.
// Defaults to false when omitted.
func (c *CRDEntry) ConstructorEnabled() bool {
	r := c.OperatorBox.Reconciler
	return r != nil && r.ConstructorDecl != nil
}

// IsHealthEnabled reports whether the /health endpoint is enabled for this CRD.
// Defaults to true when omitted.
func (c *CRDEntry) IsHealthEnabled() bool {
	if c.Endpoints.Health == nil {
		return true
	}
	return *c.Endpoints.Health
}

// IsInfoEnabled reports whether the /info endpoint is enabled for this CRD.
// Defaults to true when omitted.
func (c *CRDEntry) IsInfoEnabled() bool {
	if c.Endpoints.Info == nil {
		return true
	}
	return *c.Endpoints.Info
}

// CrossAccessEnabled reports whether cross: reads are permitted for this CRD.
// Defaults to true when omitted.
func (c *CRDEntry) CrossAccessEnabled() bool {
	return c.CrossAccess == nil || *c.CrossAccess
}

// CrossAccessEnabled reports whether cross: reads are permitted for this CRD.
// Defaults to true when omitted.
func (c *CRDEntry) HasCrossDecl() bool {
	if c == nil {
		return false
	}
	return !c.OperatorBox.Empty() && len(c.OperatorBox.Cross) > 0
}

// HasHooks reports whether this CRD has hooks wired — either a YAML-declared
// hooks block or a Go-registered HookFactory.
func (c *CRDEntry) HasHooks() bool {
	r := c.OperatorBox.Reconciler
	return (r != nil && r.Hooks != nil) || c.OperatorBox.HookFactory != nil
}

// HasConstructor reports whether a Go-registered constructor is wired for this CRD.
func (c *CRDEntry) HasConstructor() bool {
	return c.OperatorBox.Constructor != nil
}

// HooksArgs returns the args declared under reconciler.hooks.args in the Katalog.
// Returns nil when no hooks declaration or no args are present.
func (c *CRDEntry) HooksArgs() map[string]interface{} {
	if r := c.OperatorBox.Reconciler; r != nil && r.Hooks != nil {
		return r.Hooks.Args
	}
	return nil
}

// HooksArgs returns the args map from this reconciler config's hooks declaration.
// Returns nil when no hooks are declared or no args are set.
func (r *ReconcilerConfig) HooksArgs() map[string]interface{} {
	if r == nil || r.Hooks == nil {
		return nil
	}
	return r.Hooks.Args
}

// HooksExternal returns the external call specs declared under reconciler.hooks.external.
// Returns nil when no hooks declaration or no external calls are declared.
func (c *CRDEntry) HooksExternal() []ExternalCallSpec {
	if r := c.OperatorBox.Reconciler; r != nil && r.Hooks != nil {
		return r.Hooks.External
	}
	return nil
}

// HasHooksExternal reports whether the CRD declares any external calls under reconciler.hooks.external.
func (c *CRDEntry) HasHooksExternal() bool {
	return len(c.HooksExternal()) > 0
}

// ConstructorArgs returns the args declared under reconciler.constructor.args in the Katalog.
// Returns nil when no constructor declaration or no args are present.
func (c *CRDEntry) ConstructorArgs() map[string]interface{} {
	if r := c.OperatorBox.Reconciler; r != nil && r.ConstructorDecl != nil {
		return r.ConstructorDecl.Args
	}
	return nil
}

// TargetConstructorArgs returns the constructor args declared under
// serve.target.entries[targetName].operatorBox.reconciler.constructor.args.
// Returns nil when the target entry, its operatorBox, or its constructor declaration is absent.
func (c *CRDEntry) TargetConstructorArgs(targetName string) map[string]interface{} {
	if c.Serve == nil || c.Serve.Target.Entries == nil {
		return nil
	}
	entry, ok := c.Serve.Target.Entries[targetName]
	if !ok || entry.OperatorBox == nil || entry.OperatorBox.Reconciler == nil {
		return nil
	}
	if entry.OperatorBox.Reconciler.ConstructorDecl == nil {
		return nil
	}
	return entry.OperatorBox.Reconciler.ConstructorDecl.Args
}

// HasTargetConstructorFactories reports whether any serve target declares a
// custom constructor (reconciler.default: false with a constructor declaration).
func (c *CRDEntry) HasTargetConstructorFactories() bool {
	if c.Serve == nil || c.Serve.Target.Entries == nil {
		return false
	}
	for _, entry := range c.Serve.Target.Entries {
		box := entry.OperatorBox
		if box.Empty() {
			continue
		}
		rec := box.Reconciler
		if rec.Empty() || rec.IsDefault() || !rec.HasConstructorDecl() {
			continue
		}
		return true
	}
	return false
}

// IsEnabledAllEndpoints reports whether the all endpoints are disabled for this CRD.
// Defaults to false when omitted.
func (c *CRDEntry) IsEnabledAllEndpoints() bool {
	if c.Endpoints.Enabled == nil {
		return true
	}
	return *c.Endpoints.Enabled
}

// GetDependencies returns the dependency names for this CRD in sorted order.
func (c *CRDEntry) GetDependencies() []string {
	return c.DependsOn.Names()
}

// Returns true when either validation or mutation rules are declared.
// Used to decide whether to create the endpoints and/or populate the admission block in the health response.
// Even when ENABLE_ADMISSION_WEBHOOK=true
func (c *CRDEntry) HasValidationOrMutationRules() bool {
	return c.HasValidationRules() || c.HasMutationRules()
}

// Separate helpers for hasMutationRules and hasValidationRules
func (c *CRDEntry) HasMutationRules() bool {
	if c.Mutation == nil {
		return false
	}
	return len(c.Mutation.Rules) > 0
}

// ShouldMutateFirst reports whether this CRD prefers mutation first or not
//
// Default is true
func (c *CRDEntry) ShouldMutateFirst() bool {
	if c.Mutation == nil {
		return true
	}
	return c.Mutation.MutateFirst
}

// HasValidationRules reports whether this CRD has any validation behavior configured
func (c *CRDEntry) HasValidationRules() bool {
	if c.Validation == nil {
		return false
	}
	return len(c.Validation.Rules) > 0
}

// HasProviders reports whether this CRD declares any provider blocks.
func (c *CRDEntry) HasProviders() bool {
	return len(c.OperatorBox.ProviderBlocks) > 0
}

// AutoscaleEnabled reports whether this CRD declares the autoscale block
func (c *CRDEntry) AutoscaleEnabled() bool {
	return c.OperatorBox.Autoscale != nil
}

// HasRollbackRules reports whether this CRD has any rollback behavior configured —
// either via an explicit rollback: block or the rollBackOnError: true shorthand.
func (c *CRDEntry) HasRollbackRules() bool {
	return c.OperatorBox.Rollback != nil || c.OperatorBox.RollBackOnError
}

// HasCRDFile reports whether this CRDEntry declares a CRD file
// to be auto-applied before the operator starts.
func (c *CRDEntry) HasCRDFile() bool {
	return c != nil && c.CRDFile != ""
}

// HasCRFiles reports whether this CRDEntry declares CR YAML files
// to be applied before the runtime starts.
func (c *CRDEntry) HasCRFiles() bool {
	return c != nil && len(c.CRFiles) > 0
}

// HasSetup reports whether this CRDEntry declares any setup work
// to be done before Orkestra starts.
func (c *CRDEntry) HasSetup() bool {
	if c == nil || c.Setup == nil {
		return false
	}
	return len(c.Setup.Apply) > 0 || len(c.Setup.Helm) > 0 || len(c.Setup.Wait) > 0
}

// NotificationEnabled reports whether this CRD declares the notification block
// Enabled by default
func (c *CRDEntry) IsNotificationEnabled() bool {
	if c.NotificationEnabled == nil {
		return true
	}
	return *c.NotificationEnabled
}

// ValidateMetricField returns an error if the field is not a known autoscale metric.
func (c *CRDEntry) ValidateMetricField(field string) error {
	known := map[string]struct{}{
		"metrics.workersBusyPercent":     {},
		"metrics.workersIdlePercent":     {},
		"metrics.queueDepth":             {},
		"metrics.reconcileDurationP95Ms": {},
		"metrics.errorRatePercent":       {},
	}

	if _, ok := known[field]; !ok {
		return fmt.Errorf(
			"unknown autoscale metric field %q — valid fields: %s",
			field,
			strings.Join([]string{
				"metrics.workersBusyPercent",
				"metrics.workersIdlePercent",
				"metrics.queueDepth",
				"metrics.reconcileDurationP95Ms",
				"metrics.errorRatePercent",
			}, ", "),
		)
	}

	return nil
}

// HasAutoscaleProfile reports whether this crd defined autoscale profile
func (c *CRDEntry) HasAutoscaleProfile() bool {
	return c.OperatorBox.Autoscale != nil && c.OperatorBox.Autoscale.Profile != ""
}

// AutoScaleProfile returns the string value of the autoscale profile
func (c *CRDEntry) AutoScaleProfile() string {
	return c.OperatorBox.Autoscale.Profile
}

// IsConversionParticipant reports whether this CRD is a participant-only member
// of a conversion pair. Participants hold no conversion paths — those live on
// the CRD that owns the /convert logic. Used to skip path registration so a
// participant entry can never clobber the real rules during Katalog load.
func (c *CRDEntry) IsConversionParticipant() bool {
	if c.Conversion == nil {
		return false
	}
	return c.Conversion.Participant
}

// UpdateCRDCaBundle reports whether this CRD declares an updateCRD field
// Used to update the crd when certificate is autogenerted by orkestra
func (c *CRDEntry) UpdateCRDCaBundle() bool {
	if c.Conversion == nil {
		return false
	}
	return c.Conversion.UpdateCRD
}

// InvolvedInConversion reports whether this CRD is involved in version conversion.
// A CRD is involved when it either declares conversion paths (the CRD that hosts
// the /convert logic) or explicitly opts in with participant: true (the stable/
// storage-version CRD on the other side of the pair).
func (c *CRDEntry) InvolvedInConversion() bool {
	if c.Conversion == nil {
		return false
	}
	return len(c.Conversion.Paths) > 0 || c.Conversion.Participant
}

// HasNamespaceRules reports whether this CRD declares any namespace rules.
func (c *CRDEntry) HasNamespaceRules() bool {
	return len(c.AllowedNamespaces) > 0 || len(c.RestrictedNamespaces) > 0
}

// HasOnCreate reports whether this CRD declares any onCreate hooks.
func (c *CRDEntry) HasOnCreate() bool {
	return c.OperatorBox.OnCreate != nil
}

// HasOnReconcile reports whether this CRD declares any onReconcile hooks.
func (c *CRDEntry) HasOnReconcile() bool {
	return c.OperatorBox.OnReconcile != nil
}

// HasOnDelete reports whether this CRD declares any onDelete hooks.
func (c *CRDEntry) HasOnDelete() bool {
	return c.OperatorBox.OnDelete != nil
}

// HasStatusFields reports whether this CRD declares any status fields.
func (c *CRDEntry) HasStatusFields() bool {
	return c.OperatorBox.Status != nil && c.OperatorBox.Status.HasFields()
}

// AllRestrictedNamespaces returns a list of restricted namespaces for this crd
func (c *CRDEntry) AllRestrictedNamespaces() RestrictedNamespaces {
	return c.RestrictedNamespaces
}

// AllAllowedNamespaces returns a list of allowed namespaces for this crd
func (c *CRDEntry) AllAllowedNamespaces() AllowedNamespaces {
	return c.AllowedNamespaces
}

// IsNamespaceRestricted returns true if either allowedNamespaces or restrictedNamespaces is not empty
func (c *CRDEntry) IsNamespaceRestricted() bool {
	return c.HasAllowedNamespaces() || c.HasRestrictedNamespaces()
}

// AllowedNamespacesOnly reports if only allowedNamespaces is defined for this crd.
func (c *CRDEntry) AllowedNamespacesOnly() bool {
	return len(c.AllowedNamespaces) > 0 && len(c.RestrictedNamespaces) == 0
}

// RestrictedNamespacesOnly reports if only restrictedNamespaces is defined for this crd.
func (c *CRDEntry) RestrictedNamespacesOnly() bool {
	return len(c.RestrictedNamespaces) > 0 && len(c.AllowedNamespaces) == 0
}

// HasAllowedNamespaces reports if allowedNamespaces is defined for this crd.
func (c *CRDEntry) HasAllowedNamespaces() bool {
	return len(c.AllowedNamespaces) > 0
}

// HasRestrictedNamespaces reports if restrictedNamespaces is defined for this crd.
func (c *CRDEntry) HasRestrictedNamespaces() bool {
	return len(c.RestrictedNamespaces) > 0
}

// IsNamespaceAuthorized returns true if the namespace is allowed for this CRD.
//
// Authorization rules:
//   - If only allowedNamespaces is set: namespace must be in the list
//   - If only restrictedNamespaces is set: namespace must NOT be in the list
//   - If both are set: namespace must be in allowedNamespaces AND NOT in restrictedNamespaces
//   - If neither is set: all namespaces are allowed
func (c *CRDEntry) IsNamespaceAuthorized(namespace string) bool {
	// If both are empty, all namespaces are allowed
	if !c.HasAllowedNamespaces() && !c.HasRestrictedNamespaces() {
		return true
	}

	// Check allowedNamespaces (if set)
	if c.HasAllowedNamespaces() {
		allowed := false
		for _, ns := range c.AllowedNamespaces {
			if ns == namespace {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	// Check restrictedNamespaces (if set)
	if c.HasRestrictedNamespaces() {
		for _, ns := range c.RestrictedNamespaces {
			if ns == namespace {
				return false // namespace is restricted
			}
		}
	}

	return true
}

// IsValidServiceType reports whether the provided service type is valid.
// Accepted values (case‑insensitive):
//   - ClusterIP
//   - NodePort
//   - LoadBalancer
//
// SetWorkers resolves the worker count for this CRD.
// Reads from operatorBox.reconciler.workers; falls back to the global default.
func (c *CRDEntry) SetWorkers(def int) int {
	if c.OperatorBox.Reconciler != nil && c.OperatorBox.Reconciler.Workers != 0 {
		return c.OperatorBox.Reconciler.Workers
	}
	return def
}

// SetResync resolves the resync period for this CRD.
// Reads from operatorBox.reconciler.resync; falls back to the global default.
func (c *CRDEntry) SetResync(def time.Duration) time.Duration {
	if c.OperatorBox.Reconciler != nil && c.OperatorBox.Reconciler.Resync.Duration != 0 {
		return c.OperatorBox.Reconciler.Resync.Duration
	}
	return def
}

// SetQueueDepth resolves the queue depth for this CRD.
// Reads from operatorBox.reconciler.queue.maxDepth; falls back to the global default.
func (c *CRDEntry) SetQueueDepth(def int) int {
	if c.OperatorBox.Reconciler != nil && c.OperatorBox.Reconciler.Queue.MaxDepth != 0 {
		return c.OperatorBox.Reconciler.Queue.MaxDepth
	}
	return def
}

// SetFailureThreshold resolves the queue failure threshold for this CRD.
// Reads from operatorBox.reconciler.queue.failureThreshold; falls back to the global default.
func (c *CRDEntry) SetFailureThreshold(def int) int {
	if c.OperatorBox.Reconciler != nil && c.OperatorBox.Reconciler.Queue.FailureThreshold != 0 {
		return c.OperatorBox.Reconciler.Queue.FailureThreshold
	}
	return def
}

// SharedQueue reports whether this CRD uses the shared default workqueue.
func (c *CRDEntry) SharedQueue() bool {
	if c.OperatorBox.Reconciler == nil || c.OperatorBox.Reconciler.Queue.Shared == nil {
		return false
	}
	return *c.OperatorBox.Reconciler.Queue.Shared
}

func IsValidServiceType(t string) bool {
	switch strings.ToLower(t) {
	case "", "clusterip", "nodeport", "loadbalancer":
		return true
	default:
		return false
	}
}

// HasUserLabels reports whether the CRD entry declares any user-defined labels.
func (e CRDEntry) HasUserLabels() bool {
	return len(e.Labels) > 0
}

// IsValidProtocol reports whether the provided protocol is valid.
// Accepted values (case‑insensitive):
//   - TCP
//   - UDP
//   - SCTP
func IsValidProtocol(p string) bool {
	switch strings.ToUpper(p) {
	case "", "TCP", "UDP", "SCTP":
		return true
	default:
		return false
	}
}
