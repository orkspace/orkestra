# 03 — ListerWatch Construction

## Two paths to a ListerWatcher

Every informer needs a `cache.ListerWatcher` — an object that can `List` all existing resources and `Watch` for changes. Orkestra builds these two different ways depending on whether the CRD has a registered Go type.

```
CRD entry
    │
    ├── IsDynamic() == true
    │       kube.NewDynamicListerWatcher(CRDInfo, ListOptions)
    │       → DynamicListerWatcher (pkg/kubeclient/dynamic.go)
    │       Uses the dynamic client: dynamic.Resource(gvr).Namespace(ns).List/Watch
    │       Returns *unstructured.Unstructured objects — no Go type needed
    │       infFactory.ForListerWatcher(lw, obj, ctx, opts)
    │
    └── IsDynamic() == false
            infFactory.For(obj, ctx, opts)
            → Factory.newListWatch(obj, opts)
            → GenericClient (pkg/kubeclient/genericclient.go)
            Uses the typed REST client: restClient.Get().Namespace(ns).Resource(plural)
            Returns registered Go types — scheme-aware
```

## DynamicListerWatcher (dynamic path)

`DynamicListerWatcher` in `pkg/kubeclient/dynamic.go` holds:

```
DynamicListerWatcher
├── kube           — *Kubeclient (lazy: dynamic client resolved at call time)
├── gvr            — GroupVersionResource for the CRD
├── namespace      — "" = all namespaces (NamespaceAll), or a single namespace
├── namespaced     — false = cluster-scoped, skip Namespace() in builder chain
├── labelSelector  — injected into ListOptions at call time
└── fieldSelector  — injected into ListOptions at call time
```

Namespaced CRDs scope to `namespace` (or `NamespaceAll` when Empty(). Cluster-scoped CRDs omit the `Namespace()` call entirely:

```go
if d.namespaced {
    ns := d.namespace
    if ns == "" { ns = metav1.NamespaceAll }
    return d.kube.dynamic.Resource(d.gvr).Namespace(ns).List(ctx, options)
}
return d.kube.dynamic.Resource(d.gvr).List(ctx, options)
```

### Tier 1 scoping for dynamic CRDs

When the namespace filter's `IsSingleNamespace()` is true, `konstructRuntime` sets `dynNamespace = filter.SingleNamespace()` and passes it into `CRDInfo.Namespace`. The `DynamicListerWatcher` then scopes its `List`/`Watch` to that single namespace:

```go
dynNamespace := crd.Namespace           // operator-level setting
if opts.Namespace != "" {
    dynNamespace = opts.Namespace       // Tier 1 overrides
}
lw := kube.NewDynamicListerWatcher(kubeclient.CRDInfo{
    ...
    Namespace: dynNamespace,
    ...
}, ...)
```

## Factory.newListWatch (typed path)

`newListWatch` in `helper.go` builds a `cache.ListWatch` whose closures call the `GenericClient` interface. Both `List` and `Watch` block on `<-f.ready` — they will not open connections until `Factory.Start()` closes the ready channel.

```go
func (f *Factory) newListWatch(obj runtime.Object, opts Options) *cache.ListWatch {
    return &cache.ListWatch{
        ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
            <-f.ready
            // inject selectors from opts
            client, err := f.clientProvider.For(obj)
            if opts.Namespace != "" {
                return client.ListInNamespace(ctx, opts.Namespace, options)
            }
            return client.List(ctx, options)
        },
        WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
            <-f.ready
            // inject selectors from opts
            client, err := f.clientProvider.For(obj)
            if opts.Namespace != "" {
                return client.WatchInNamespace(ctx, opts.Namespace, options)
            }
            return client.Watch(ctx, options)
        },
    }
}
```

`opts.Namespace` is set by the namespace filter (Tier 1) when `IsSingleNamespace()` is true. Empty means cluster-scoped watch.

## GenericClient interface

`GenericClient` in `pkg/runtime/informer/type.go` defines the four operations the ListerWatcher closures need:

```go
type GenericClient interface {
    List(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error)
    Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
    ListInNamespace(ctx context.Context, namespace string, opts metav1.ListOptions) (runtime.Object, error)
    WatchInNamespace(ctx context.Context, namespace string, opts metav1.ListOptions) (watch.Interface, error)
}
```

`List`/`Watch` use the namespace baked into the `Client` at construction time (from `CRDInfo.Namespace`). `ListInNamespace`/`WatchInNamespace` override that with an explicit namespace — used only by the Tier 1 path for typed informers.

The concrete implementation is `*kubeclient.Client` in `pkg/kubeclient/genericclient.go`. See [pkg/kubeclient/README.md](../../../kubeclient/README.md) for details.

## Options

```go
type Options struct {
    Name          string
    Resync        time.Duration
    Wq            *queue.Workqueue
    LabelSelector string
    FieldSelector string
    Namespace     string  // "" = cluster-scoped; set by Tier 1 filter
}
```

`Name` is the human-readable label stored in `InformerEntry`. `Resync` defaults to `Factory.defaultResync` when zero. `Wq` selects the per-CRD queue; nil falls back to the shared default queue. `Namespace` is set by `konstructRuntime` when the namespace filter's `IsSingleNamespace()` is true.

## For vs ForListerWatcher

| Method | When used | ListerWatcher source |
|--------|-----------|----------------------|
| `For(obj, ctx, opts)` | Typed CRDs with registered Go types | `Factory.newListWatch(obj, opts)` using `GenericClient` |
| `ForListerWatcher(lw, obj, ctx, opts)` | Dynamic CRDs, custom watchers | Caller-provided `cache.ListerWatcher` |

Both methods call `getOrCreate` internally. If an informer already exists for the GVK, the existing one is returned without creating a duplicate.

---

**← Back to** [01 — Architecture](01-architecture.md)
