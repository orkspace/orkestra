// pkg/types/types_operatorbox.go
package types

import (
	"slices"
	"strings"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/runtime/sentinel"
)

// ── FailPolicy ────────────────────────────────────────────────────────────────────

// FailPolicy controls what a gate does when it cannot evaluate its conditions —
// for example when an external: call fails or times out.
type FailPolicy string

const (
	// FailPolicyOpen passes the gate on evaluation failure.
	// The object is enqueued / reconciled as if the gate was not declared.
	// This is the default when failPolicy is omitted.
	FailPolicyOpen FailPolicy = "open"

	// FailPolicyClosed holds the gate on evaluation failure.
	// The object is dropped from the queue / held back from the reconciler.
	// Use on reconcileGate when unknown state is worse than a missed reconcile.
	FailPolicyClosed FailPolicy = "closed"
)

// String() stringifies a policy
func (f FailPolicy) String() string {
	return string(f)
}

// ValidFailPolicies returns all known failPolicy values in declaration order.
func ValidFailPolicies() []string {
	return []string{string(FailPolicyOpen), string(FailPolicyClosed)}
}

// IsValidFailPolicy reports whether s is a known FailPolicy value.
func IsValidFailPolicy(s string) bool {
	switch FailPolicy(s) {
	case FailPolicyOpen, FailPolicyClosed:
		return true
	}
	return false
}

// FailPolicyJoined returns a comma-separated list of valid failPolicy values for error messages.
func FailPolicyJoined() string { return strings.Join(ValidFailPolicies(), ", ") }

// ── PreReconcileConfig ────────────────────────────────────────────────────────────

// GateConditions declares when/or conditions and optional external calls
// shared by both preReconcile gates.
type GateConditions struct {
	// External declares HTTP or gRPC calls made before conditions are evaluated.
	// Results are injected into the resolver under .external.<name>.* and are
	// available in when:/or: field expressions.
	External []ExternalCallSpec `yaml:"external,omitempty" json:"external,omitempty"`

	// Sentinels declares the event-time values this operator uses in gate conditions.
	// Declared here as a shorthand instead of when/or conditions. It uses the same
	// semantics as 'or' conditions since first match passes. Must be a valid subset of
	// preReconcile.sentinels. Checked first before the conditions. Use when to require
	// more than one sentinel
	Sentinels []string `yaml:"sentinels,omitempty" json:"sentinels,omitempty"`

	// EventAware preserves individual events through queue deduplication for this gate.
	// When true, each event that reaches the reconcile gate is evaluated as a distinct
	// work item. When false, normal queue coalescing applies and gate evaluation is
	// state-oriented: multiple events for the same resource may collapse into one
	// reconciliation.
	EventAware bool `yaml:"eventAware,omitempty" json:"eventAware,omitempty"`

	// When declares AND conditions. All must be true for the gate to pass.
	When []Condition `yaml:"when,omitempty" json:"when,omitempty"`

	// Or declares OR conditions. At least one must be true.
	// When both When and Or are declared, both must pass.
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// FailPolicy controls what the gate does when it cannot evaluate its conditions —
	// for example when an external: call fails or times out.
	// Defaults to open when omitted.
	FailPolicy FailPolicy `yaml:"failPolicy,omitempty" json:"failPolicy,omitempty"`
}

// HasConditions reports whether any when/or conditions are declared.
func (g *GateConditions) HasConditions() bool {
	return g != nil && (len(g.When) > 0 || len(g.Or) > 0)
}

// HasExternal reports whether any external calls are declared.
func (g *GateConditions) HasExternal() bool {
	return g != nil && len(g.External) > 0
}

// HasGate reports whether the gate has anything to evaluate — conditions or external calls.
func (g *GateConditions) HasGate() bool {
	return g != nil && (len(g.When) > 0 ||
		len(g.Or) > 0 ||
		len(g.External) > 0 ||
		len(g.Sentinels) > 0)
}

// HasSentinels reports whether the gate has declared sentinels
func (g *GateConditions) HasSentinels() bool {
	return g != nil && len(g.Sentinels) > 0
}

// IsEventAware reports whether this gate requires per-event evaluation.
func (g *GateConditions) IsEventAware() bool {
	return g != nil && g.EventAware
}

// SentinelContains reports true if s is declared in g.Sentinels.
func (g *GateConditions) SentinelContains(s string) bool {
	if !g.HasSentinels() {
		return false
	}
	for _, name := range g.Sentinels {
		if name == s {
			return true
		}
	}
	return false
}

// DeclaredSentinels returns the sentinel names declared under a gate condition
// Returns nil when no sentinels are declared. Safe on nil receiver.
func (g *GateConditions) DeclaredSentinels() []string {
	if g == nil {
		return nil
	}
	return g.Sentinels
}

// SentinelsAllowed returns true when any declared sentinel key is present and
// set to "true" in declared. OR semantics — first match wins.
func (g *GateConditions) SentinelsAllowed(declared map[string]string) bool {
	if !g.HasSentinels() {
		return false
	}

	for k, v := range declared {
		if g.SentinelContains(k) {
			if v == "true" {
				return true
			}
		}
	}

	return false
}

// WhenConditions returns the AND conditions, safe on nil receiver.
func (g *GateConditions) WhenConditions() []Condition {
	if g == nil {
		return nil
	}
	return g.When
}

// OrConditions returns the OR conditions, safe on nil receiver.
func (g *GateConditions) OrConditions() []Condition {
	if g == nil {
		return nil
	}
	return g.Or
}

// ExternalCalls returns the external calls declared on this gate, or nil when
// the gate is nil or has no external declarations.
func (g *GateConditions) ExternalCalls() []ExternalCallSpec {
	if g == nil {
		return nil
	}
	return g.External
}

// ── PreReconcileConfig ────────────────────────────────────────────────────────────

// PreReconcileConfig controls whether an event enters the queue and whether a dequeued
// item reaches the reconciler. Two gates: enqueueGate fires at the informer before the
// object enters the queue; reconcileGate fires at the kordinator after dequeue. Events
// that fail either gate are silently dropped — the reconciler is never invoked.
type PreReconcileConfig struct {
	// External declares HTTP or gRPC calls made once before either gate is evaluated.
	// Results are injected into the resolver under .external.<name>.* and are
	// available in both enqueueGate and reconcileGate field expressions.
	External []ExternalCallSpec `yaml:"external,omitempty" json:"external,omitempty"`

	// Sentinels declares the event-time values this operator uses in gate conditions.
	// Each sentinel is computed by the informer's UpdateFunc against oldObj/newObj
	// and carried through the queue entry so both enqueueGate and reconcileGate
	// can reference it. The informer computes only declared sentinels.
	//
	// Valid values: SentinelGenerationChanged, SentinelLabelsChanged, SentinelAnnotationsChanged,
	// SentinelDeletionStarted, SentinelFinalizersChanged and 9more.
	//
	// ork validate fails if a sentinel is used in a gate template but not declared
	// here, or if a sentinel is used outside the preReconcile context.
	Sentinels []string `yaml:"sentinels,omitempty" json:"sentinels,omitempty"`

	// EnqueueGate declares informer-level gate conditions evaluated in handleEvent
	// before the object enters the work queue. When the gate fires the object is
	// silently dropped — it never reaches the kordinator or reconciler.
	// No health state change; kordinator is never involved.
	EnqueueGate *GateConditions `yaml:"enqueueGate,omitempty" json:"enqueueGate,omitempty"`

	// ReconcileGate declares kordinator-level gate conditions evaluated after
	// dequeue, before the reconciler is called. When conditions are not met
	// the item is discarded and CRD health is set to gated.
	ReconcileGate *GateConditions `yaml:"reconcileGate,omitempty" json:"reconcileGate,omitempty"`
}

// HasSentinels returns true if sentinels are declared
func (r *PreReconcileConfig) HasSentinels() bool {
	if r == nil {
		return false
	}
	return r.Sentinels != nil
}

// DeclaredSentinels returns the sentinel names declared under preReconcile.sentinels.
// Returns nil when no sentinels are declared. Safe on nil receiver.
func (r *PreReconcileConfig) DeclaredSentinels() []string {
	if r == nil {
		return nil
	}
	return r.Sentinels
}

// InvalidSentinels returns any sentinel values that are not valid Sentinel constants.
// Returns nil when all values are valid. Safe on nil receiver.
func (r *PreReconcileConfig) InvalidSentinels() []string {
	if r == nil {
		return nil
	}
	_, invalid := sentinel.IsAllValid(r.Sentinels)
	return invalid
}

// InvalidGateSentinels returns any sentinel values that are not subset of preReconcile.sentinels.
// It does not check for validity of the sentinel. That is already done by the upstream preReconcile InvalidSentinels()
// Returns nil, false when all values are valid. Safe on nil receiver.
func (r *PreReconcileConfig) InvalidGateSentinels(g *GateConditions) ([]string, bool) {
	if r == nil && g == nil {
		return nil, false
	}

	invalid := []string{}
	for _, sent := range g.Sentinels {
		if !slices.Contains(r.Sentinels, sent) {
			invalid = append(invalid, sent)
		}
	}
	if len(invalid) > 0 {
		return invalid, true
	}

	return nil, false
}

// HasPreReconcileConditions reports whether reconcileGate has any when/or conditions declared.
func (r *PreReconcileConfig) HasPreReconcileConditions() bool {
	return r != nil && (r.ReconcileGate.HasConditions() || r.EnqueueGate.HasConditions())
}

// HasEnqueueGate reports whether the enqueue gate has anything to evaluate.
func (r *PreReconcileConfig) HasEnqueueGate() bool {
	return r != nil && (r.EnqueueGate.HasGate() || len(r.External) > 0)
}

// HasReconcileGate reports whether the reconcile gate has anything to evaluate.
func (r *PreReconcileConfig) HasReconcileGate() bool {
	return r != nil && (r.ReconcileGate.HasGate() || len(r.External) > 0 || r.ReconcileGate.HasSentinels() || r.ReconcileGate.IsEventAware())
}

// IsEventAware reports whether this reconcile gate requires per-event evaluation.
func (r *PreReconcileConfig) IsEventAware() bool {
	return r != nil && r.HasReconcileGate() && r.ReconcileGate.IsEventAware()
}

// HasPreReconcileExternal reports whether preReconcile-level external calls are declared.
func (r *PreReconcileConfig) HasPreReconcileExternal() bool {
	return r != nil && len(r.External) > 0
}

// HasEnqueueGateSentinel reports whether the enqueueGate declares sentinels.
func (r *PreReconcileConfig) HasEnqueueGateSentinel() bool {
	return r != nil && r.EnqueueGate != nil && len(r.EnqueueGate.Sentinels) > 0
}

// HasReconcileGateSentinel reports whether the reconcileGate declares sentinels.
func (r *PreReconcileConfig) HasReconcileGateSentinel() bool {
	return r != nil && r.ReconcileGate != nil && len(r.ReconcileGate.Sentinels) > 0
}

// HasEnqueueGateExternal reports whether the enqueueGate declares external calls.
func (r *PreReconcileConfig) HasEnqueueGateExternal() bool {
	return r != nil && r.EnqueueGate != nil && len(r.EnqueueGate.External) > 0
}

// HasReconcileGateExternal reports whether the reconcileGate declares external calls.
func (r *PreReconcileConfig) HasReconcileGateExternal() bool {
	return r != nil && r.ReconcileGate != nil && len(r.ReconcileGate.External) > 0
}

// GateExternalCalls returns all external calls declared across enqueueGate and
// reconcileGate. Returns nil when the receiver is nil or neither gate has calls.
func (r *PreReconcileConfig) GateExternalCalls() [][]ExternalCallSpec {
	if r == nil {
		return nil
	}
	var phases [][]ExternalCallSpec
	if calls := r.EnqueueGate.ExternalCalls(); len(calls) > 0 {
		phases = append(phases, calls)
	}
	if calls := r.ReconcileGate.ExternalCalls(); len(calls) > 0 {
		phases = append(phases, calls)
	}
	return phases
}

// WhenConditions returns the reconcileGate AND conditions, safe on nil receiver.
func (r *PreReconcileConfig) WhenConditions() []Condition {
	if r == nil {
		return nil
	}
	return r.ReconcileGate.WhenConditions()
}

// OrConditions returns the reconcileGate OR conditions, safe on nil receiver.
func (r *PreReconcileConfig) OrConditions() []Condition {
	if r == nil {
		return nil
	}
	return r.ReconcileGate.OrConditions()
}

// ── OperatorBoxConfig ──────────────────────────────────────────────────────────

// ReconcilerConfig declares which reconciler implementation runs and how it is tuned.
type ReconcilerConfig struct {
	// Default: true → generic.Reconciler (default when omitted).
	// false → custom reconciler; Constructor must be declared.
	Default *bool `yaml:"default,omitempty" json:"default,omitempty" validate:"omitempty"`

	// Hooks declares a Go hook function. Signature: func() domain.AnyReconcileHooks.
	// Mutually exclusive with OnCreate/OnReconcile/OnDelete templates.
	Hooks *HookDeclaration `yaml:"hooks,omitempty" json:"hooks,omitempty" validate:"omitempty"`

	// ConstructorDecl declares a custom reconciler constructor. Required when Default: false.
	// Signature: NewReconcilerFunc.
	ConstructorDecl *ConstructorDeclaration `yaml:"constructor,omitempty" json:"constructor,omitempty" validate:"omitempty"`

	// Profile is a named tuning preset. Built-ins: high-throughput, conservative, development.
	// Inline Workers/Resync/Queue override the profile.
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`

	// Workers is the number of concurrent reconcile goroutines. 0 → DEFAULT_WORKERS.
	Workers int `yaml:"workers,omitempty" json:"workers,omitempty" validate:"omitempty,gte=1,lte=50"`

	// Resync is the full re-list interval for the informer cache. 0 → DEFAULT_RESYNC.
	Resync Duration `yaml:"resync,omitempty" json:"resync,omitempty"`

	Queue   Queue          `yaml:"queue,omitempty" json:"queue,omitempty"`
	Requeue *RequeueConfig `yaml:"requeue,omitempty"`

	// Include is a path to a YAML file whose reconciler: block is merged under this config.
	// Inline fields take precedence. Cleared after expansion.
	Include string `yaml:"include,omitempty" json:"include,omitempty"`
}

// RequeueConfig declares per-object requeue behavior after a successful reconcile.
// Errors are retried via queue.retryBackoff, not requeue.
type RequeueConfig struct {
	// After is a template expression resolving to a Go duration string (e.g. "{{ .spec.checkInterval }}").
	// Empty means no requeue — wait for the next informer event.
	After string `yaml:"after,omitempty"`

	When []Condition `yaml:"when,omitempty"`
	Or   []Condition `yaml:"or,omitempty"`
}

// IsDefault returns true when the reconciler should use the generic.Reconciler.
// When Default is nil (not declared), it defaults to true.
func (r *ReconcilerConfig) IsDefault() bool {
	if r == nil {
		return true
	}
	if r.Default == nil {
		return true
	}
	return *r.Default
}

// HasHooksDecl reports whether a hook declaration exists.
func (r *ReconcilerConfig) HasHooksDecl() bool {
	if r == nil {
		return false
	}
	return r.Hooks != nil
}

// HasRetryBackoff reports whether a retryBackoff is declared on this reconciler's queue.
func (r *ReconcilerConfig) HasRetryBackoff() bool {
	return r != nil && r.Queue.HasRetryBackoff()
}

// HasConstructorDecl reports whether a constructor declaration exists.
func (r *ReconcilerConfig) HasConstructorDecl() bool {
	if r == nil {
		return false
	}
	return r.ConstructorDecl != nil
}

// HasRequeueDecl reports whether a requeue configuration exists.
func (r *ReconcilerConfig) HasRequeueDecl() bool {
	if r == nil {
		return false
	}
	return r.Requeue != nil
}

// IsRequeueEmpty reports whether the requeue configuration is effectively empty.
func (r *ReconcilerConfig) IsRequeueEmpty() bool {
	if r == nil || r.Requeue == nil {
		return true
	}
	rc := r.Requeue
	return rc.After == "" && len(rc.When) == 0 && len(rc.Or) == 0
}

// Empty reports whether this requeue configuration has no effective behavior.
func (rc *RequeueConfig) Empty() bool {
	if rc == nil {
		return true
	}
	return rc.After == "" && len(rc.When) == 0 && len(rc.Or) == 0
}

func (p *PreReconcileConfig) Empty() bool { return p == nil }
func (r *ReconcilerConfig) Empty() bool   { return r == nil }

// OperatorBoxConfig is the unit of reconciliation in Orkestra.
//
// CRDs in, operators out.
//
// Each CRD entry in a Katalog gets its own operatorBox:
// an isolated informer, queue, worker pool, reconciler, health state, and metrics —
// independent of every other CRD in the same process.
//
// The five sections follow the execution flow of one reconcile cycle:
//
//	observe       — data: cross-CRD reads and secondary watches available at reconcile time
//	preReconcile  — gate: should this event enter the queue or reach the reconciler?
//	runtime       — policy: autoscale, rollback, finalizers, and deletion guards
//	reconcile     — work: implementation identity, lifecycle templates, execution tuning
//	emit          — output: status fields and events written after every reconcile
type OperatorBoxConfig struct {
	// Observe makes external state available inside the reconcile context. cross: reads
	// another CRD's CR via the informer cache (same binary) or HTTP (cross-binary/cluster).
	// watch: registers secondary resource watches that act as additional reconcile triggers.
	Observe *Observe `yaml:"observe,omitempty" json:"observe,omitempty"`

	// PreReconcile controls whether an event enters the queue (enqueueGate) and whether
	// a dequeued item reaches the reconciler (reconcileGate). Events that fail either
	// gate are silently dropped — the reconciler is never invoked.
	PreReconcile *PreReconcileConfig `yaml:"preReconcile,omitempty" json:"preReconcile,omitempty"`

	// Runtime governs the operatorBox as a long-lived entity, not a single reconcile cycle.
	// Covers autoscaling the worker pool, rollback on error, finalizer lifecycle,
	// namespace guards, and deletion protection.
	Runtime *RuntimeConfig `yaml:"runtime,omitempty" json:"runtime,omitempty"`

	// Reconcile declares what runs and how. Default (omitted or default: true) uses the
	// generic.Reconciler driven by onCreate/onReconcile/onDelete templates. Set default: false
	// and declare constructor: to bring a typed Go reconciler. Workers, resync, queue, and
	// requeue tune execution regardless of which reconciler runs.
	Reconcile *ReconcileConfig `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`

	// Emit writes the reconciler's conclusions back to the CR and the event stream.
	// status: declares fields patched onto the CR after every reconcile.
	// events: declares named structured events emitted on lifecycle transitions.
	Emit *EmitConfig `yaml:"emit,omitempty" json:"emit,omitempty"`
}

// EffectiveCross returns the cross-CRD declarations from observe.cross.
// Always call this instead of navigating the struct directly — the field
// location is owned by this method and may move without notice to callers.
func (box *OperatorBoxConfig) EffectiveCross() []CrossCRDDeclaration {
	if box == nil || box.Observe == nil {
		return nil
	}
	return box.Observe.Cross
}

// Empty reports true when this operatorBox is empty
func (box *OperatorBoxConfig) Empty() bool {
	return box == nil
}

// GetWatchEntry returns the watch entry matching the secondary GVK.
func (c *OperatorBoxConfig) GetWatchEntry(secondaryGVK string) *WatchEntry {
	if c == nil || c.Observe == nil || c.Observe.Watch == nil {
		return nil
	}

	w := c.Observe.Watch
	for i := range w {
		gvk := w[i].GVKString()
		if gvk == secondaryGVK {
			return &w[i]
		}
	}

	return nil
}

// EffectiveFinalizers returns the finalizer list from runtime.finalizers. Safe on nil receiver.
func (c *OperatorBoxConfig) EffectiveFinalizers() []string {
	if c == nil || c.Runtime == nil {
		return nil
	}
	return c.Runtime.Finalizers
}

// EffectiveAutoscale returns the autoscale config from runtime.autoscale. Safe on nil receiver.
func (c *OperatorBoxConfig) EffectiveAutoscale() *AutoscaleSpec {
	if c == nil || c.Runtime == nil {
		return nil
	}
	return c.Runtime.Autoscale
}

// HasCleanup reports whether operatorBox.runtime.cleanup has at least one condition.
func (c *OperatorBoxConfig) HasCleanup() bool {
	return c != nil && c.Runtime != nil && c.Runtime.HasCleanup()
}

// EffectiveCleanup returns the cleanup config or nil when absent. Safe on nil receiver.
func (c *OperatorBoxConfig) EffectiveCleanup() *CleanupConfig {
	if c == nil || c.Runtime == nil {
		return nil
	}
	return c.Runtime.EffectiveCleanup()
}

// EffectiveRemoveFinalizers reports whether runtime.removeFinalizers is set. Safe on nil receiver.
func (c *OperatorBoxConfig) EffectiveRemoveFinalizers() bool {
	return c != nil && c.Runtime != nil && c.Runtime.RemoveFinalizers
}

// EffectiveStatus returns the status config from emit.status.
// Returns nil when neither block is declared. Safe on nil receiver.
func (c *OperatorBoxConfig) EffectiveStatus() *StatusConfig {
	if c == nil || c.Emit == nil {
		return nil
	}
	return c.Emit.Status
}

// EffectiveOnCreate returns the onCreate templates from reconcile.onCreate. Safe on nil receiver.
func (c *OperatorBoxConfig) EffectiveOnCreate() *HookTemplates {
	if c == nil || c.Reconcile == nil {
		return nil
	}
	return c.Reconcile.OnCreate
}

// EffectiveOnReconcile returns the onReconcile templates from reconcile.onReconcile. Safe on nil receiver.
func (c *OperatorBoxConfig) EffectiveOnReconcile() *HookTemplates {
	if c == nil || c.Reconcile == nil {
		return nil
	}
	return c.Reconcile.OnReconcile
}

// EffectiveOnDelete returns the onDelete templates from reconcile.onDelete. Safe on nil receiver.
func (c *OperatorBoxConfig) EffectiveOnDelete() *HookTemplates {
	if c == nil || c.Reconcile == nil {
		return nil
	}
	return c.Reconcile.OnDelete
}

// HasEmit reports whether an emit block is declared.
func (c *OperatorBoxConfig) HasEmit() bool {
	return c != nil && c.Emit != nil
}

// EmitEntries returns the named event declarations, or nil when none are declared.
func (c *OperatorBoxConfig) EmitEntries() map[string]*EmitEventEntry {
	if c == nil || c.Emit == nil {
		return nil
	}
	return c.Emit.Events
}

// GetEventEntry returns the event entry matching the declaration name.
func (c *OperatorBoxConfig) GetEventEntry(eventName string) *EventEntry {
	if c == nil || c.Observe == nil || c.Observe.Events == nil {
		return nil
	}

	return c.Observe.Events[eventName]
}

// HookDeclaration declares where a Go hook function lives.
// The function must match: func() domain.AnyReconcileHooks.
type HookDeclaration struct {
	Location string `yaml:"location" json:"location" validate:"required"`
	Version  string `yaml:"version,omitempty" json:"version,omitempty" validate:"omitempty"`
	// Fetch: when true, ork generate runs go get <location>@<version>.
	Fetch    bool   `yaml:"fetch,omitempty" json:"fetch,omitempty"`
	Function string `yaml:"function" json:"function" validate:"required"`
	Alias    string `yaml:"alias,omitempty" json:"alias,omitempty" validate:"omitempty"`

	// ManagedResources is used for RBAC generation.
	ManagedResources []domain.ManagedResource `json:"managedResources,omitempty" yaml:"managedResources,omitempty"`

	// RunHooksFirst: when true, the hook runs before declarative templates (hybrid pattern).
	RunHooksFirst bool `yaml:"runHooksFirst,omitempty" json:"runHooksFirst,omitempty"`

	// Args are passed to the hook at reconcile time via kube.Args().
	// Values support template expressions evaluated against the CR.
	Args map[string]interface{} `yaml:"args,omitempty" json:"args,omitempty"`

	// External declares HTTP calls made before the hook is invoked.
	// Results are injected under .external.<name>.* and available in args templates.
	External []ExternalCallSpec `yaml:"external,omitempty" json:"external,omitempty"`
}

// ConstructorDeclaration declares where a custom reconciler constructor lives.
// The function must match: NewReconcilerFunc.
type ConstructorDeclaration struct {
	Location string `yaml:"location" json:"location" validate:"required"`
	Version  string `yaml:"version,omitempty" json:"version,omitempty" validate:"omitempty"`
	// Fetch: when true, ork generate runs go get <location>@<version>.
	Fetch    bool   `yaml:"fetch,omitempty" json:"fetch,omitempty"`
	Function string `yaml:"function" json:"function" validate:"required"`
	Alias    string `yaml:"alias,omitempty" json:"alias,omitempty" validate:"omitempty"`

	// ManagedResources is used for RBAC generation.
	ManagedResources []domain.ManagedResource `json:"managedResources,omitempty" yaml:"managedResources,omitempty"`

	// Args are passed to the constructor at startup via kube.Args().
	Args map[string]interface{} `yaml:"args,omitempty" json:"args,omitempty"`
}
