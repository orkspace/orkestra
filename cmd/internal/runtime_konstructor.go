// cmd/internal/runtime_konstructor.go
//
// konstructRuntime — the complete Orkestra runtime registry.
//
// This file is the single place where all runtime komponents are assembled.
// It is the equivalent of a dependency injection container — every komponent
// is created here, every dependency is threaded here, and nothing is started
// here. Starting happens in orkestra.Start() in declaration order.
//
// ── Architecture overview ─────────────────────────────────────────────────
//
//   Katalog (YAML)
//       │
//       ▼
//   merger → katalog.Katalog          One Katalog per operator binary.
//       │                             Holds all CRD declarations, reconciler
//       │                             configs, validation/mutation rules.
//       │
//       ▼
//   kubeclient.Kubeclient             REST config, dynamic client, typed
//       │                             clientset. Started first — everything else
//       │                             needs it.
//       │
//       ├──► ClientProvider           One REST client constructor per CRD.
//       │                             Deferred — constructed on first use.
//       │
//       ├──► SharedInformerFactory    One SharedIndexInformer per CRD.
//       │        │                    Starts watching the API server on Start().
//       │        │                    Routes watch events into per-CRD workqueues.
//       │        │
//       │        └──► per-CRD informer (cache.SharedIndexInformer)
//       │                 Holds all CR instances in memory.
//       │                 Zero API calls for reads after initial sync.
//       │
//       ├──► ProviderRegistry         AWS, MongoDB, Stripe — external infra providers.
//       │                             Registered before factory closures so all
//       │                             reconcilers share the same registry.
//       │
//       ├──► ResourceKatalog          Maps GVK → (CRD, informer, reconcilerFactory).
//       │    (ktrlRegistry)           Also implements KatalogRegistry for cross-CRD
//       │                             observation via GetInformerByName.
//       │
//       ├──► per-CRD reconciler factory closure
//       │        Captures: crdInfo, infCopy, ev, kube, anyHooks, newObj,
//       │                  providerRegistry, ktrlRegistry
//       │        Called by startCRDWorkers after orkestra.Start().
//       │        Returns a *GenericReconciler[T] ready to process items.
//       │
//       ├──► DependencyKordinator     Starts CRD workers in topological order.
//       │                             Waits for dependencies to meet their declared
//       │                             condition (started | healthy) before starting
//       │                             dependent workers.
//       │
//       └──► HealthServer             HTTP server for health, Katalog API, and
//                                     Control Center. Routes registered before Start().
//
// ── Reconcile loop (per CR item dequeued) ────────────────────────────────
//
//   workqueue.Get(key)
//       │
//       ▼
//   GenericReconciler.Reconcile(ctx, key)
//       │
//       ├── informer.GetIndexer().GetByKey(key)   (in-memory, zero API call)
//       │
//       ├── ensureFinalizers / ensureManagedLabel / ensureManagedAnnotations
//       │
//       ├── handleDeletion  (if DeletionTimestamp set)
//       │     └── runTemplateOnDelete → provider.Delete → removeFinalizers
//       │
//       └── reconcileImpl
//             ├── mutation  (apply defaults)
//             ├── validation (deny violations halt, warn violations log)
//             │
//             ├── OnReconcile hook (Go typed hook, if registered)
//             │
//             └── runTemplateReconcile  (declarative path)
//                   ├── 1. NewResolver(obj)           .spec.*, .status.*, .metadata.*
//                   ├── 2. readCross(decls)           .cross.<kind>.status.*
//                   │         └── katalogRegistry.GetInformerByName(kind)
//                   │               └── informer.GetIndexer().GetByKey(key)
//                   │                     zero API calls for same-binary CRDs
//                   ├── 3. runExternal(calls)         .external.<n>.status, .body
//                   │         └── http.Do(req) per call, sequential
//                   ├── 4. forEach expansion           N sources → N reconciles
//                   ├── 5. runResourceGroup(onCreate)
//                   │         runDeployments, runServices, runSecrets (once:),
//                   │         runConfigMaps, runServiceAccounts, runJobs, runCronJobs
//                   ├── 6. runResourceGroup(onReconcile) — same, update=true
//                   └── 7. runProviders(blocks)        aws:, mongodb:, stripe:
//                               └── provider.Reconcile(ctx, req) per block
//
//   After reconcileImpl:
//       patchStatusWithChildren(ctx, obj, err)
//           ├── ReadChildren → .children.*  (API server, parallel, RV="0")
//           ├── resolveStatusFields(when:, or:, template expressions)
//           └── PATCH /status

package internal

import (
	"context"
	"strings"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/event"
	"github.com/orkspace/orkestra/pkg/health"
	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/pipeline"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/merger"
	ork "github.com/orkspace/orkestra/pkg/orkestra"
	"github.com/orkspace/orkestra/pkg/runtime/informer"
	"github.com/orkspace/orkestra/pkg/runtime/informer/observe"
	"github.com/orkspace/orkestra/pkg/runtime/kordinator"
	"github.com/orkspace/orkestra/pkg/runtime/queue"
	"github.com/orkspace/orkestra/pkg/runtime/reconciler"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/client-go/tools/cache"
)

// runtimeKfg is the assembled runtime — returned to runtime.go so it can call
// orkestra.Start(ctx) and block until shutdown.
type runtimeKfg struct {
	konfig   *konfig.Konfig
	katalog  *katalog.Katalog
	komp     *[]domain.Komponent
	event    *event.Event
	kube     *kubeclient.Kubeclient
	kord     *kordinator.DependencyKordinator
	orkestra *ork.Orkestra
}

// konstructRuntime wires the entire Orkestra runtime.
//
// Nothing is started here. Every component is constructed and threaded together
// as closures and pointers. orkestra.Start() calls komponent.Start() in
// registration order, and komponent.Stop() in reverse order on shutdown.
//
// The method is intentionally long — this is the one place where all wiring
// is visible. Splitting it would scatter the dependency graph across files
// and make it harder to reason about startup order.
func konstructRuntime(kfg *konfig.Konfig, m *merger.Merger, ctx context.Context) *runtimeKfg {

	// ── 1a. Instance ────────────────────────────────────────────────────────────
	kfg.SetInstance(konfig.Runtime())

	// ── 1b. Katalog ────────────────────────────────────────────────────────────
	// Loads and validates the YAML Katalog. After this point, kat.Enabled()
	// returns only CRDs that passed schema validation and are not disabled.
	// Invalid CRDs are logged and excluded — they do not block the operator.
	kat := pipeline.NewKatalog(kfg, m)

	if registryURL := kfg.RegistryConfig().RegistryURL; registryURL != "" {
		m.SetRegistryURL(registryURL)
		logger.Info().Str("registry", registryURL).Msg("registry URL configured from ORK_REGISTRY")
	}

	// ── 2. Scheme ─────────────────────────────────────────────────────────────
	// Each CRD type (e.g. *PipelineList) must be registered with the scheme so
	// the REST client knows how to decode API server responses. For dynamic CRDs
	// (unstructured mode), this is a no-op — they use the dynamic client.
	scheme, err := katalog.NewSchemeRegistry(kat)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to build scheme registry")
	}

	// ── 3. Core komponents ────────────────────────────────────────────────────
	// Created here, started later by orkestra in registration order.

	kube := kubeclient.NewKubeclient(kfg, scheme)
	cs := kube.Clientset()

	// kubeclient is started immediately — the informer factory's missing-CRD
	// check needs the REST config during construction, before orkestra.Start().
	if err := kube.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to start kubeclient")
	}

	// HealthServer — HTTP-only (health, readiness, metrics, Katalog API routes).
	// Routes registered below before Start() binds the port.
	hs := health.NewHealthServer(kfg)

	// Event recorder — surfaces notable state changes to the Kubernetes event stream
	// (visible via kubectl describe). Shared by all controllers that emit events.
	ev := event.NewEvent(kube)

	// Default work queue — rate-limiting reconcile queue shared by controllers that
	// do not need a dedicated queue. Bounded to prevent runaway reconcile storms.
	defaultWq := queue.NewWorkqueue("default workqueue")

	// Queue registry — maps GVK strings to dedicated work queues. Controllers
	// register here so that cross-controller enqueues are dispatched correctly.
	queueRegistry := queue.NewQueueRegistry()

	// ── 4a. REST client provider ──────────────────────────────────────────────
	// Associates each CRD type with a constructor that builds a typed REST
	// client. The constructor is deferred — called on first informer use.
	// Dynamic CRDs skip this — they use the dynamic client directly.
	provider := kube.NewClientProvider()

	for _, crd := range kat.Enabled() {
		crd := crd
		if crd.IsDynamic() {
			continue
		}
		object, list := crd.GetRuntimeObjects()
		logger.Debug().Str("gvk", crd.GVKString()).Msg("registering CRD client provider")

		provider.Register(object, func(k *kubeclient.Kubeclient) (informer.GenericClient, error) {
			return k.NewClient(list, kubeclient.CRDInfo{
				Kind:          crd.APITypes.Kind,
				Group:         crd.APITypes.Group,
				Version:       crd.APITypes.Version,
				APIPath:       crd.APITypes.APIPath,
				GroupVersion:  crd.GroupVersion,
				Plural:        crd.APITypes.Plural,
				Namespace:     crd.Namespace,
				Namespaced:    crd.IsNamespaced(),
				ForceConflict: crd.ResolveForceConflict(),
			})
		})
	}

	// ── 4b. Shared informer factory ───────────────────────────────────────────
	// Creates one SharedIndexInformer per CRD. On Start(), each informer opens
	// a watch against the API server and populates its in-memory cache.
	// Watch events are routed into per-CRD workqueues via handleEvent.
	infFactory := informer.SharedInformerFactory(kube.RestConfig(), informer.FactoryOptions{
		Provider:      provider,
		Scheme:        scheme,
		DefaultWq:     defaultWq,
		Konfig:        kfg,
		Katalog:       kat,
		ClientSet:     cs,
		QueueRegistry: queueRegistry,
	})

	// ── 4c. Provider registry ─────────────────────────────────────────────────
	// External infrastructure providers (AWS, MongoDB, etc.).
	// Must be built BEFORE the factory loop so all reconciler closures capture
	// the same fully-initialised registry. loadProviders is non-fatal —
	// unavailable providers log a warning and the operator starts regardless.
	providerRegistry := loadProviders(ctx, kat)

	// One ProviderStats per CRD — shared between GenericReconciler (writes on each
	// provider call) and BuildCRDInfoHandler (reads for the /katalog/{crd} response).
	// Only created for CRDs that declare provider blocks — others get nil.
	providerStatsMap := make(map[string]*health.ProviderStats)
	for _, crd := range kat.Enabled() {
		if crd.HasProviders() {
			providerStatsMap[crd.GVKString()] = health.NewProviderStats()
		}
	}

	// ── 4d. Kordinator registry + per-CRD wiring ──────────────────────────────
	// ktrlRegistry maps GVK → (CRDEntry, SharedIndexInformer, ReconcilerFactory).
	// It also implements reconciler.KatalogRegistry via GetInformerByName,
	// enabling cross-CRD observation with zero API server calls.
	ktrlRegistry := kordinator.NewKordinatorRegistry()

	// ── 4e. CRD health map ────────────────────────────────────────────────────
	// One CRDHealth per CRD — shared between the DependencyKordinator
	// (which updates it on each reconcile) and the HTTP health routes
	// (which read it on each request). All three reference the same pointers.
	crdHealthMap := make(map[string]*kordinator.CRDHealth)
	for _, crd := range kat.Enabled() {
		gvk := crd.GVKString()
		crdHealthMap[gvk] = kordinator.NewCRDHealth(crd.Name)
	}

	logger.Debug().Msg("wiring CRDs into kordinator registry...")

	finalizers := kfg.Finalizers()
	for _, crd := range kat.Enabled() {
		crd := crd
		gvk := crd.GVKString()
		object, _ := crd.GetRuntimeObjects()

		wq := queueRegistry.Register(gvk, crd.QueueConfig())

		// compute selectors
		labelSelector := orktypes.Labels(crd.LabelSelector).String()
		fieldSelector := orktypes.Labels(crd.FieldSelector).String()

		opts := informer.Options{
			Name:          crd.APITypes.Kind,
			Resync:        crd.OperatorBox.Reconciler.Resync.Duration,
			LabelSelector: labelSelector,
			FieldSelector: fieldSelector,
		}
		if crd.SharedQueue() {
			opts.Wq = nil // use the shared default queue
		} else {
			opts.Wq = wq
		}

		// ── Namespace filter — Tier 1 (scope ListerWatcher) + Tier 2 (pre-enqueue) ──
		// Tier 2 is always registered when namespace rules exist.
		// Tier 1 scopes the ListerWatcher to a single namespace when allowedNamespaces
		// has exactly one entry — the informer never sees events from other namespaces.
		if crd.HasNamespaceRules() {
			if crd.IsSingleNamespace() {
				opts.Namespace = crd.SingleNamespace()
				logger.Debug().
					Str("crd", crd.APITypes.Kind).
					Str("namespace", opts.Namespace).
					Msg("informer: namespace-scoped watch (Tier 1)")
			}
			filter := &informer.NamespaceFilter{
				AllowedNamespaces:    []string(crd.AllowedNamespaces),
				RestrictedNamespaces: []string(crd.RestrictedNamespaces),
			}
			infFactory.RegisterNamespaceFilter(gvk, filter)
			logger.Debug().
				Str("crd", crd.APITypes.Kind).
				Str("filter", informer.NamespaceFilterSummary(filter)).
				Msg("informer: namespace filter registered (Tier 2)")
		}

		// Choose typed or dynamic informer.
		// Dynamic CRDs use *unstructured.Unstructured — no Go type needed.
		// Typed CRDs use the registered concrete Go type for type-safe access.
		var inf cache.SharedIndexInformer

		// For dynamic CRDs, use opts.Namespace from the Tier 1 filter when set;
		// otherwise fall back to crd.Namespace (operator-level namespace setting).
		dynNamespace := crd.Namespace
		if opts.Namespace != "" {
			dynNamespace = opts.Namespace
		}

		logger.Debug().
			Bool("dynamic:", crd.IsDynamic()).
			Msgf("[DEBUG] CRD %s: location = %q\n", crd.APITypes.Kind, crd.APITypes.Location)
		if crd.IsDynamic() {
			lw := kube.NewDynamicListerWatcher(kubeclient.CRDInfo{
				Kind:          crd.APITypes.Kind,
				Group:         crd.APITypes.Group,
				Version:       crd.APITypes.Version,
				APIPath:       crd.APITypes.APIPath,
				GroupVersion:  crd.GroupVersion,
				Plural:        crd.APITypes.Plural,
				Namespace:     dynNamespace,
				Namespaced:    crd.IsNamespaced(),
				ForceConflict: crd.ResolveForceConflict(),
			}, kubeclient.ListOptions{
				LabelSelector: labelSelector,
				FieldSelector: fieldSelector,
			})
			inf = infFactory.ForListerWatcher(lw, object, ctx, opts)
		} else {
			inf = infFactory.For(object, ctx, opts)
		}

		finalizers = append(finalizers, crd.OperatorBox.Finalizers...)

		infCopy := inf

		// Build the reconciler factory.
		// For default: true CRDs — GenericReconciler interprets the Katalog declaratively.
		// For default: false CRDs — a custom Constructor is required.
		//
		// The factory is a closure — it captures all values at construction time
		// and is called by startCRDWorkers after informers are synced.
		// Each call returns a fresh reconciler instance for one worker goroutine.
		var factory func() domain.Reconciler

		if crd.DefaultReconcile() {
			objCopy := object

			var anyHooks domain.AnyReconcileHooks
			if crd.OperatorBox.HookFactory != nil {
				anyHooks = crd.OperatorBox.HookFactory()
			}

			logger.Debug().Str("gvk", gvk).Msg("wiring GenericReconciler factory")

			// Attach hooks.args to a copy of the kube client; hooks read them via kube.Args().
			var hookKube kubeclient.Interface = kube.
				WithForceConflict(crd.ResolveForceConflict())
			if args := crd.HooksArgs(); len(args) > 0 {
				hookKube = kube.WithArgs(kubeclient.Args(args))
			}

			pStats := providerStatsMap[gvk]
			factory = func() domain.Reconciler {
				return reconciler.NewGenericReconciler(
					crd,
					infCopy,
					ev,
					hookKube,
					anyHooks,
					func() domain.Object {
						return objCopy.DeepCopyObject().(domain.Object)
					},
					ktrlRegistry,     // cross-CRD informer lookup via GetInformerByName
					crdHealthMap,     // cross-CRD health map via HealthProvider
					providerRegistry, // aws:, mongodb:, etc. block dispatch
					pStats,           // per-CRD provider error rate tracking
					kat,              // Katalog for notification wiring
				)
			}
		} else {
			if !crd.ConstructorEnabled() {
				logger.Fatal().
					Str("gvk", gvk).
					Msg("reconciler.default is false but no Constructor provided")
			}

			logger.Debug().Str("gvk", gvk).Msg("wiring custom reconciler factory")

			// Attach constructor.args, informer, and event recorder to a copy of the
			// kube client. Constructor authors access them via kube.GetInformer() etc.
			var ctorKube kubeclient.Interface = kube.
				WithInformer(infCopy).
				WithEventRecorder(ev).
				WithStoreFor(infFactory.StoreFor).
				WithIndexerFor(infFactory.IndexerFor).
				WithForceConflict(crd.ResolveForceConflict())
			if args := crd.ConstructorArgs(); len(args) > 0 {
				ctorKube = ctorKube.WithArgs(kubeclient.Args(args))
			}

			factory = func() domain.Reconciler {
				return crd.OperatorBox.Constructor(ctorKube)
			}
		}

		// Wrap with MuxReconciler when per-target constructors are declared.
		// MuxReconciler dispatches each reconcile cycle to the matching target's
		// domain.Reconciler, falling back to the base factory for CRs with no
		// annotation or an unrecognised target name.
		if crd.HasTargetConstructorFactories() {
			baseFactory := factory
			crdCopy := crd
			factory = func() domain.Reconciler {
				targets := make(map[string]domain.Reconciler, len(crdCopy.TargetReconcilerFactories))
				for targetName, ctor := range crdCopy.TargetReconcilerFactories {
					var targetKube kubeclient.Interface = kube.
						WithInformer(infCopy).
						WithEventRecorder(ev).
						WithStoreFor(infFactory.StoreFor).
						WithIndexerFor(infFactory.IndexerFor).
						WithForceConflict(crdCopy.ResolveForceConflict())
					if args := crdCopy.TargetConstructorArgs(targetName); len(args) > 0 {
						targetKube = targetKube.WithArgs(kubeclient.Args(args))
					}
					targets[targetName] = ctor(targetKube)
				}
				return orktarget.NewMuxReconciler(infCopy, targets, baseFactory())
			}
			logger.Debug().
				Str("gvk", gvk).
				Int("targets", len(crd.TargetReconcilerFactories)).
				Msg("wiring MuxReconciler factory")
		}

		// Register informs the DependencyKordinator which informer and factory
		// belong to this CRD. Workers are not started yet — that happens in Start().
		ktrlRegistry.Register(gvk, crd, inf, factory)
		logger.Debug().Str("gvk", gvk).Msg("CRD registered")
	}

	// ── 5. HTTP routes ───────────────────────────────────────────────────────
	// All routes registered before hs.Start() — the mux is shared.
	//
	// Per-CRD routes:
	//   /katalog/{crd}/health       		→ 200 healthy, 503 degraded
	//   /katalog/{crd}              		→ CRD config + live reconcile stats
	//	 /katalog/{crd}/raw			 		→ the user's config
	//   /katalog/{crd}/enriched	 		→ the runtime config
	//   /katalog/{crd}/cr           		→ all CR instances (informer cache, <1ms)
	//   /katalog/{crd}/cr/{ns}/{n}  		→ CR detail + children (watch cache, <50ms)
	//   /katalog/{crd}/cr/{...}/events 	→ recent events (watch cache, <50ms)
	//
	// Aggregate:
	//	 /katalog/raw				 		→ the user's katalog config
	//	 /katalog/enriched				 	→ the runtime katalog config
	//   /katalog                    		→ all CRDs, dependency graph, health summary
	orkHealth := kordinator.NewOrkestraHealth()

	for _, crd := range kat.Enabled() {
		gvk := crd.GVKString()
		crdHealth := crdHealthMap[gvk]
		crdName := strings.ToLower(crd.Name)

		entry, _ := ktrlRegistry.Get(gvk)
		inf := entry.Informer

		if !crd.IsEnabledAllEndpoints() {
			continue
		}

		if crd.IsHealthEnabled() {
			hs.Register(
				"/katalog/"+crdName+"/health",
				kordinator.BuildCRDHealthHandler(crd, kfg, inf, crdHealth, orkHealth),
			)
		}

		if crd.IsInfoEnabled() {
			hs.Register(
				"/katalog/"+crdName,
				kordinator.BuildCRDInfoHandler(
					crd, kfg, inf, crdHealth,
					orkHealth,
					providerStatsMap[gvk],
				),
			)
			hs.Register(
				"/katalog/"+crdName+"/cr",
				kordinator.BuildCRListHandler(crd, inf, orkHealth),
			)
			hs.Register(
				"/katalog/"+crdName+"/cr/",
				kordinator.BuildCRDetailAndEventsHandler(crd, inf, kube, crd.OperatorBox, orkHealth),
			)
		}

		// Register raw and enriched CRD definition endpoint
		hs.Register(
			"/katalog/"+crdName+"/raw",
			kordinator.BuildCRDRawHandler(m, crd.Name),
		)
		hs.Register(
			"/katalog/"+crdName+"/enriched",
			kordinator.BuildCRDEnrichedHandler(kat, crd.Name),
		)

		logger.Debug().
			Str("health", "/katalog/"+crdName+"/health").
			Str("info", "/katalog/"+crdName).
			Str("raw", "/katalog/"+crdName+"/raw").
			Str("enriched", "/katalog/"+crdName+"/enriched").
			Msg("registered CRD routes")
	}

	hs.Register("/katalog/raw", kordinator.BuildRawKatalogHandler(m))
	hs.Register("/katalog/enriched", kordinator.BuildEnrichedKatalogHandler(kat))
	hs.Register("/katalog", kordinator.BuildKatalogHandler(kat, kfg, ktrlRegistry, crdHealthMap, orkHealth))

	// ── 6a. Secondary resource observers ────────────────────────────
	// Observe secondary resources declared in operatorBox.watch/events.
	obs := observe.New(observe.Dependencies{
		Kube:          kube,
		Informer:      infFactory,
		QueueRegistry: queueRegistry,
		Katalog:       kat,
	})

	// ── 6b. Dependency kordinator ──────────────────────────────────────────────
	// Starts CRD workers in topological order defined by the dependency graph.
	// For each CRD, waits until all declared dependsOn CRDs meet their
	// condition (started | healthy) before calling factory() and starting workers.
	//
	// Worker lifecycle:
	//   Start()  → wait for informer sync → call factory() per worker → run loop
	//   Shutdown → drain queue → stop workers → remove from active set
	kord := kordinator.NewDependencyKordinator(
		kube,
		infFactory,
		obs,
		ktrlRegistry,
		kat,
		ev,
		hs,
		queueRegistry,
		defaultWq,
		crdHealthMap,
		orkHealth,
		kfg.Katalog().DefaultWorkers(),
		katalog.NewDependencyGraph(kat),
		kfg.Katalog().ShutdownTimeout(),
	)

	// ── 7. Komponent list ─────────────────────────────────────────────────────
	// Start order: each komponent must start after its dependencies.
	// Stop order: reverse of start order (automatic).
	//
	// HealthServer starts first so it can serve /ready during startup.
	// Kubeclient is already started above but is still registered so
	// orkestra manages its Stop().
	komponents := []domain.Komponent{
		hs,            // 1. HTTP server — /ready, /livez, /katalog routes
		kube,          // 2. REST clients — already started, managed for Stop()
		ev,            // 3. event recorder — depends on kube
		queueRegistry, // 4. per-CRD bounded queues
		defaultWq,     // 5. default unbounded queue
		infFactory,    // 6. informer factory — starts watchers, closes ready channel
		kord,          // 7. dependency kordinator — starts workers in topo order
	}

	// suppress unused variable warning — finalizers is populated but only
	// referenced by the reconciler closures captured above.
	_ = finalizers

	// ── 8. Orkestra ───────────────────────────────────────────────────────────
	// The supervisor. Calls Start() on each komponent in order.
	// On OS signal (SIGTERM/SIGINT) or fatal error, calls Stop() in reverse.
	// Graceful shutdown: drains queues before stopping workers.
	o := ork.NewOrkestra(
		kfg.RunningInstance(),
		kfg.Katalog().ShutdownGracePeriod(),
		kfg.Ork().LogLevel(),
	)
	o.Register(komponents)

	return &runtimeKfg{
		konfig:   kfg,
		katalog:  kat,
		komp:     &komponents,
		event:    ev,
		kube:     kube,
		kord:     kord,
		orkestra: o,
	}
}
