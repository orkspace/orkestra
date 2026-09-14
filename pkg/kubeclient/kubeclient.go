// pkg/kubeclient/kubeclient.go
package kubeclient

import (
	"context"
	"fmt"
	"sync/atomic"

	"errors"
	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/utils"
	apiextclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// Kubeclient defines what a kube client is
type Kubeclient struct {
	name       string
	restConfig *rest.Config
	clientset  kubernetes.Interface
	dynamic    dynamic.Interface
	apiext     apiextclientset.Interface
	Info       *CRDInfo
	started    *atomic.Bool
	mapper     meta.RESTMapper

	// Starter konfig
	konfig *konfig.Konfig
	scheme *runtime.Scheme

	// rawArgs holds the template declarations from hooks.args or constructor.args.
	// String values may contain {{ }} expressions resolved per-CR by ScopedFor.
	rawArgs map[string]interface{}
	// args holds the fully-evaluated args for the current reconcile scope.
	// Set by ScopedFor; nil means fall back to rawArgs as-is.
	args Args

	// informer is the primary CRD's SharedIndexInformer, injected by the runtime
	// before the constructor is called. Accessible via GetInformer().
	informer cache.SharedIndexInformer
	// eventRecorder is the event recorder for this CRD, injected by the runtime
	// before the constructor is called. Accessible via GetEventRecorder().
	eventRecorder EventRecorder
	// storeFor is a closure that returns the informer store for a GVK, injected by
	// the runtime so ToClient can serve cached reads. Nil when not wired.
	storeFor func(schema.GroupVersionKind) cache.Store
	// indexerFor is a closure that returns the cache.Indexer for a GVK, injected by
	// the runtime so ToClient can use ByIndex for field-selector queries. Nil when not wired.
	indexerFor func(schema.GroupVersionKind) cache.Indexer

	// Testing
	FakeClientset kubernetes.Interface
}

// Compile check — *Kubeclient must satisfy this.
var _ Interface = (*Kubeclient)(nil)

func (k *Kubeclient) RESTMapper() meta.RESTMapper {
	return k.mapper
}

// RefreshMapper forces the deferred mapper to refresh its discovery cache.
// Call this after creating or updating CRDs so RESTMapping will pick them up.
func (k *Kubeclient) RefreshMapper() {
	if k.mapper == nil {
		return
	}
	if dm, ok := k.mapper.(*restmapper.DeferredDiscoveryRESTMapper); ok {
		dm.Reset()
	}
}

// Implementing yhe Komponent interface
var _ domain.Komponent = (*Kubeclient)(nil)

// -----------------------------------------------------------------------------
// Entry point
// -----------------------------------------------------------------------------
// NewKubeclient returns a new Kubeclient with the correct scheme
func NewKubeclient(kfg *konfig.Konfig, scheme *runtime.Scheme) *Kubeclient {
	if scheme == nil {
		utils.Exit(errors.New("scheme cannot be nil"))
	}

	return &Kubeclient{
		name:    "kubeclient",
		scheme:  scheme,
		konfig:  kfg,
		started: new(atomic.Bool),
	}
}

// Start is called by orkestra.Start() to start kube client
func (k *Kubeclient) Start(ctx context.Context) error {
	cfg, err := k.buildConfig()
	if err != nil {
		return err
	}

	// Store config
	k.restConfig = cfg

	// Build core clientset
	logger.Debug().Msg("creating core clientset")
	k.clientset, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("kubeclient -- failed to create clientset: %w", err)
	}

	// Build dynamic client
	logger.Debug().Msg("creating dynamic client")
	k.dynamic, err = dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("kubeclient -- failed to create dynamic client: %w", err)
	}

	// Build apiextensions clientset (for CRD patching)
	logger.Debug().Msg("creating apiextensions clientset")
	k.apiext, err = apiextclientset.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("kubeclient -- failed to create apiextensions clientset: %w", err)
	}

	// Build a cached discovery client and deferred RESTMapper
	logger.Debug().Msg("creating discovery client and RESTMapper")
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return fmt.Errorf("kubeclient -- failed to create discovery client: %w", err)
	}
	// memory cache for discovery
	cached := memory.NewMemCacheClient(dc)
	// deferred mapper that lazily queries discovery and caches mappings
	k.mapper = restmapper.NewDeferredDiscoveryRESTMapper(cached)

	k.started.Store(true)
	return nil
}

// buildConfig returns a *rest config or nil in error
func (k *Kubeclient) buildConfig() (*rest.Config, error) {
	if k.restConfig != nil {
		return k.restConfig, nil
	}

	if k.scheme == nil {
		return nil, errors.New("scheme cannot be nil")
	}

	var restCfg *rest.Config
	var err error

	if k.konfig.Cluster().KubekonfigPath() != "" {
		logger.Debug().Msg("using kubeconfig")
		restCfg, err = clientcmd.BuildConfigFromFlags(k.konfig.Cluster().MasterURL(), k.konfig.Cluster().KubekonfigPath())
	} else {
		logger.Debug().Msg("using incluster configuration")
		restCfg, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, err
	}

	return restCfg, nil
}

// On-demand rest client
func (k *Kubeclient) RestClientFor(apiPath, group, version string) (*rest.RESTClient, error) {
	return k.SharedClientFactory(apiPath, group, version)
}

// On-demand dynamic client
func (k *Kubeclient) DynamicClientFor(apiPath, group, version string) (dynamic.Interface, error) {
	return k.dynamic, nil
}

// Notes
// Why merge patch and not strategic merge patch or Update? Three reasons:
// 1. First, you only touch metadata.finalizers — the rest of the object is untouched,
// which avoids resourceVersion conflicts if the object was updated between your cache read and this call.
// 2. Second, strategic merge patch requires the object type to be registered and understood by the API server's
// strategy engine — dynamic clients should use merge patch.
// 3. Third, Update sends the full object and requires a current resourceVersion — more fragile, more data over the wire.

// Started is called by orkestra for healthcheck
func (k *Kubeclient) Started() bool { return k.started.Load() }

// Shutdown is called by orkestra fir graceful shutdown
func (k *Kubeclient) Shutdown(ctx context.Context) {}

// Name returns the name of the kubeclient
func (k *Kubeclient) Name() string { return k.name }

// RestConfig returns the rest confif for the kube client
func (k *Kubeclient) RestConfig() *rest.Config { return k.restConfig }

// Clientset returns the kubernetes interface
func (k *Kubeclient) Clientset() kubernetes.Interface { return k.clientset }

// Dynamic returns yhe dynamic interface. Useful in 'dynamic' reconciler mode
func (k *Kubeclient) DynamicClient() dynamic.Interface { return k.dynamic }

// Scheme returns the runtime scheme for yhe kubeclient
func (k *Kubeclient) Scheme() *runtime.Scheme { return k.scheme }

// ApiextensionsClient returns the apiextensions clientset for CRD operations.
func (k *Kubeclient) ApiextensionsClient() apiextclientset.Interface { return k.apiext }

// New fake client
func NewFakeClientset() kubernetes.Interface {
	return fake.NewClientset()
}

// Args returns the resolved args for the current reconcile scope.
// If ScopedFor has been called, returns the per-CR evaluated args.
// Otherwise returns the raw (unevaluated) args from the Katalog declaration.
func (k *Kubeclient) Args() Args {
	if k.args != nil {
		return k.args
	}
	if k.rawArgs != nil {
		return Args(k.rawArgs)
	}
	return Args{}
}

// WithArgs returns a shallow copy of the Kubeclient with the given args stored
// as rawArgs. Used by the runtime to attach katalog-declared args before a hook
// or constructor is called; ScopedFor then evaluates templates at reconcile time.
func (k *Kubeclient) WithArgs(args Args) Interface {
	cp := *k
	cp.rawArgs = map[string]interface{}(args)
	cp.args = nil
	return &cp
}

// ScopedFor evaluates template expressions in rawArgs using eval and returns a
// copy with the resolved args attached. Called by GenericReconciler after building
// the per-CR resolver so hook authors see evaluated args automatically.
func (k *Kubeclient) ScopedFor(eval func(string) (string, bool)) Interface {
	cp := *k
	if len(k.rawArgs) == 0 {
		return &cp
	}
	cp.args = ResolveArgsMap(k.rawArgs, eval)
	return &cp
}

// WithInformer returns a copy of this Interface with the primary CRD informer
// attached. Called by the runtime before invoking a constructor function.
func (k *Kubeclient) WithInformer(inf cache.SharedIndexInformer) Interface {
	cp := *k
	cp.informer = inf
	return &cp
}

// WithEventRecorder returns a copy of this Interface with the event recorder
// attached. Called by the runtime before invoking a constructor function.
func (k *Kubeclient) WithEventRecorder(ev EventRecorder) Interface {
	cp := *k
	cp.eventRecorder = ev
	return &cp
}

// GetInformer returns the primary CRD's SharedIndexInformer.
// Available inside constructor functions — nil if called outside that context.
func (k *Kubeclient) GetInformer() cache.SharedIndexInformer {
	return k.informer
}

// GetEventRecorder returns the event recorder for this CRD.
// Available inside constructor functions — nil if called outside that context.
func (k *Kubeclient) GetEventRecorder() EventRecorder {
	return k.eventRecorder
}

// WithStoreFor returns a copy of this Interface with the given store-lookup
// closure attached. Called by the runtime before invoking a constructor so that
// ToClient can serve cached reads for informer-backed types.
func (k *Kubeclient) WithStoreFor(fn func(schema.GroupVersionKind) cache.Store) Interface {
	cp := *k
	cp.storeFor = fn
	return &cp
}

// GetStoreFor returns the store-lookup closure, or nil if none was attached.
func (k *Kubeclient) GetStoreFor() func(schema.GroupVersionKind) cache.Store {
	return k.storeFor
}

// WithIndexerFor returns a copy of this Interface with the given indexer-lookup
// closure attached. Called by the runtime before invoking a constructor so that
// ToClient can use ByIndex for field-selector queries.
func (k *Kubeclient) WithIndexerFor(fn func(schema.GroupVersionKind) cache.Indexer) Interface {
	cp := *k
	cp.indexerFor = fn
	return &cp
}

// GetIndexerFor returns the indexer-lookup closure, or nil if none was attached.
func (k *Kubeclient) GetIndexerFor() func(schema.GroupVersionKind) cache.Indexer {
	return k.indexerFor
}

// WithForceConflict returns a copy of this Interface with the CRD-level
// force-conflict attached.
func (k *Kubeclient) WithForceConflict(forceConflict *bool) Interface {
	cp := *k
	info := k.Info
	if info == nil {
		info = &CRDInfo{}
	} else {
		// Make a copy to avoid mutating the original
		copyInfo := *info
		info = &copyInfo
	}
	info.ForceConflict = forceConflict
	cp.Info = info
	return &cp
}

// ForceConflict returns the CRD-level force-conflict setting.
func (k *Kubeclient) ForceConflict() *bool {
	if k == nil || k.Info == nil {
		return nil
	}
	return k.Info.ForceConflict
}
