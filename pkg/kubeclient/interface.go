package kubeclient

import (
	"context"

	"github.com/orkspace/orkestra/domain"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// EventRecorder is a minimal interface for recording Kubernetes events.
// pkg/event.Recorder satisfies this — kubeclient does not import pkg/event
// directly to avoid an import cycle (pkg/event imports pkg/kubeclient).
type EventRecorder interface {
	Eventf(obj runtime.Object, eventType, reason, messageFmt string, args ...interface{})
}

// Interface is the interface every registry function depends on.
// *Kubeclient satisfies this with real clients.
// *simulate.FakeKubeclient satisfies this with k8s.io/client-go/kubernetes/fake.
type Interface interface {
	Clientset() kubernetes.Interface
	DynamicClient() dynamic.Interface
	RESTMapper() meta.RESTMapper
	RestConfig() *rest.Config
	Scheme() *runtime.Scheme

	// Args returns the args declared under hooks.args or constructor.args in katalog.yaml.
	// Returns an empty Args map when no args were declared.
	Args() Args

	// WithArgs returns a copy of this Interface with the given args attached.
	// Used by the runtime to inject per-CRD args before calling a hook or constructor.
	WithArgs(args Args) Interface

	// WithInformer returns a copy of this Interface with the primary CRD informer
	// attached. Called by the runtime before invoking a constructor function.
	WithInformer(inf cache.SharedIndexInformer) Interface

	// WithStoreFor attaches a closure that returns the informer store for a GVK.
	// Called by the runtime so ToClient can serve cached reads for registered types.
	// fn is evaluated lazily at client.Get/List time — it always reflects the live
	// factory state, including informers registered after this call.
	WithStoreFor(fn func(schema.GroupVersionKind) cache.Store) Interface

	// GetStoreFor returns the store-lookup closure, or nil if none was attached.
	GetStoreFor() func(schema.GroupVersionKind) cache.Store

	// WithIndexerFor attaches a closure that returns the cache.Indexer for a GVK.
	// Called by the runtime so ToClient can use ByIndex for field-selector queries.
	WithIndexerFor(fn func(schema.GroupVersionKind) cache.Indexer) Interface

	// GetIndexerFor returns the indexer-lookup closure, or nil if none was attached.
	GetIndexerFor() func(schema.GroupVersionKind) cache.Indexer

	// WithEventRecorder returns a copy of this Interface with the event recorder
	// attached. Called by the runtime before invoking a constructor function.
	WithEventRecorder(ev EventRecorder) Interface

	// GetInformer returns the primary CRD's SharedIndexInformer.
	// Available inside constructor functions — nil if called outside that context.
	GetInformer() cache.SharedIndexInformer

	// GetEventRecorder returns the event recorder for this CRD.
	// Available inside constructor functions — nil if called outside that context.
	GetEventRecorder() EventRecorder

	// ScopedFor evaluates any template expressions in the rawArgs using eval and
	// returns a copy of this Interface with the resolved args attached.
	// Called by GenericReconciler after building the resolver so hook authors see
	// fully-evaluated args without any extra wiring. Constructor authors call it
	// themselves using their own resolver.
	ScopedFor(eval func(string) (string, bool)) Interface

	// CRUD
	// List(ctx context.Context, obj domain.ObjectList, opts ...metav1.ListOptions) error

	// Get retrieves an object from the cluster by namespace and name into the provided domain.Object.
	Get(ctx context.Context, key domain.ObjectKey, into domain.Object, opts metav1.GetOptions) error

	// Create creates a new object in the cluster using the provided domain.Object.
	Create(ctx context.Context, obj domain.Object, opts metav1.CreateOptions) error

	// Update updates an existing object in the cluster with the provided domain.Object.
	Update(ctx context.Context, obj domain.Object, opts metav1.UpdateOptions) error

	// Patch applies a partial modification to an existing object using the provided Patch.
	Patch(ctx context.Context, obj domain.Object, patch Patch, opts metav1.PatchOptions) error

	// Delete removes an object from the cluster using the provided domain.Object.
	Delete(ctx context.Context, obj domain.Object, opts metav1.DeleteOptions) error

	// DeleteAllOf removes all objects matching the given list options using the provided domain.Object.
	DeleteAllOf(ctx context.Context, obj domain.Object, deleteOpts metav1.DeleteOptions, listOpts metav1.ListOptions) error

	// Apply performs a server-side apply operation using the provided ApplyConfiguration.
	Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts metav1.ApplyOptions) error

	// IsObjectNamespaced reports whether the object's resource is namespace-scoped.
	IsObjectNamespaced(obj runtime.Object) (bool, error)

	// GroupVersionKindFor resolves the object's GVK from the kubeclient scheme.
	GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error)

	// Patch helpers — used by the generic reconciler for finalizer, label,
	// annotation, and status updates. Implementations must be idempotent.
	PatchFinalizers(ctx context.Context, obj runtime.Object, finalizers []string, opts metav1.PatchOptions) error
	PatchLabels(ctx context.Context, obj runtime.Object, base, desired map[string]string, opts metav1.PatchOptions) error
	PatchAnnotations(ctx context.Context, obj runtime.Object, annotations map[string]string, opts metav1.PatchOptions) error
	PatchStatus(ctx context.Context, obj domain.Object, statusFields map[string]interface{}, opts metav1.PatchOptions) error
	PatchSpec(ctx context.Context, obj domain.Object, specFields map[string]interface{}, opts metav1.PatchOptions) error

	// Non-Infrastructure methods

	// WithForceConflict returns a copy of this Interface with the CRD-level
	// force-conflict attached. Called at construction time.
	WithForceConflict(forceConflict *bool) Interface
	// ForceConflict returns the CRD-level force-conflict setting.
	ForceConflict() *bool
}
