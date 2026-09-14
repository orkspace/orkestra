// pkg/orktypes/types.go
package types

import (
	"strings"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/utils"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ── Registries ────────────────────────────────────────────────────────────────
// Package-level registries — one set per Orkestra instance.
// Populated by RegisterRuntimeObjects() in zz_generated_runtime_registry.go,
// which is produced by `ork generate registry --file <path>`.
// Keyed by schema.GroupVersionKind. Set during Katalog validation.
//
// User code never reads or writes these directly. Orkestra reads them during
// Katalog validation via addRuntimeObjects(), addHooks(), and addReconcilers().

var ObjectRegistry = map[schema.GroupVersionKind]func() runtime.Object{}
var ListRegistry = map[schema.GroupVersionKind]func() runtime.Object{}
var HookRegistry = map[schema.GroupVersionKind]func() domain.AnyReconcileHooks{}
var ReconcilerRegistry = map[schema.GroupVersionKind]NewReconcilerFunc{}

// TargetHookRegistry and TargetReconcilerRegistry hold per-target factories for
// CRDs whose targets declare a distinct hook binary or custom constructor.
// Outer key: GVK. Inner key: target name (matches serve.target.<name>).
// Populated by the generator alongside HookRegistry / ReconcilerRegistry and
// consumed by addTargetHooks() / addTargetConstructors() during Katalog validation.
//
// Targets that share the CRD-level binary (only overriding hooks.args) do NOT
// appear here — mergeReconcilerConfig in EffectiveOperatorBox handles them at
// reconcile time without any separate registration.
var TargetHookRegistry = map[schema.GroupVersionKind]map[string]func() domain.AnyReconcileHooks{}
var TargetReconcilerRegistry = map[schema.GroupVersionKind]map[string]NewReconcilerFunc{}

// SchemeAdderFns holds AddToScheme functions collected from generated init()
// calls. Each generated zz_generated_runtime_registry.go appends to this slice
// in its init(); NewSchemeRegistry drains it via RegisterTypedScheme.
// This is the bridge between the user's generated package and the Orkestra
// internal pkg/runtime stub — no explicit call needed beyond the blank import.
var SchemeAdderFns []func(*runtime.Scheme) error

// ── CRDMode ────────────────────────────────────────────────────────────

// CRDMode controls how the GenericReconciler handles CR objects at runtime.
//
// typed
//
//	Requires compiled API types via apiTypes.location.
//	Objects are decoded into concrete Go structs by the REST client.
//	Full type safety and generated DeepCopy methods.
//	Required when Go hooks reference typed fields directly.
//	Use when you have generated API types and want compile-time guarantees.
//
// dynamic
//
//	No compiled types needed. Objects are map[string]interface{} at runtime.
//	Works with any CRD without code generation or controller-gen.
//	Required for declarative hook templates (onCreate, onReconcile, onDelete)
//	because field values are resolved at reconcile time via Go text/template
//	expressions against the live CR object map.
//	Use when you want zero-code operator behavior from the Katalog alone.
//
// Auto-detection when mode is omitted:
//
//	apiTypes.location is set   → typed       (compiled types available)
//	apiTypes.location is empty → dynamic (no compiled types)
//
// Override auto-detection by setting mode explicitly:
//
//	crd:
//	 - name: websites
//	   mode: dynamic   		# force dynamic even if location is set
//	   mode: typed          # force typed even if location is empty
type CRDMode string
type DependencyCondtion string

const (
	// KonductorLeaseName is the coordination.k8s.io/v1 Lease that holds the Runtime leader identity.
	KonductorLeaseName = "orkestra-konductor"

	CRDModeTyped   CRDMode = "typed"
	CRDModeDynamic CRDMode = "dynamic"

	// DependencyCondtion values — gate a CRD's reconcile on the state of its dependencies.
	DependencyConditionStarted  DependencyCondtion = "started"
	DependencyConditionHealthy  DependencyCondtion = "healthy"
	DependencyCondtionPending   DependencyCondtion = "pending"
	DependencyCondtionReady     DependencyCondtion = "ready"
	DependencyConditionDegraded DependencyCondtion = "degraded"
	DependencyConditionDeleted  DependencyCondtion = "deleted"
)

func (m CRDMode) String() string {
	return string(m)
}

var (
	readLocal       = utils.ReadLocal
	strictUnmarshal = utils.StrictUnmarshal
)

// ── APITypes ──────────────────────────────────────────────────────────────────
// Mirrors the apiTypes block in crd-katalog.yaml.
// ork generate reads this block to emit ObjectRegistry + ListRegistry entries
// and the RegisterScheme() function.

type APITypes struct {
	// Object — Go type name for a single CR instance. Required for typed mode.
	// Used by ork generate to emit ObjectRegistry entries.
	// e.g. "Project" → func() runtime.Object { return &projv1.Project{} }
	Object string `yaml:"object,omitempty" json:"object,omitempty" validate:"omitempty"`

	// List — Go type name for the CR list. Required for typed mode.
	// Used by ork generate to emit ListRegistry entries.
	// e.g. "ProjectList" → func() runtime.Object { return &projv1.ProjectList{} }
	List string `yaml:"objectList,omitempty" json:"objectList,omitempty" validate:"omitempty"`

	// Alias — Go import alias for the API types package. Optional.
	// Auto-derived from the last two segments of Location if not set.
	// e.g. "projv1" → import projv1 "github.com/.../project/v1alpha1"
	Alias string `yaml:"alias,omitempty" json:"alias,omitempty" validate:"omitempty"`

	// Group — Kubernetes API group. Required in all modes.
	// e.g. "platform.orkestra.io"
	Group string `yaml:"group" json:"group" validate:"required,hostname_rfc1123"`

	// Version — API version. Required in all modes.
	// e.g. "v1alpha1"
	Version string `yaml:"version" json:"version" validate:"required"`

	// Kind — resource Kind. Required in all modes.
	// e.g. "Platform"
	Kind string `yaml:"kind" json:"kind" validate:"required"`

	// Plural — lowercase plural resource name. Required in all modes.
	// Used for REST client URL construction.
	// e.g. "projects"
	Plural string `yaml:"plural" json:"plural" validate:"required"`

	// APIPath — REST API path prefix. Default: /apis.
	// Override to /api only for core Kubernetes types (Pod, ConfigMap, etc.)
	// Almost always leave this empty — Orkestra defaults it to /apis.
	APIPath string `yaml:"apiPath,omitempty" json:"apiPath,omitempty" validate:"omitempty"`

	// Location — fully qualified Go import path for the API types package.
	// Required for typed mode. Used by ork generate for import statements
	// and scheme registration in RegisterScheme().
	// Not needed for dynamic mode — omit entirely.
	// e.g. "github.com/orkspace/orkestra/api/types/project/v1alpha1"
	Location string `yaml:"location,omitempty" json:"location,omitempty" validate:"omitempty"`
}

// ── Queue ─────────────────────────────────────────────────────────────────────
type QueueType string

const (
	// TypedDelayingInterface is an Interface that can Add an item at a later time.
	// This makes it easier to requeue items after failures without ending up in a hot-loop.
	//
	// https://pkg.go.dev/k8s.io/client-go@v0.36.1/util/workqueue#TypedDelayingInterface
	//
	// 	queue:
	// 	  type: "delayed"
	DelayedQueueType QueueType = "delayed"

	// TypedRateLimitingInterface is an interface that rate limits items being added to the queue.
	//
	// This is the default when not defined
	//
	// 	queue:
	// 	  type: ""
	RateLimitedQueueType QueueType = "ratelimited"
)

func (t QueueType) String() string {
	return string(t)
}

func (t QueueType) ValidQueueTypes() []string {
	return []string{DelayedQueueType.String(), RateLimitedQueueType.String()}
}

func (t QueueType) IsValid(q string) bool {
	q = strings.ReplaceAll(q, "-", "")
	q = strings.ReplaceAll(q, "/", "")
	q = strings.ReplaceAll(q, " ", "")

	for _, name := range t.ValidQueueTypes() {
		if strings.ToLower(q) == name {
			return true
		}
	}
	return false
}

type Queue struct {
	// Shared — use the shared default workqueue instead of a per-CRD queue.
	// Default: false (each CRD gets its own isolated queue).
	Shared *bool `yaml:"shared,omitempty" json:"shared,omitempty"`

	// QType — chooses the type of workqueue to be used.
	// Valid values are "delayed", "ratelimited"
	//
	// 	queue:
	// 	  type: "delayed"
	//
	// Default is ratelimited
	QType string `yaml:"type,omitempty" json:"type,omitempty"`

	// MaxDepth — depth reference for behaviour:. 0 means unlimited — no items are dropped based
	// on depth alone. A positive value sets the limit at which behaviour.onLimit fires and the
	// 100% reference for behaviour.onThreshold. Has no effect without a behaviour declaration.
	MaxDepth int `yaml:"maxDepth,omitempty" json:"maxDepth,omitempty" validate:"omitempty,gte=0"`

	// FailureThreshold — consecutive failures before CRD health degrades.
	// 0 → uses FAILURE_THRESHOLD env var.
	FailureThreshold int `yaml:"failureThreshold,omitempty" json:"failureThreshold,omitempty" validate:"omitempty,gte=0"`

	// Behaviour — per-CRD queue behaviour. Configures what happens when the queue
	// reaches its capacity limit or a declared depth threshold.
	//
	// Without conditions (always drop on limit):
	//
	// 	queue:
	//	  behaviour:
	//	    onLimit:
	//	      drop: true
	//
	// With conditions (delegate drop decision to the evaluator):
	//
	// 	queue:
	//	  behaviour:
	//	    onLimit:
	//	      when:
	//	        - field: "{{ inBusinessHours }}"
	//	          equals: false
	Cfg *QueueBehaviour `yaml:"behaviour,omitempty" json:"behaviour,omitempty"`

	// RetryBackoff — per-CRD backoff applied inside the reconcile loop when
	// the reconciler returns an error before re-enqueuing. Shorthand ("5s")
	// sets initial only; full form controls initial/max/multiplier/maxAttempts.
	RetryBackoff *RetryBackoffConfig `yaml:"retryBackoff,omitempty" json:"retryBackoff,omitempty"`
}

// Empty reports whether the queue configuration has no meaningful settings.
// Used to skip unnecessary config blocks in the Katalog.
func (q *Queue) Empty() bool {
	if q == nil {
		return true
	}
	return q.MaxDepth == 0 && q.Cfg == nil && q.RetryBackoff == nil && q.QType == "" && q.Shared == nil && q.FailureThreshold == 0
}

func (q *Queue) Type() string {
	if q == nil {
		return ""
	}
	if q.QType == "" {
		q.QType = RateLimitedQueueType.String()
	}
	return q.QType
}

func (q *Queue) IsRatelimitedType(s string) bool {
	if q == nil {
		return false
	}
	return s == RateLimitedQueueType.String()
}

func (q *Queue) IsDelayedType(s string) bool {
	if q == nil {
		return false
	}
	return s == DelayedQueueType.String()
}

var _ domain.Workqueue = (*Queue)(nil)

func (q *Queue) MaxQueueDepth() int {
	if q == nil {
		return 0
	}
	return q.MaxDepth
}

func (q *Queue) ThresholdValue() int {
	if q == nil || !q.HasBehaviour() {
		return 0
	}
	return q.Behaviour().ThresholdValue()
}

// HasRetryBackoff reports whether a retryBackoff is declared on this queue.
func (q *Queue) HasRetryBackoff() bool {
	return q != nil && q.RetryBackoff != nil
}

// IsUnlimited reports whether a MaxDepth is 0.
func (q *Queue) IsUnlimited() bool {
	return q != nil && q.MaxDepth == 0
}

// HasBehaviour reports whether behaviour is declared on this queue.
func (q *Queue) HasBehaviour() bool {
	return q != nil && q.Cfg != nil
}

// HasOnLimit reports whether onLimit behaviour is declared on this queue.
func (q *Queue) HasOnLimit() bool {
	return q != nil && q.Cfg != nil && q.Cfg.OnLimit != nil
}

// HasOnThreshold reports whether onThreshold behaviour is declared on this queue.
func (q *Queue) HasOnThreshold() bool {
	return q != nil && q.Cfg != nil && q.Cfg.OnThreshold != nil
}

// HasOnLimitConditions reports whether any when/or conditions are declared for onLimit.
func (q *Queue) HasOnLimitConditions() bool {
	return q.HasOnLimit() && q.Behaviour().OnLimit.HasCondition()
}

// HasOnThresholdConditions reports whether any when/or conditions are declared for onThreshold.
func (q *Queue) HasOnThresholdConditions() bool {
	return q.HasOnThreshold() && q.Behaviour().OnThreshold.HasCondition()
}

// HasBehaviourCondition returns true if there is at least one of when/or in onLimit or onThreshold
func (q *Queue) HasBehaviourCondition() bool {
	return q.HasBehaviour() && q.Behaviour().HasCondition()
}

// OnLimitWhen returns the when conditions for onLimit.
func (q *Queue) OnLimitWhen() []Condition {
	if !q.HasOnLimit() {
		return nil
	}
	return q.Cfg.OnLimit.When
}

// OnLimitOr returns the or conditions for onLimit.
func (q *Queue) OnLimitOr() []Condition {
	if !q.HasOnLimit() {
		return nil
	}
	return q.Cfg.OnLimit.Or
}

// OnThresholdWhen returns the when conditions for onThreshold.
func (q *Queue) OnThresholdWhen() []Condition {
	if !q.HasOnThreshold() {
		return nil
	}
	return q.Cfg.OnThreshold.When
}

// OnThresholdOr returns the or conditions for onThreshold.
func (q *Queue) OnThresholdOr() []Condition {
	if !q.HasOnThreshold() {
		return nil
	}
	return q.Cfg.OnThreshold.Or
}

// Behaviour returns the behaviour configuration for this queue. Nil-safe.
func (q *Queue) Behaviour() *QueueBehaviour {
	if q.Empty() || !q.HasBehaviour() {
		return nil
	}
	return q.Cfg
}

// ThresholdReached reports true if the declared onThreshold percent has been reached.
func (q *Queue) ThresholdReached(depth int) bool {
	if q == nil || !q.HasOnThreshold() {
		return false
	}

	max := q.MaxDepth
	if max == 0 {
		return false
	}
	thresh := q.Behaviour().ThresholdValue()
	percent := float64(depth) / float64(max) * 100

	return float64(thresh) <= percent
}

// Empty reports whether the queue behaviour configuration has no meaningful settings.
// Used to skip unnecessary config blocks in the Katalog.
func (q *QueueBehaviour) Empty() bool {
	if q == nil {
		return true
	}
	return q.OnLimit == nil && q.OnThreshold == nil
}

// QueueBehaviour is declared under queue.behaviour
// It contains what happens to the queue on limit (currently supported)
type QueueBehaviour struct {
	// Declares what happens to an item when the queue capacity has reached its limit
	OnLimit *QueueBehaviourSetting `yaml:"onLimit,omitempty" json:"onLimit,omitempty"`

	// OnThreshold declares what happens when a depth-percent threshold is reached.
	// value: is required and must be between 1 and 100 — it is the queue fullness
	// percentage at which the behaviour fires. drop: is always true when onThreshold
	// is declared; setting it to false is accepted but warned.
	//
	// 	queue:
	//	  behaviour:
	//	    onThreshold:
	//	      value: 70
	//
	// With conditions (delegate drop decision to the evaluator):
	//
	// 	queue:
	//	  behaviour:
	//	    onThreshold:
	//	      value: 70
	//	      when:
	//	        - field: "{{ inBusinessHours }}"
	//	          equals: false
	OnThreshold *QueueBehaviourSetting `yaml:"onThreshold,omitempty" json:"onThreshold,omitempty"`
}

// HasOnLimit reports whether onLimit is configured for this queue's behaviour.
func (b *QueueBehaviour) HasOnLimit() bool {
	return b != nil && b.OnLimit != nil
}

// HasOnThreshold reports whether onThreshold is configured for this queue's behaviour.
func (b *QueueBehaviour) HasOnThreshold() bool {
	return b != nil && b.OnThreshold != nil
}

// ThresholdValue returns the onThreshold.value. Nil-safe.
func (b *QueueBehaviour) ThresholdValue() int {
	if b == nil || !b.HasOnThreshold() {
		return 0
	}
	return b.OnThreshold.ThresholdValue()
}

// HasCondition reports whether any when/or conditions are declared on either setting.
func (s *QueueBehaviour) HasCondition() bool {
	if s.Empty() {
		return false
	}
	if s.HasOnThreshold() {
		return s.OnThreshold.HasCondition()
	}
	return s.OnLimit.HasCondition()
}

// QueueBehaviourSetting defines the action taken when the queue reaches its limit or threshold.
type QueueBehaviourSetting struct {
	// Drop controls whether the item is dropped when the limit/threshold fires.
	// Default: false (unlimited queue). onThreshold always sets drop to true.
	Drop *bool `yaml:"drop,omitempty" json:"drop,omitempty"`

	// Value is the queue-fullness percentage at which onThreshold fires.
	// Required under onThreshold; ignored under onLimit. Must be 1–100.
	Value int `yaml:"value,omitempty" json:"value,omitempty"`

	// When is a list of conditions that must ALL be true for this field to be
	// visible. Evaluated client-side as the user fills the form. An empty When
	// means the field is always visible.
	// Supports: equals, notEquals, time, dayOfWeek, cron, negate — same as
	// template source when: blocks.
	When []Condition `yaml:"when,omitempty" json:"when,omitempty"`

	// Or is a list of conditions where at least ONE must be true for the
	// field to be visible. OR counterpart to When (AND).
	Or []Condition `yaml:"or,omitempty" json:"or,omitempty"`
}

// HasCondition reports whether any when/or condition are declared.
func (s *QueueBehaviourSetting) HasCondition() bool {
	return s != nil && (len(s.When) > 0 || len(s.Or) > 0)
}

// WhenConditions returns when conditions declared.
func (s *QueueBehaviourSetting) WhenConditions() []Condition {
	if s == nil {
		return nil
	}
	return s.When
}

// OrConditions returns any or conditions declared.
func (s *QueueBehaviourSetting) OrConditions() []Condition {
	if s == nil {
		return nil
	}
	return s.Or
}

// HasDrop reports whether drop is declared.
func (s *QueueBehaviourSetting) HasDrop() bool {
	return s != nil && s.Drop != nil
}

// ShouldDrop reports whether drop is true.
func (s *QueueBehaviourSetting) ShouldDrop() bool {
	return s.HasDrop() && s.Drop == boolPtr(true)
}

// HasValue reports whether value is declared.
func (s *QueueBehaviourSetting) HasValue() bool {
	return s != nil && s.Value > 0
}

// ThresholdValue returns the onThreshold.value. Nil-safe.
func (s *QueueBehaviourSetting) ThresholdValue() int {
	if s == nil || !s.HasValue() {
		return 0
	}
	return s.Value
}

// IsValidValue reports whether value declared is between 1 and 100.
func (s *QueueBehaviourSetting) IsValidValue() bool {
	return s != nil && (s.Value >= 1 && s.Value < 100)
}
