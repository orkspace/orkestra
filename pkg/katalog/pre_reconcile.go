package katalog

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/external"
	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils/common"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

// compile time check
var _ domain.Katalog = (*Katalog)(nil)

// EvaluatePreReconcile evaluates preReconcile.reconcileGate conditions for the named CRD.
// Returns (true, "") when conditions pass and the reconciler should run.
// Returns (false, reason) when gated — reconciler must not be called.
//
// preReconcile.external runs first (shared enrichment), then reconcileGate.external,
// then conditions are evaluated against the accumulated resolver.
func (k *Katalog) EvaluatePreReconcile(ctx context.Context, gvk string, obj *unstructured.Unstructured, cs kubernetes.Interface, opts domain.EvaluateOptions) (allowed bool, reason string) {
	box := k.effectiveBox(obj, gvk)
	if box.Empty() {
		return true, ""
	}

	pr := box.PreReconcile
	if pr.Empty() || !pr.HasReconcileGate() {
		return true, ""
	}

	resolver := k.effectiveResolver(ctx, obj, pr, opts)
	if resolver.Empty() {
		return true, ""
	}

	var err error
	if pr.HasPreReconcileExternal() {
		if resolver, err = external.Run(ctx, gvk, resolver, pr.External, cs); err != nil {
			return true, "" // shared pre-reconcile external: always fail-open
		}
	}
	g := pr.ReconcileGate
	if pr.HasReconcileGateExternal() {
		if resolver, err = external.Run(ctx, gvk, resolver, g.External, cs); err != nil {
			if g.FailPolicy == orktypes.FailPolicyClosed {
				return false, "reconcileGate: external evaluation failed (failPolicy: closed)"
			}
			return true, ""
		}
	}

	// Evaluate reconcileGate.sentinels (shorthand) - first match wins
	if g.HasSentinels() {
		return g.SentinelsAllowed(opts.Sentinels), ""
	}

	if !orktypes.EvaluateConditions(resolver.Data(), pr.WhenConditions(), pr.OrConditions(), resolver.TemplateEvaluator()) {
		return false, preReconcileGateReason(pr, resolver)
	}
	return true, ""
}

// EvaluateEnqueueFilter evaluates preReconcile.enqueueGate conditions for the named CRD.
// Returns true when the object should be enqueued, false when it should be dropped.
// Accepts domain.Object so it works for both dynamic and typed CRDs.
//
// preReconcile.external runs first (shared enrichment), then enqueueGate.external,
// then conditions are evaluated against the accumulated resolver.
func (k *Katalog) EvaluateEnqueueFilter(ctx context.Context, gvk string, obj domain.Object, cs kubernetes.Interface, opts domain.EvaluateOptions) bool {
	box := k.effectiveBox(obj, gvk)
	if box.Empty() {
		return true
	}

	pr := box.PreReconcile
	if pr.Empty() || !pr.HasEnqueueGate() {
		return true
	}

	resolver := k.effectiveResolver(ctx, obj, pr, opts)
	if resolver.Empty() {
		return true
	}

	var err error
	if pr.HasPreReconcileExternal() {
		if resolver, err = external.Run(ctx, gvk, resolver, pr.External, cs); err != nil {
			return true // shared pre-reconcile external: always fail-open
		}
	}

	return k.evaluateGate(ctx, gvk, pr.EnqueueGate, resolver, cs, opts.Sentinels)
}

// EvaluateWatchEnqueueFilter evaluates a watch entry's enqueueGate.
func (k *Katalog) EvaluateWatchEnqueueFilter(ctx context.Context, primaryGVK, secondaryGVK string, obj domain.Object, cs kubernetes.Interface, opts domain.EvaluateOptions) bool {
	box := k.effectiveBox(obj, primaryGVK)
	if box == nil {
		return true
	}

	resolver := k.effectiveResolver(ctx, obj, nil, opts)
	if resolver.Empty() {
		return true
	}

	entry := k.LookupByGVKString(primaryGVK).Entry()
	if entry == nil {
		return true
	}

	watchEntry := box.GetWatchEntry(secondaryGVK)
	if watchEntry == nil || watchEntry.EnqueueGate == nil {
		return true
	}

	return k.evaluateGate(ctx, secondaryGVK, watchEntry.EnqueueGate, resolver, cs, opts.Sentinels)
}

// EvaluateEventEnqueueFilter evaluates an event entry's enqueueGate.
func (k *Katalog) EvaluateEventEnqueueFilter(ctx context.Context, primaryGVK, eventName string, obj domain.Object, cs kubernetes.Interface, opts domain.EvaluateOptions) bool {
	box := k.effectiveBox(obj, primaryGVK)
	if box == nil {
		return true
	}

	resolver := k.effectiveResolver(ctx, obj, nil, opts)
	if resolver.Empty() {
		return true
	}

	entry := k.LookupByGVKString(primaryGVK).Entry()
	if entry == nil {
		return true
	}

	eventEntry := box.GetEventEntry(eventName)
	if eventEntry == nil || eventEntry.EnqueueGate == nil {
		return true
	}

	return k.evaluateGate(ctx, eventName, eventEntry.EnqueueGate, resolver, cs, opts.Sentinels)
}

// EvaluateQueueBehaviourConditions completes the queue behaviour evaluation started by the
// workqueue but delegated to the informer. Evaluates queue.behaviour conditions for the named CRD.
// Returns true when the object should be enqueued, false when it should be dropped.
// Accepts domain.Object so it works for both dynamic and typed CRDs.
//
// Here because it influences 'pre-reconcile' decisions.
func (k *Katalog) EvaluateQueueBehaviourConditions(ctx context.Context, gvk string, obj domain.Object, opts domain.EvaluateOptions) bool {
	box := k.effectiveBox(obj, gvk)
	if box.Empty() {
		return true
	}

	rc := box.Reconciler
	q := rc.Queue
	if rc.Empty() || q.Empty() || !q.HasBehaviourCondition() {
		return true
	}

	resolver := k.effectiveResolver(ctx, obj, box.PreReconcile, opts)
	if resolver == nil {
		return true
	}

	if q.HasOnLimitConditions() {
		return orktypes.EvaluateConditions(resolver.Data(), q.OnLimitWhen(), q.OnLimitOr(), resolver.TemplateEvaluator())
	}
	if q.HasOnThresholdConditions() {
		return orktypes.EvaluateConditions(resolver.Data(), q.OnThresholdWhen(), q.OnThresholdOr(), resolver.TemplateEvaluator())
	}

	return true
}

// effectiveResolver computes the common resolver used by all evaluators
func (k *Katalog) effectiveResolver(ctx context.Context, obj domain.Object, pr *orktypes.PreReconcileConfig, opts domain.EvaluateOptions) *orktmpl.Resolver {
	resolver, err := orktmpl.NewResolver(ctx, obj)
	if err != nil {
		return nil
	}
	if !k.Profiles.Empty() {
		resolver = resolver.WithProfiles(k.UserProfiles())
	}
	if !k.Notes.Empty() {
		resolver = resolver.WithUserNotes(k.UserNotes())
	}
	// .request context
	if intent := orktarget.ResolveIntentFromObject(resolver.Data()); intent != nil {
		resolver = resolver.WithRequest(intent)
	}
	// .metrics context
	if metrics := common.ResolveResourceMetricFromObject(resolver.Data()); metrics != nil {
		resolver = resolver.WithMetrics(metrics)
	}
	// .health context
	if health := common.ResolveResourceHealthFromObject(resolver.Data()); health != nil {
		resolver = resolver.WithHealth(health)
	}
	// .events context
	if len(opts.Events) > 0 {
		resolver = resolver.WithEvents(opts.Events)
	}
	if len(opts.Sentinels) > 0 {
		resolver = resolver.WithSentinels(pr.DeclaredSentinels(), opts.Sentinels)
	}
	return resolver
}

// effectiveBox computes the common operatorBox used by all evaluators
func (k *Katalog) effectiveBox(obj domain.Object, gvk string) *orktypes.OperatorBoxConfig {
	if obj == nil {
		return nil
	}
	entry := k.LookupByGVKString(gvk).Entry()
	if entry == nil {
		return nil
	}
	target := orktarget.ResolveTargetFromAnnotations(obj.GetAnnotations())
	return entry.EffectiveOperatorBox(target)
}

// preReconcileGateReason returns a human-readable description of why the gate fired.
func preReconcileGateReason(pr *orktypes.PreReconcileConfig, resolver *orktmpl.Resolver) string {
	eval := resolver.TemplateEvaluator()
	for _, cond := range pr.WhenConditions() {
		if !orktypes.EvaluateConditions(resolver.Data(), []orktypes.Condition{cond}, nil, eval) {
			val, _ := resolver.Resolve(cond.Field)
			return fmt.Sprintf("when: %q = %q, want %q", cond.Field, val, cond.Equals)
		}
	}
	return "or: no condition satisfied"
}

// evaluateGate evaluates a gate against the supplied resolver.
//
// Gate external runs first, then sentinel shorthand, then conditions.
func (k *Katalog) evaluateGate(
	ctx context.Context,
	gvk string,
	g *orktypes.GateConditions,
	resolver *orktmpl.Resolver,
	cs kubernetes.Interface,
	sentinels map[string]string,
) bool {
	if resolver == nil {
		return true
	}

	var err error
	if g.HasExternal() {
		if resolver, err = external.Run(ctx, gvk, resolver, g.External, cs); err != nil {
			if g.FailPolicy == orktypes.FailPolicyClosed {
				return false
			}
			return true
		}
	}

	// Sentinel shorthand takes precedence over conditions.
	if g.HasSentinels() {
		return g.SentinelsAllowed(sentinels)
	}

	return orktypes.EvaluateConditions(
		resolver.Data(),
		g.WhenConditions(),
		g.OrConditions(),
		resolver.TemplateEvaluator(),
	)
}

// IsEventAware reports whether the named CRD has opted into event-aware
// reconcileGate evaluation.
//
// When true, events entering the CRD's workqueue must retain their individual
// event identity rather than being coalesced with other events for the same
// object. This applies to the entire reconcileGate evaluation, not only
// sentinel conditions.
func (k *Katalog) IsEventAware(obj domain.Object, name string) bool {
	if k == nil {
		return false
	}

	box := k.effectiveBox(obj, name)
	if box == nil {
		return false
	}

	pr := box.PreReconcile
	if pr == nil {
		return false
	}

	if pr.HasReconcileGate() {
		return pr.ReconcileGate.IsEventAware()
	}

	return false
}

// GetPreReconcileSentinels returns the sentinel names declared by
// preReconcile.sentinels for the named CRD.
//
// The informer uses this declaration to compute event-time sentinel values
// from old and new objects. Sentinel declaration is owned by the Katalog;
// the informer does not maintain a separate sentinel configuration registry.
// Returns nil when the CRD is unknown or declares no sentinels.
func (k *Katalog) GetPreReconcileSentinels(obj domain.Object, gvkString string) []string {
	if k == nil {
		return nil
	}
	box := k.effectiveBox(obj, gvkString)
	if box == nil {
		return nil
	}

	pr := box.PreReconcile
	if pr == nil {
		return nil
	}

	return pr.DeclaredSentinels()
}
