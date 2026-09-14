package domain

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// EvaluateOptions provides resolver data used during evaluator execution.
type EvaluateOptions struct {
	// Events contains observed Kubernetes Event data keyed by EventEntry name.
	Events map[string]interface{}

	// Sentinels contains computed sentinel values for the observed object.
	Sentinels map[string]string
}

// Katalog is the subset of pkg/katalog.Katalog used by packages that cannot
// import pkg/katalog directly (pkg/runtime/informer, pkg/runtime/queue) without
// forming an import cycle. Implemented by *pkg/katalog.Katalog.
type Katalog interface {
	// EvaluateQueueBehaviourConditions completes queue behaviour evaluation for a CRD.
	// Called by the informer when the workqueue's NeedsBehaviourEval() is true — meaning
	// the workqueue detected a depth/threshold condition but deferred the when/or evaluation
	// because it requires the full preReconcile resolver context.
	// Returns true to enqueue, false to drop.
	EvaluateQueueBehaviourConditions(ctx context.Context, gvkString string, obj Object, opts EvaluateOptions) bool

	// EvaluateEnqueueFilter evaluates preReconcile.enqueueGate conditions for the named CRD.
	// Returns true when the object should be enqueued, false when it should be dropped.
	EvaluateEnqueueFilter(ctx context.Context, gvkString string, obj Object, cs kubernetes.Interface, opts EvaluateOptions) bool

	// EvaluateWatchEnqueueFilter evaluates a watch entry's enqueueGate.
	EvaluateWatchEnqueueFilter(ctx context.Context, primaryGVK, secondaryGVK string, obj Object, cs kubernetes.Interface, opts EvaluateOptions) bool

	// EvaluateEventEnqueueFilter evaluates an event entry's enqueueGate.
	EvaluateEventEnqueueFilter(ctx context.Context, primaryGVK, eventName string, obj Object, cs kubernetes.Interface, opts EvaluateOptions) bool

	// EvaluatePreReconcile evaluates preReconcile.reconcileGate conditions for the named CRD.
	// Returns (true, "") when conditions pass and the reconciler should run.
	// Returns (false, reason) when gated — reconciler must not be called.
	EvaluatePreReconcile(ctx context.Context, gvk string, obj *unstructured.Unstructured, cs kubernetes.Interface, opts EvaluateOptions) (allowed bool, reason string)

	// IsEventAware reports whether the named CRD has opted into event-aware
	// reconcileGate evaluation.
	//
	// When true, events entering the CRD's workqueue must retain their individual
	// event identity rather than being coalesced with other events for the same
	// object. This applies to the entire reconcileGate evaluation, not only
	// sentinel conditions.
	IsEventAware(obj Object, gvkString string) bool

	// GetPreReconcileSentinels returns the sentinel names declared by
	// preReconcile.sentinels for the named CRD.
	//
	// The informer uses this declaration to compute event-time sentinel values
	// from old and new objects. Sentinel declaration is owned by the Katalog;
	// the informer does not maintain a separate sentinel configuration registry.
	// Returns nil when the CRD is unknown or declares no sentinels.
	GetPreReconcileSentinels(obj Object, gvkString string) []string

	// ResolveGVR resolves a ManagedResource into a concrete GroupVersionResource.
	ResolveGVR(r ManagedResource) (schema.GroupVersionResource, bool)

	// CRD name lookups — resolve a GVK/GVR/kind/target string to the katalog CRD entry name.
	GetNameByGVKString(gvkString string) string
	GetNameByGVRString(gvrString string) string
	GetNameByKind(kind string) string
	GetNameByTarget(target string) string
}
