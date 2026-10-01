// pkg/reconciler/generic.go
package generic

import (
	"context"
	"fmt"
	"slices"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/event"
	"github.com/orkspace/orkestra/pkg/gateway/notification"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/kordinator/prepare"
	"github.com/orkspace/orkestra/pkg/runtime/runners"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// Reconciler manages the full lifecycle of one CRD.
//
// It coordinates context enrichment, cache reads, deletion handling,
// metadata management, template execution, reconciliation priority, events,
// and logging. Resource-specific operations are implemented in separate
// resource runners.
//
// PTR must be a pointer to the concrete CR struct (for example, *Database).
// The dynamic registry path uses domain.Object, which is also supported
// because the informer cache stores the underlying concrete object.
type Reconciler[PTR domain.Object] struct {
	informer cache.SharedIndexInformer
	event    event.Recorder
	kube     kubeclient.Interface
	// hooks holds type-erased, domain.Object-parameterized callbacks built at
	// construction time from the user's ReconcileHooks[PTR]. Stored as
	// ObjectHooks rather than ReconcileHooks[PTR] so the reconciler remains
	// compatible with the runtime registry path (PTR = domain.Object interface).
	hooks domain.ObjectHooks

	// targetHooks holds per-target hook sets for CRDs that have distinct hook
	// binaries per serve.target entry (TargetHookFactories non-Empty().
	// Built once at construction time; read concurrently during reconcile.
	// When empty, all targets fall back to the CRD-level hooks field.
	targetHooks map[string]domain.ObjectHooks

	operatorBox *orktypes.OperatorBoxConfig
	newObj      func() PTR
	crd         orktypes.CRDEntry
	kat         *katalog.Katalog

	// Notification
	notifStack *notification.NotificationStack
}

// discardRecorder is the package-private noop used when nil is passed for ev.
// Used by ork simulate
type discardRecorder struct{}

func (discardRecorder) Eventf(_ runtime.Object, _, _, _ string, _ ...interface{}) {}

// New constructs a Reconciler for the given CRD.
//
// PTR must be a pointer to the concrete CR type (e.g. *Database). When called
// from the runtime registry path in runtime_konstructor.go, PTR is inferred as
// domain.Object (the interface) — this is also valid because the constraint
// domain.Object is satisfied and the informer stores the correct concrete type.
//
// anyHooks, if non-nil, must implement domain.HookBinder. Every
// domain.ReconcileHooks[T] value satisfies HookBinder automatically via its
// BindToObjectHooks() method. Passing any other type panics at startup.
func New[PTR domain.Object](
	crd orktypes.CRDEntry,
	informer cache.SharedIndexInformer,
	ev event.Recorder,
	kube kubeclient.Interface,
	anyHooks domain.AnyReconcileHooks,
	newObj func() PTR,
	kat *katalog.Katalog,
) *Reconciler[PTR] {

	// Adapt the user's strongly-typed ReconcileHooks[PTR] to the type-erased
	// ObjectHooks stored on the reconciler. BindToObjectHooks wraps each hook
	// in a closure that performs obj.(PTR) at call time — safe because the
	// informer cache only ever holds objects of the type it was built for.
	var hooks domain.ObjectHooks
	if anyHooks != nil {
		binder, ok := anyHooks.(domain.HookBinder)
		if !ok {
			panic(fmt.Sprintf(
				"New[%T]: hooks value must implement domain.HookBinder "+
					"(got %T) — use domain.ReconcileHooks[*YourType]{...} or a type that "+
					"wraps one and forwards BindToObjectHooks()",
				newObj(), anyHooks,
			))
		}
		hooks = binder.BindToObjectHooks()
	}

	// Build per-target hook sets for targets that declare a distinct hook binary.
	// Targets that share the CRD-level binary only override args and use hooks above.
	targetHooks := make(map[string]domain.ObjectHooks, len(crd.TargetHookFactories))
	for targetName, factory := range crd.TargetHookFactories {
		anyH := factory()
		if binder, ok := anyH.(domain.HookBinder); ok {
			targetHooks[targetName] = binder.BindToObjectHooks()
		}
	}

	if ev == nil {
		ev = discardRecorder{}
	}

	box := crd.OperatorBox
	// Always inject a system finalizer so handleDeletion runs before the CR is removed.
	// This guarantees explicit GC for cluster-scoped resources (Namespaces, ClusterRoles,
	// ClusterRoleBindings, PVs, cluster-scoped custom resources) that Kubernetes GC
	// cannot cascade through owner references.
	if !slices.Contains(box.EffectiveFinalizers(), labels.CleanupFinalizer) {
		if box.Runtime == nil {
			box.Runtime = &orktypes.RuntimeConfig{}
		}
		box.Runtime.Finalizers = append(box.Runtime.Finalizers, labels.CleanupFinalizer)
	}

	r := &Reconciler[PTR]{
		crd:         crd,
		operatorBox: box,
		informer:    informer,
		event:       ev,
		kube:        kube,
		hooks:       hooks,
		targetHooks: targetHooks,
		newObj:      newObj,
		kat:         kat,
	}

	// Wire notification: GatewayNotifier when a gateway endpoint is configured;
	// DirectNotifier otherwise (standalone SMTP/Slack dispatch on the runtime).
	if kat != nil && crd.IsNotificationEnabled() {
		var notifier notification.Notifier
		if ep := kat.GatewayEndpoint(); ep != "" {
			notifier = notification.NewGatewayNotifier(ep)
		} else {
			notifier = notification.NewDirectNotifier(kat)
		}
		r.notifStack = notification.NewNotificationStack(kat, notifier)
	}

	return r
}

var _ domain.Reconciler = (*Reconciler[domain.Object])(nil)

// Reconcile dispatches to the correct reconcile implementation.
// Order:
//  1. Conditional provisioning (when blocks) — handled by runTemplateReconcile
//  2. Go hooks → Declarative templates → No-op (through reconcileImpl())
func (r *Reconciler[PTR]) Reconcile(ctx context.Context, req domain.Request) (domain.Result, error) {
	ctx = kubeclient.WithKubeclient(ctx, r.kube)
	if err := ctx.Err(); err != nil {
		return domain.Result{}, err
	}

	ctx = logger.WithRequestID(ctx)
	ctx = logger.WithCRD(ctx, r.crd.GVKString())
	ctx = logger.WithResource(ctx, req.Key)

	if req.Prepared == nil {
		return domain.Result{}, fmt.Errorf("req.Prepared is nil for %q — all reconciles must go through kordinator", req.Key)
	}

	box := prepare.BoxFrom(req.Prepared)
	target := req.Prepared.Target
	resolver := req.Prepared.Context.(*orktmpl.Resolver)
	obj, err := domain.ToTypedWith(req.Prepared.Object, r.newObj)
	if err != nil {
		return domain.Result{}, err
	}

	hooks := r.hooksFor(target)
	ctx = r.withTargetArgs(ctx, box)

	// Scoped kube: hook args template expressions evaluated against this resolver.
	ctx = kubeclient.WithKubeclient(ctx, r.kube.ScopedFor(resolver.TemplateEvaluator()))

	if obj.GetDeletionTimestamp() != nil {
		logger.FromContext(ctx).Info().
			Str("name", obj.GetName()).
			Msgf("deletion handler called for %s", r.crd.GVKString())
		r.event.Eventf(obj, corev1.EventTypeNormal, "Deleting",
			fmt.Sprintf("Deleting %s %s/%s", r.crd.GVKString(), obj.GetNamespace(), obj.GetName()))
		return domain.Result{}, r.handleDeletion(ctx, resolver, obj, box, hooks)
	}

	if err := r.reconcileImpl(ctx, resolver, obj, box, hooks); err != nil {
		return domain.Result{}, err
	}

	return domain.Result{}, r.cleanupPreviousSurface(ctx, obj)
}

// reconcileImpl dispatches to the correct reconcile implementation.
// Priority: Go hooks → declarative templates → no-op.
func (r *Reconciler[PTR]) reconcileImpl(ctx context.Context, resolver *orktmpl.Resolver, obj PTR, box orktypes.OperatorBoxConfig, hooks domain.ObjectHooks) error {
	var err error

	hasTemplates := box.EffectiveOnCreate() != nil || box.EffectiveOnReconcile() != nil
	switch {
	case hooks.OnReconcile != nil:
		// Go hooks — user-provided, full type-safe access.
		// Requires: ork generate registry to register in HookRegistry.
		//
		// Order: by default declared templates run first (hybrid 90/10 pattern).
		// Set hooks.runHooksFirst: true in the Katalog to run the hook first.
		if !r.crd.RunHooksFirst() && hasTemplates {
			resolver, err = r.runTemplateReconcile(ctx, resolver, obj, box)
		}
		if err == nil {
			err = hooks.OnReconcile(ctx, obj)
		}
		if err == nil && r.crd.RunHooksFirst() && hasTemplates {
			resolver, err = r.runTemplateReconcile(ctx, resolver, obj, box)
		}

	case box.EffectiveOnCreate() != nil || box.EffectiveOnReconcile() != nil:
		// Declarative templates — interpreted at runtime.
		// Requires: nothing. ork generate registry NOT needed.
		// The returned resolver carries cross/external data for status evaluation.
		resolver, err = r.runTemplateReconcile(ctx, resolver, obj, box)

	default:
		// No-op — finalizers, events, metrics still handled above.
		logger.FromContext(ctx).Info().
			Str("name", obj.GetName()).
			Msgf("reconciled %s (no-op)", r.crd.GVKString())
		// Status still patched for no-op reconcilers
	}

	if err != nil {
		logger.FromContext(ctx).Error().Err(err).
			Str("name", obj.GetName()).
			Msgf("reconciliation failed for %s", r.crd.GVKString())

		r.event.Eventf(obj, corev1.EventTypeWarning, r.crd.APITypes.Kind+"ReconcileError",
			fmt.Sprintf("Failed to reconcile %s %s/%s: %v",
				r.crd.GVKString(), obj.GetNamespace(), obj.GetName(), err))
		return err
	}

	r.event.Eventf(obj, corev1.EventTypeNormal, r.crd.APITypes.Kind+"Reconciled",
		fmt.Sprintf("Successfully reconciled %s %s/%s",
			r.crd.GVKString(), obj.GetNamespace(), obj.GetName()))

	logger.FromContext(ctx).Info().
		Str("name", obj.GetName()).
		Msgf("reconciled %s", r.crd.GVKString())

	return nil
}

// handleDeletion runs cleanup then removes our finalizers.
// Finalizers are never removed on error — object stays protected until
// cleanup succeeds.
func (r *Reconciler[PTR]) handleDeletion(ctx context.Context, resolver *orktmpl.Resolver, obj PTR, box orktypes.OperatorBoxConfig, hooks domain.ObjectHooks) error {
	switch {
	case hooks.OnDelete != nil:
		if err := hooks.OnDelete(ctx, obj); err != nil {
			r.event.Eventf(obj, corev1.EventTypeWarning, r.crd.APITypes.Kind+"DeleteError",
				fmt.Sprintf("Deletion hook failed: %v", err))
			return fmt.Errorf("deletion hook: %w", err)
		}

	case box.EffectiveOnDelete() != nil:
		if err := r.runTemplateOnDelete(ctx, resolver, obj, box); err != nil {
			r.event.Eventf(obj, corev1.EventTypeWarning, r.crd.APITypes.Kind+"DeleteError",
				fmt.Sprintf("Template deletion failed: %v", err))
			return fmt.Errorf("template deletion: %w", err)
		}
	}

	// Cluster-scoped resources require explicit deletion regardless of whether an
	// onDelete block exists: the GC does not cascade through owner references on them.
	// runTemplateOnDelete already handles this when OnDelete is set; run it here for all
	// other cases (no onDelete block, or Go hook path).
	if box.EffectiveOnDelete() == nil {
		if kube, ok := kubeclient.FromContext(ctx); ok {
			if err := runners.DeleteOwnedClusterScopedResources(ctx, kube, resolver, obj, box); err != nil {
				return fmt.Errorf("namespace cleanup: %w", err)
			}
		}
	}

	if err := r.patchStripFinalizers(ctx, obj, box); err != nil {
		r.event.Eventf(obj, corev1.EventTypeWarning, r.crd.APITypes.Kind+"FinalizerRemovalError",
			fmt.Sprintf("Failed to remove finalizers: %v", err))
		return err
	}

	r.event.Eventf(obj, corev1.EventTypeNormal, r.crd.APITypes.Kind+"Deleted",
		fmt.Sprintf("Successfully deleted %s %s/%s",
			r.crd.GVKString(), obj.GetNamespace(), obj.GetName()))

	return nil
}
