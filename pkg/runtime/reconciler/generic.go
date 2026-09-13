// pkg/reconciler/generic.go
package reconciler

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/event"
	"github.com/orkspace/orkestra/pkg/gateway/notification"
	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/autoscaler"
	"github.com/orkspace/orkestra/pkg/runtime/kordinator"
	"github.com/orkspace/orkestra/pkg/runtime/kordinator/vitals"
	orkqueue "github.com/orkspace/orkestra/pkg/runtime/queue"
	"github.com/orkspace/orkestra/pkg/runtime/runners"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// GenericReconciler manages the full lifecycle of one CRD.
//
// It coordinates context enrichment, cache reads, deletion handling,
// metadata management, template execution, reconciliation priority, events,
// and logging. Resource-specific operations are implemented in separate
// resource runners.
//
// PTR must be a pointer to the concrete CR struct (for example, *Database).
// The dynamic registry path uses domain.Object, which is also supported
// because the informer cache stores the underlying concrete object.
type GenericReconciler[PTR domain.Object] struct {
	katalogRegistry   *kordinator.ResourceKatalog
	crdHealthRegistry map[string]*vitals.CRDHealth
	providerRegistry  orktypes.ProviderRegistry
	providerStats     providerStatsRecorder
	informer          cache.SharedIndexInformer
	event             event.Recorder
	kube              kubeclient.Interface
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

	operatorBox orktypes.OperatorBoxConfig
	newObj      func() PTR
	crd         orktypes.CRDEntry
	kat         *katalog.Katalog

	// workerSem gates concurrent reconcile execution. All worker goroutines run
	// continuously; the semaphore controls how many may be in Reconcile simultaneously.
	// Resized at runtime by the autoscaler when autoscale: is declared.
	workerSem *autoscaler.ResizableSemaphore

	// autoMetrics holds live operatorbox runtime metrics for autoscale evaluation.
	autoMetrics *autoscaler.AutoMetrics

	// autoscaler is non-nil when operatorBox.autoscale is declared.
	autoscaler *autoscaler.Autoscaler

	// queue is the per-CRD workqueue, injected by startCRDWorkers after construction.
	// Used by SetQueueDepthLimit and the resync goroutine.
	queue *orkqueue.Workqueue

	// resyncNs holds the current resync interval in nanoseconds.
	// 0 means the resync goroutine is idle (informer handles baseline resync).
	// Written by SetResyncInterval; read by the resync goroutine.
	resyncNs atomic.Int64

	// Notification
	notifStack *notification.NotificationStack

	// rollbackHistory tracks per-CR failure timestamps for window-based rollback triggers.
	// Key: "namespace/name". Guarded by rollbackMu.
	rollbackHistory map[string]*rollbackFailureHistory
	rollbackMu      sync.Mutex

	// spawnWorker is injected by kordinator after construction. Called by ResizeWorkers
	// when scaling up to start additional goroutines matching the new semaphore capacity.
	// nil when autoscale is not declared or kordinator hasn't injected it yet.
	spawnWorker func()

	// rollbackNotifier is injected by kordinator after construction. Called when
	// rollback is triggered or cleared so CRDHealth can track rollback stats.
	rollbackTriggerFn func()
	rollbackClearFn   func()
}

// discardRecorder is the package-private noop used when nil is passed for ev.
// Used by ork simulate
type discardRecorder struct{}

func (discardRecorder) Eventf(_ runtime.Object, _, _, _ string, _ ...interface{}) {}

// NewGenericReconciler constructs a GenericReconciler for the given CRD.
//
// PTR must be a pointer to the concrete CR type (e.g. *Database). When called
// from the runtime registry path in runtime_konstructor.go, PTR is inferred as
// domain.Object (the interface) — this is also valid because the constraint
// domain.Object is satisfied and the informer stores the correct concrete type.
//
// anyHooks, if non-nil, must implement domain.HookBinder. Every
// domain.ReconcileHooks[T] value satisfies HookBinder automatically via its
// BindToObjectHooks() method. Passing any other type panics at startup.
func NewGenericReconciler[PTR domain.Object](
	crd orktypes.CRDEntry,
	informer cache.SharedIndexInformer,
	ev event.Recorder,
	kube kubeclient.Interface,
	anyHooks domain.AnyReconcileHooks,
	newObj func() PTR,
	katalogRegistry *kordinator.ResourceKatalog,
	crdHealthRegistry map[string]*vitals.CRDHealth,
	providerRegistry orktypes.ProviderRegistry,
	providerStats providerStatsRecorder,
	kat *katalog.Katalog,
) *GenericReconciler[PTR] {

	// Adapt the user's strongly-typed ReconcileHooks[PTR] to the type-erased
	// ObjectHooks stored on the reconciler. BindToObjectHooks wraps each hook
	// in a closure that performs obj.(PTR) at call time — safe because the
	// informer cache only ever holds objects of the type it was built for.
	var hooks domain.ObjectHooks
	if anyHooks != nil {
		binder, ok := anyHooks.(domain.HookBinder)
		if !ok {
			panic(fmt.Sprintf(
				"NewGenericReconciler[%T]: hooks value must implement domain.HookBinder "+
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
	workers := box.Reconciler.Workers
	if workers <= 0 {
		workers = 1
	}
	sem := autoscaler.NewResizableSemaphore(workers)
	autoMet := autoscaler.NewAutoMetrics(sem)

	// Always inject a system finalizer so handleDeletion runs before the CR is removed.
	// This guarantees explicit GC for cluster-scoped resources (Namespaces, ClusterRoles,
	// ClusterRoleBindings, PVs, cluster-scoped custom resources) that Kubernetes GC
	// cannot cascade through owner references.
	if !slices.Contains(box.Finalizers, labels.CleanupFinalizer) {
		box.Finalizers = append(box.Finalizers, labels.CleanupFinalizer)
	}

	r := &GenericReconciler[PTR]{
		katalogRegistry:   katalogRegistry,
		crdHealthRegistry: crdHealthRegistry,
		providerRegistry:  providerRegistry,
		providerStats:     providerStats,
		crd:               crd,
		operatorBox:       box,
		informer:          informer,
		event:             ev,
		kube:              kube,
		hooks:             hooks,
		targetHooks:       targetHooks,
		newObj:            newObj,
		workerSem:         sem,
		autoMetrics:       autoMet,
		rollbackHistory:   make(map[string]*rollbackFailureHistory),
		kat:               kat,
	}

	if crd.AutoscaleEnabled() {
		baseline := orktypes.AutoscaleBaseline{
			Workers:  workers,
			MaxDepth: box.Reconciler.Queue.MaxDepth,
			Resync:   box.Reconciler.Resync.Duration,
		}
		r.autoscaler = autoscaler.NewAutoscaler(
			kube.Clientset(),
			crd.APITypes.Kind,
			box.Autoscale,
			baseline,
			r,
			autoMet,
			box.Cross,
		)
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

var _ domain.Reconciler = (*GenericReconciler[domain.Object])(nil)

// Reconcile dispatches to the correct reconcile implementation.
// Order:
//  1. Conditional provisioning (when blocks) — handled by runTemplateReconcile
//  2. Go hooks → Declarative templates → No-op (through reconcileImpl())
//
// The semaphore gates concurrent execution — when an autoscaler is active it
// can reduce effective concurrency below the goroutine count without stopping goroutines.
func (r *GenericReconciler[PTR]) Reconcile(ctx context.Context, req domain.Request) (domain.Result, error) {
	if err := r.workerSem.Acquire(ctx); err != nil {
		return domain.Result{}, err // context cancelled while waiting for a concurrency slot
	}
	start := time.Now()
	err := r.reconcileCore(ctx, req.Key)
	r.workerSem.Release()
	r.autoMetrics.RecordReconcile(time.Since(start), err != nil)
	return domain.Result{}, err
}

func (r *GenericReconciler[PTR]) reconcileCore(ctx context.Context, key string) error {
	ctx = kubeclient.WithKubeclient(ctx, r.kube)
	if err := ctx.Err(); err != nil {
		return err
	}

	ctx = logger.WithRequestID(ctx)
	ctx = logger.WithCRD(ctx, r.crd.GVKString())
	ctx = logger.WithResource(ctx, key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid key %q: %w", key, err)
	}
	_ = namespace

	raw, exists, err := r.informer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("getting %q from store: %w", key, err)
	}

	if !exists {
		logger.FromContext(ctx).Info().Msgf("%s/%s not found — deleted", namespace, name)
		if r.hooks.OnNotFound != nil {
			return r.hooks.OnNotFound(ctx, key)
		}
		return nil
	}

	obj, ok := raw.(PTR)
	if !ok {
		return fmt.Errorf("type assertion failed: expected %T, got %T", r.newObj(), raw)
	}
	rawObj := obj.DeepCopyObject().(PTR)

	// Resolve the effective operatorBox and target for this CR. CRs routed through
	// the gateway carry a serve-target annotation; the box for that target governs
	// this cycle. Falls back to the CRD-level box for direct kubectl applies.
	box, target := r.effectiveBoxAndTarget(rawObj)
	hooks := r.hooksFor(target)
	ctx = r.withTargetArgs(ctx, box)

	// Normalize before mutation/validation/template rendering ─────────────
	// Normalize + base resolver
	obj, resolver, normalizeChanges, err := r.applyNormalize(ctx, rawObj)
	if err != nil {
		return err
	}
	if len(normalizeChanges) > 0 {
		resolver = resolver.WithNormalizeChanges(normalizeChanges)
	}
	if r.kat != nil && !r.kat.Profiles.Empty() {
		resolver = resolver.WithProfiles(r.kat.Profiles)
	}
	if r.kat != nil && !r.kat.Notes.Empty() {
		resolver = resolver.WithUserNotes(r.kat.UserNotes())
	}
	// Inject raw serve intent as .request.<field> so operatorBox templates,
	// mutation rules, and validation rules can all read the caller's vocabulary.
	// Only present when the CR was submitted through the Gateway API in target mode.
	if intent := orktarget.ResolveIntentFromObject(resolver.Data()); intent != nil {
		resolver = resolver.WithRequest(intent)
	}
	// Gives operator: unique live CRD access for the rest of this reconcile
	// pass — validation.rules and any when:/or: block evaluated against
	// this resolver (mutation rules, template sources) all share it.
	resolver = resolver.WithUniquenessChecker(newUniquenessChecker(ctx, r.kube, r.crd.GVR(), r.crd.IsNamespaced()))
	// Run hook-declared external calls before ScopedFor so their results are
	// available as .external.<name>.* when args template expressions are evaluated.
	// This gives typed hooks access to external systems without needing an HTTP
	// client — the runtime makes the calls, the hook reads the results via kube.Args().
	if r.crd.HasHooksExternal() {
		resolver, err = runExternal(ctx, r.crd.GVKString(), resolver, r.crd.HooksExternal(), r.kube.Clientset())
		if err != nil {
			return err
		}
	}
	// Replace base kube in context with a scoped copy that has hooks.args
	// template expressions evaluated against this CR's resolver (full note FuncMap).
	ctx = kubeclient.WithKubeclient(ctx, r.kube.ScopedFor(resolver.TemplateEvaluator()))

	// ──────────────────────────────────────────────────────────────────────────────
	// GVK FIX: typed objects from the informer cache may arrive without a valid
	// GroupVersionKind on the very first reconcile after a CR is created.
	// This occurs because the watch event from the API server sometimes omits
	// the TypeMeta fields (`apiVersion`, `kind`). The subsequent reconcile loop
	// (e.g., after an operator restart) or a full list operation does include them.
	//
	// The effect: without a correct GVK, owner references created by hooks or
	// registry functions become invalid, causing API server rejections like
	//
	//   metadata.ownerReferences.apiVersion: Invalid value: "": version must not be empty
	//
	// The fix: set the missing GVK using the known values from the CRD entry,
	// which Orkestra parsed during startup from the Katalog. This ensures every
	// typed object presented to hooks and child‑resource creation carries a
	// complete TypeMeta.
	//
	// ──────────────────────────────────────────────────────────────────────────────
	if obj.GetObjectKind().GroupVersionKind().Empty() {
		obj.GetObjectKind().SetGroupVersionKind(r.crd.GVK())
	}
	// Check if resource is being deleted
	if obj.GetDeletionTimestamp() != nil {
		logger.FromContext(ctx).Info().
			Str("name", obj.GetName()).
			Msgf("deletion handler called for %s", r.crd.GVKString())

		r.event.Eventf(obj, corev1.EventTypeNormal, "Deleting",
			fmt.Sprintf("Deleting %s %s/%s", r.crd.GVKString(), obj.GetNamespace(), obj.GetName()))

		return r.handleDeletion(ctx, resolver, obj, box, hooks)
	}

	// Namespace guard — skip reconcile for CRs in restricted or non-allowed namespaces.
	// Deletion is always allowed so finalizers can be removed; this guard runs after
	// the deletion-timestamp check so deleting CRs are never blocked.
	if r.crd.HasNamespaceRules() {
		result := CheckNamespace(ctx, obj, obj.GetNamespace(), r.crd.RestrictedNamespaces, r.crd.AllowedNamespaces, r.crd.APITypes.Kind)
		if !result.Allowed {
			logger.FromContext(ctx).Debug().
				Str("name", obj.GetName()).
				Str("namespace", obj.GetNamespace()).
				Str("reason", result.Reason).
				Msg("reconcile: skipping CR in restricted/non-allowed namespace")
			return nil
		}
	}

	// ── Finalizer management ─────────────────────────────────────────────────────
	// Finalizers block deletion until cleanup is complete.
	// If RemoveFinalizers is false (normal operation), ensure required finalizers exist.
	// If RemoveFinalizers is true (e.g., for testing or forced cleanup), remove them.
	if !r.crd.RemoveFinalizers {
		if err := r.ensureFinalizers(ctx, obj, box); err != nil {
			r.event.Eventf(obj, corev1.EventTypeWarning, r.crd.APITypes.Kind+"FinalizerError",
				fmt.Sprintf("Failed to add finalizers: %v", err))
			return err
		}
	} else {
		logger.FromContext(ctx).Debug().Msgf("removing finalizers for %s", obj.GetName())
		if err := r.removeFinalizers(ctx, obj, box); err != nil {
			r.event.Eventf(obj, corev1.EventTypeWarning, r.crd.APITypes.Kind+"FinalizerRemovalError",
				fmt.Sprintf("Failed to remove finalizers: %v", err))
			return err
		}
		logger.FromContext(ctx).Debug().Msgf("finalizers removed for %s", obj.GetName())
	}

	// ── Label & annotation management ────────────────────────────────────────────
	// The label manager applies three label invariants derived from the Katalog:
	//   1. managed=true           — ownership marker, always present
	//   2. deletion-protection    — present iff global protection + CRD protectCRs
	//   3. strict-mode-exempt     — present iff the CRD has opted out of strictMode
	//
	// All three are computed in memory first and sent as a single JSON Merge Patch
	// (the controller-runtime MergeFrom pattern). Absent keys in a Merge Patch are
	// left unchanged by the server; to delete a key the patch must set it to null.
	// PatchLabels handles this by diffing serverLabels (snapshot) against the
	// post-mutation desired state.
	//
	// Two-phase protection removal: when transitioning from protected→unprotected,
	// both the deletion-protection label and the exempt label would normally be
	// removed. But the strict-mode webhook intercepts the UPDATE and only allows
	// removal when strict-mode-exempt=true appears in the new object. Removing both
	// in the same patch fails that check. The reconciler therefore forces the exempt
	// label ON for the first cycle; on the next cycle deletion-protection is already
	// gone, the webhook objectSelector no longer matches, and the exempt label can
	// be cleaned up without any webhook interception.
	labelMgr := labels.NewManager(labels.Config{
		Standalone:                r.kat.IsStandaloneGateway(),
		DeletionProtectionEnabled: r.kat.IsDeletionProtectionEnabled(),
	})

	serverLabels := copyStringMap(obj.GetLabels()) // snapshot before any mutation

	labelMgr.EnsureManagedLabel(obj)

	if r.kat.IsDeletionProtectionEnabled() {
		shouldHaveProtection := r.kat.IsDeletionProtectionEnabled() && r.crd.ShouldProtectCRs()
		labelMgr.EnsureDeletionProtectionLabel(obj, shouldHaveProtection)

		effectiveStrict := r.crd.IsStrictDeletionProtection(r.kat.IsStrictModeEnabled())
		currentlyProtected := serverLabels[labels.DeletionProtectionLabel] == labels.DeletionProtectionValue
		if !shouldHaveProtection && currentlyProtected {
			effectiveStrict = false // keep exempt label present so the webhook allows the removal
		}

		logger.Debug().
			Str("crd", r.crd.Name).
			Str("resource", obj.GetName()).
			Bool("effectiveStrict", effectiveStrict).
			Bool("currentlyProtected", currentlyProtected).
			Msg("label: evaluating strict mode")
		labelMgr.EnsureStrictModeExemptLabel(obj, effectiveStrict)
	}

	// User-defined labels from CRDEntry.Labels — values are templates resolved
	// against the current CR. Keys must be static valid label identifiers.
	if r.crd.HasUserLabels() {
		resolved := make(map[string]string, len(r.crd.Labels))
		for k, v := range r.crd.Labels {
			val, err := resolver.Resolve(v)
			if err != nil {
				return fmt.Errorf("labels: CRD %q: key %q: %w", r.crd.Name, k, err)
			}
			resolved[k] = val
		}
		labelMgr.EnsureUserLabels(obj, resolved)
	}

	// One atomic patch: diff serverLabels → desired. No-op if nothing changed.
	if err := r.kube.PatchLabels(ctx, obj, serverLabels, obj.GetLabels(), metav1.PatchOptions{}); err != nil {
		return err
	}

	// Annotations only ever add keys (managed-by, managed-since are write-once),
	// so a plain Merge Patch with the desired map is correct here.
	if labelMgr.EnsureManagedAnnotations(obj, r.crd.KatalogName) {
		if err := r.kube.PatchAnnotations(ctx, obj, obj.GetAnnotations(), metav1.PatchOptions{}); err != nil {
			return err
		}
	}

	// ── Step 5: Reconcile implementation ──────────────────────────────────────
	if err := r.reconcileImpl(ctx, resolver, obj, box, hooks); err != nil {
		return err
	}

	// ── Step 6: Surface orphan cleanup ────────────────────────────────────────
	return r.cleanupPreviousSurface(ctx, rawObj)
}

// reconcileImpl dispatches to the correct reconcile implementation.
// Priority: Go hooks → declarative templates → no-op.
//
// Rollback phase order:
//  1. Rollback gate  — if rollback is active, re-apply previous spec and return
//  2. Snapshot       — on success, capture current spec as rollback baseline
//  3. Mutation/validation
//  4. Reconcile dispatch
//  5. Failure trigger check — record failure; trigger rollback if threshold met
//  6. Status patch
func (r *GenericReconciler[PTR]) reconcileImpl(ctx context.Context, resolver *orktmpl.Resolver, obj PTR, box orktypes.OperatorBoxConfig, hooks domain.ObjectHooks) error {
	var err error

	// ── Phase 1: Rollback gate ────────────────────────────────────────────────
	//
	// 	In Development
	//
	// if isRollbackActive(obj) {
	// 	logger.FromContext(ctx).Info().
	// 		Str("name", obj.GetName()).
	// 		Msg("rollback: active — blocking normal reconcile")
	// 	if rbErr := r.runRollback(ctx, resolver, obj); rbErr != nil {
	// 		logger.FromContext(ctx).Error().Err(rbErr).
	// 			Str("name", obj.GetName()).
	// 			Msg("rollback: failed to re-apply previous state")
	// 	}
	// 	r.patchStatusWithChildren(ctx, obj, resolver, fmt.Errorf("rollback active"))
	// 	return nil // do not propagate — stays in rollback loop
	// }

	// ── Reconcile-time mutation and validation ────────────────────────────────
	// Ordering respects MutationConfig.MutateFirst:
	//   true (default)           — mutate first (apply defaults) → validate → reconcile
	//   false 					  — validate → mutate valid objects → reconcile
	//
	// Mutation failures are non-fatal: logged, reconcile continues.
	// Validation deny failures halt reconcile and return an error.

	// ── Reconcile-time mutation and validation ────────────────────────────────
	if r.crd.HasMutationRules() && r.crd.ShouldMutateFirst() {
		var mutErr error
		resolver, mutErr = r.applyReconcileTimeMutation(ctx, resolver, obj)
		if mutErr != nil {
			logger.FromContext(ctx).Warn().Err(mutErr).
				Str("name", obj.GetName()).
				Msg("reconcile mutation failed — continuing")
		}
	}

	var lastValResult *ValidationResult
	if r.crd.HasValidationRules() {
		var valErr error
		resolver, lastValResult, valErr = r.applyReconcileTimeValidation(ctx, resolver, obj)
		if valErr != nil {
			r.patchStatusWithChildren(ctx, obj, resolver, valErr, box, lastValResult)
			return valErr
		}
	}

	if r.crd.HasMutationRules() && !r.crd.ShouldMutateFirst() {
		var mutErr error
		resolver, mutErr = r.applyReconcileTimeMutation(ctx, resolver, obj)
		if mutErr != nil {
			logger.FromContext(ctx).Warn().Err(mutErr).
				Str("name", obj.GetName()).
				Msg("reconcile mutation failed — continuing")
		}
	}
	hasTemplates := box.OnCreate != nil || box.OnReconcile != nil
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

	case box.OnCreate != nil || box.OnReconcile != nil:
		// Declarative templates — interpreted at runtime.
		// Requires: nothing. ork generate registry NOT needed.
		// The returned resolver carries cross/external/git data for status evaluation.
		resolver, err = r.runTemplateReconcile(ctx, resolver, obj, box)

	default:
		// No-op — finalizers, events, metrics still handled above.
		logger.FromContext(ctx).Info().
			Str("name", obj.GetName()).
			Msgf("reconciled %s (no-op)", r.crd.GVKString())
		// Status still patched for no-op reconcilers
	}

	// ── Phase 5: Rollback trigger check ─────────────────────────────────────
	// TODO
	// if err != nil && r.crd.HasRollbackRules() {
	// 	key := obj.GetNamespace() + "/" + obj.GetName()
	// 	h := r.getFailureHistory(key)
	// 	derived := r.crd.OperatorBox.DerivedRollback()
	// 	h.record(derived.Trigger.EffectiveConsecutiveFailures())
	// 	if r.shouldRollback(len(h.times), h) {
	// 		logger.FromContext(ctx).Warn().
	// 			Str("name", obj.GetName()).
	// 			Msg("rollback: threshold reached — marking rollback active")
	// 		if markErr := r.markRollbackActive(ctx, obj); markErr != nil {
	// 			logger.FromContext(ctx).Error().Err(markErr).Msg("rollback: failed to mark active")
	// 		}
	// 	}
	// }

	// ── Phase 6: Snapshot + rollback cleanup ─────────────────────────────────
	// TODO
	// if err == nil && !r.crd.HasRollbackRules() {
	// 	// If a prior rollback cycle resolved (user corrected spec, generation
	// 	// advanced), clear the stale RollbackGenerationAnnotation and notify
	// 	// CRDHealth. snapshotSpec re-writes PreviousSpecAnnotation immediately after.
	// 	annots := obj.GetAnnotations()
	// 	if annots[orktypes.RollbackGenerationAnnotation] != "" {
	// 		if clrErr := r.clearRollback(ctx, obj); clrErr != nil {
	// 			logger.FromContext(ctx).Warn().Err(clrErr).
	// 				Str("name", obj.GetName()).
	// 				Msg("rollback: failed to clear stale rollback annotation — continuing")
	// 		}
	// 	}
	// 	if snapErr := r.snapshotSpec(ctx, obj); snapErr != nil {
	// 		logger.FromContext(ctx).Warn().Err(snapErr).
	// 			Str("name", obj.GetName()).
	// 			Msg("rollback: failed to snapshot spec — continuing")
	// 	}
	// 	r.clearFailureHistory(obj.GetNamespace() + "/" + obj.GetName())
	// }

	// Inject live runtime metrics into the resolver so status.fields templates
	// can reference .metrics.queueDepth, .metrics.workers, .metrics.autoscaleActive, etc.
	metricsMap := r.autoMetrics.AsMap()
	if r.autoscaler != nil {
		if snap := r.autoscaler.Snapshot(); snap != nil {
			metricsMap["autoscaleActive"] = snap.OverrideActive
		}
	} else {
		metricsMap["autoscaleActive"] = false
	}
	resolver = resolver.WithMetrics(metricsMap)

	// Inject live runtime health into the resolver so status.fields templates
	// can reference .health.healthy, .health.state, .health.uptime,
	// .health.totalReconciles, .health.lastError, etc.
	//
	// This surfaces the operatorbox health endpoint directly into templates,
	// enabling CR status fields to show live reconcile health, uptime,
	// dependency health, and error information without any API calls.
	h, healthOk := r.crdHealthRegistry[r.crd.GVKString()]
	if healthOk {
		resolver = resolver.WithHealth(h.HealthAsMap())
	}

	// Inject live runtime metrics/health also to the object as annotation so that the gateway
	// Uses it for metrics-level and health-level gating in validation and mutation rules.
	// This respects crossAccess and endpoint security declaration
	r.injectRuntimeHealthAndMetrics(obj, metricsMap, h.HealthAsMap(), healthOk)

	// Always patch status — best-effort, never fails reconcile.
	// Called with the outcome so Ready condition reflects reality.
	// Must run before the error return so Ready=False is written on failure.
	r.patchStatusWithChildren(ctx, obj, resolver, err, box, lastValResult)

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
func (r *GenericReconciler[PTR]) handleDeletion(ctx context.Context, resolver *orktmpl.Resolver, obj PTR, box orktypes.OperatorBoxConfig, hooks domain.ObjectHooks) error {
	switch {
	case hooks.OnDelete != nil:
		if err := hooks.OnDelete(ctx, obj); err != nil {
			r.event.Eventf(obj, corev1.EventTypeWarning, r.crd.APITypes.Kind+"DeleteError",
				fmt.Sprintf("Deletion hook failed: %v", err))
			return fmt.Errorf("deletion hook: %w", err)
		}

	case box.OnDelete != nil:
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
	if box.OnDelete == nil {
		if kube, ok := kubeclient.FromContext(ctx); ok {
			if err := runners.DeleteOwnedClusterScopedResources(ctx, kube, resolver, obj, box); err != nil {
				return fmt.Errorf("namespace cleanup: %w", err)
			}
		}
	}

	if err := r.removeFinalizers(ctx, obj, box); err != nil {
		r.event.Eventf(obj, corev1.EventTypeWarning, r.crd.APITypes.Kind+"FinalizerRemovalError",
			fmt.Sprintf("Failed to remove finalizers: %v", err))
		return err
	}

	r.event.Eventf(obj, corev1.EventTypeNormal, r.crd.APITypes.Kind+"Deleted",
		fmt.Sprintf("Successfully deleted %s %s/%s",
			r.crd.GVKString(), obj.GetNamespace(), obj.GetName()))

	return nil
}
