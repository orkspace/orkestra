// pkg/runtime/kordinator/vitals/cr_children.go
//
// Child resource fetching and readiness for the CR detail endpoint.
package vitals

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/orkspace/orkestra/pkg/children"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// childrenCacheTTL controls how long endpoint children results are served from
// memory before re-fetching from the API server. Matched to a typical resync
// period — data is at most this stale, which is acceptable for observability.
const childrenCacheTTL = 30 * time.Second

type cachedChildren struct {
	data map[string]interface{}
	at   time.Time
}

// endpointChildrenCache stores endpoint-ready children per CR, keyed by "namespace/name".
// Eliminates repeated concurrent LIST calls when the UI polls the detail endpoint.
var endpointChildrenCache sync.Map

// childGVREntry carries the GVR and readiness metadata for one child kind.
type childGVREntry struct {
	GVR        schema.GroupVersionResource
	Key        string // lowercase kind — matches .children.<key>
	Statusless bool   // from builtInRegistry — not inherited from parent CRD
}

// knownChildGVRs is built once at startup from builtInRegistry.
// Statusless is per-child-kind, independent of the parent CRD's statusless flag.
var knownChildGVRs = func() []childGVREntry {
	defs := children.ChildGVRs()

	out := make([]childGVREntry, len(defs))
	for i, d := range defs {
		m := children.BuiltInMeta(d.Key)
		out[i] = childGVREntry{
			GVR:        d.GVR,
			Key:        d.Key,
			Statusless: m.Statusless || m.SkipStatusSubresource,
		}
	}
	return out
}()

// ─────────────────────────────────────────────────────────────────────────────
// Children fetch
// ─────────────────────────────────────────────────────────────────────────────

// readChildrenForEndpoint fetches all child resources for a CR.
// Returns map[string]interface{} where:
//   - single child  → ChildSummary         (UI renders as object)
//   - multiple      → []ChildSummary        (UI renders as array)
//   - none          → key absent from map
//
// Only queries the resource types declared in rc (onCreate/onReconcile templates).
// Results are cached for childrenCacheTTL to avoid repeated concurrent LISTs.
func readChildrenForEndpoint(
	ctx context.Context,
	kube *kubeclient.Kubeclient,
	owner map[string]interface{},
	rc orktypes.OperatorBoxConfig,
) map[string]interface{} {
	if kube == nil {
		return map[string]interface{}{}
	}

	ns := metaField(owner, "namespace")
	name := metaField(owner, "name")
	cacheKey := ns + "/" + name

	// Serve from cache if still fresh.
	if cached, ok := endpointChildrenCache.Load(cacheKey); ok {
		c := cached.(cachedChildren)
		if time.Since(c.at) < childrenCacheTTL {
			return c.data
		}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	labelSelector := fmt.Sprintf("orkestra-owner=%s", name)

	// Only query types declared in the Katalog — avoids firing 22 concurrent
	// LISTs when the CR uses 2-3 resource types. Falls back to all types when
	// no config is provided (e.g. Katalog with Go hooks instead of templates).
	gvrs := relevantGVRsFromConfig(rc)

	type result struct {
		key   string
		items []ChildSummary
	}

	ch := make(chan result, len(gvrs))

	for _, entry := range gvrs {
		entry := entry
		go func() {
			ch <- result{key: entry.Key, items: fetchChildKind(fetchCtx, kube, ns, labelSelector, entry)}
		}()
	}

	childMap := make(map[string]interface{}, len(gvrs))
	for range gvrs {
		select {
		case r := <-ch:
			switch len(r.items) {
			case 0:
				// nothing — omit key
			case 1:
				childMap[r.key] = r.items[0] // flat object — backward compatible
			default:
				childMap[r.key] = r.items // array when multiple of same kind
			}
		case <-fetchCtx.Done():
			endpointChildrenCache.Store(cacheKey, cachedChildren{data: childMap, at: time.Now()})
			return childMap
		}
	}

	// Custom resources have dynamic GVRs — fetch separately from standard types.
	customSrcs := mergeCustomResourceSrcs(rc)
	if len(customSrcs) > 0 {
		customs := fetchCustomChildren(fetchCtx, kube, owner, ns, labelSelector, customSrcs)
		switch len(customs) {
		case 1:
			childMap["custom"] = customs[0]
		default:
			if len(customs) > 1 {
				childMap["custom"] = customs
			}
		}
	}

	endpointChildrenCache.Store(cacheKey, cachedChildren{data: childMap, at: time.Now()})
	return childMap
}

// InvalidateChildrenCache removes the cached children for a CR.
// Called by the reconciler after a successful reconcile so the next endpoint
// request reflects the latest state immediately.
func InvalidateChildrenCache(namespace, name string) {
	endpointChildrenCache.Delete(namespace + "/" + name)
}

// ─────────────────────────────────────────────────────────────────────────────
// GVR filtering
// ─────────────────────────────────────────────────────────────────────────────

// relevantGVRsFromConfig returns only the GVR entries for resource types
// declared in the OperatorBoxConfig's onCreate/onReconcile templates, per
// OperatorBoxConfig.DeclaredChildKinds() — the single source of truth for
// "which built-in kinds does this box declare," colocated with HookTemplates
// itself in pkg/types rather than duplicated here.
//
// When OnCreate and OnReconcile are both nil — no declarative template info
// at all (e.g. a Katalog using Go hooks instead of templates) — every known
// GVR is checked, since there's nothing to narrow the search with. This is
// distinct from a CRD that declares only custom: entries and zero built-in
// kinds: DeclaredChildKinds() correctly returns empty there too, but firing
// all ~24 built-in LIST calls anyway both wastes work and risks the fixed
// fetchTimeout expiring before the (separately-fetched) custom resources
// block ever runs, silently returning empty children with no error.
func relevantGVRsFromConfig(box orktypes.OperatorBoxConfig) []childGVREntry {
	if box.OnCreate == nil && box.OnReconcile == nil {
		return knownChildGVRs
	}
	needed := box.DeclaredChildKinds()
	if len(needed) == 0 {
		return nil
	}
	result := make([]childGVREntry, 0, len(needed))
	for _, e := range knownChildGVRs {
		if needed[e.Key] {
			result = append(result, e)
		}
	}
	return result
}

// mergeCustomResourceSrcs collects all custom resource template entries from
// both onCreate and onReconcile, deduplicating by APIVersion+Kind so each
// unique GVR is only queried once.
func mergeCustomResourceSrcs(box orktypes.OperatorBoxConfig) []orktypes.CustomResourceTemplateSource {
	type key struct{ apiVersion, kind string }
	seen := map[key]bool{}
	var out []orktypes.CustomResourceTemplateSource
	add := func(t *orktypes.HookTemplates) {
		if t == nil {
			return
		}
		for _, src := range t.CustomResource {
			if src.APIVersion == "" || src.Kind == "" {
				continue
			}
			k := key{src.APIVersion, src.Kind}
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, src)
		}
	}
	add(box.OnCreate)
	add(box.OnReconcile)
	return out
}

// fetchCustomChildren lists all custom resources declared in srcs that are
// owned by the CR (matched by orkestra-owner label). Each unique APIVersion/Kind
// is resolved to a GVR via the REST mapper and listed once.
//
// A custom resource's namespace is not necessarily the parent CR's namespace —
// metadata.namespace in the template can be a literal override (e.g. an ArgoCD
// Application hardcoded to "argocd") or a template expression. ownerMap lets us
// render it correctly via the same Resolver the create path uses, instead of
// assuming it always matches the parent.
func fetchCustomChildren(
	ctx context.Context,
	kube *kubeclient.Kubeclient,
	ownerMap map[string]interface{},
	ns, labelSelector string,
	srcs []orktypes.CustomResourceTemplateSource,
) []ChildSummary {
	resolver := orktmpl.NewResolverFromMap(ownerMap)

	var result []ChildSummary
	for i := range srcs {
		src := &srcs[i]
		gvr, err := src.ResolveGVR(kube.RESTMapper())
		if err != nil {
			logger.Warn().
				Str("apiVersion", src.APIVersion).
				Str("kind", src.Kind).
				Err(err).
				Msg("children: failed to resolve GVR for custom resource — child will not appear until this resolves")
			continue
		}

		childNS := ns
		if resolved, rerr := resolver.Resolve(src.Metadata.Namespace); rerr == nil && resolved != "" {
			childNS = resolved
		}

		resource := kube.DynamicClient().Resource(gvr)
		opts := metav1.ListOptions{
			LabelSelector:   labelSelector,
			ResourceVersion: "0",
		}
		var list *unstructured.UnstructuredList
		if src.IsNamespaced() && childNS != "" {
			list, err = resource.Namespace(childNS).List(ctx, opts)
		} else {
			list, err = resource.List(ctx, opts)
		}
		if err != nil {
			logger.Warn().
				Str("apiVersion", src.APIVersion).
				Str("kind", src.Kind).
				Str("namespace", childNS).
				Err(err).
				Msg("children: failed to list custom resource")
			continue
		}
		if len(list.Items) == 0 {
			continue
		}
		for _, obj := range list.Items {
			status, _ := obj.Object["status"].(map[string]interface{})
			if status == nil {
				status = map[string]interface{}{}
			}
			result = append(result, ChildSummary{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
				Kind:      src.Kind,
				Status:    status,
				Ready:     isChildReady(obj.Object, false),
			})
		}
	}
	return result
}

// fetchChildKind lists all resources of one kind owned by the CR.
func fetchChildKind(
	ctx context.Context,
	kube *kubeclient.Kubeclient,
	ns, labelSelector string,
	entry childGVREntry,
) []ChildSummary {
	resource := kube.DynamicClient().Resource(entry.GVR)

	opts := metav1.ListOptions{
		LabelSelector:   labelSelector,
		Limit:           listOptionsLimit,
		ResourceVersion: "0",
	}

	var list *unstructured.UnstructuredList
	var err error

	if ns != "" {
		list, err = resource.Namespace(ns).List(ctx, opts)
	} else {
		list, err = resource.List(ctx, opts)
	}

	if err != nil || len(list.Items) == 0 {
		return nil
	}

	result := make([]ChildSummary, 0, len(list.Items))
	for _, obj := range list.Items {
		status, _ := obj.Object["status"].(map[string]interface{})
		if status == nil {
			status = map[string]interface{}{}
		}
		kind := obj.GetKind()
		if kind == "" {
			kind = strings.ToUpper(entry.Key[:1]) + entry.Key[1:]
		}
		result = append(result, ChildSummary{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
			Kind:      kind,
			Status:    status,
			Ready:     isChildReady(obj.Object, entry.Statusless),
		})
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// Child readiness
// ─────────────────────────────────────────────────────────────────────────────

// isChildReady determines readiness for one child resource.
// statusless is from builtInRegistry (per-kind, not from parent CRD).
func isChildReady(obj map[string]interface{}, statusless bool) bool {
	if statusless {
		return true // ConfigMap, Secret, ServiceAccount — ready on existence
	}

	status, _ := obj["status"].(map[string]interface{})
	if status == nil {
		return false
	}

	kind := strings.ToLower(fmt.Sprint(obj["kind"]))

	switch kind {
	case "job":
		s, _ := status["succeeded"].(int64)
		return s > 0

	case "deployment", "statefulset", "replicaset":
		rr, _ := status["readyReplicas"].(int64)
		if rr == 0 {
			return false
		}
		if spec, _ := obj["spec"].(map[string]interface{}); spec != nil {
			if desired, _ := spec["replicas"].(int64); desired > 0 {
				return rr >= desired
			}
		}
		return rr > 0

	case "daemonset":
		desired, _ := status["desiredNumberScheduled"].(int64)
		ready, _ := status["numberReady"].(int64)
		return desired > 0 && ready >= desired

	case "cronjob":
		_, has := status["lastScheduleTime"]
		return has

	case "service":
		return true // Services are always considered available

	case "pod":
		phase, _ := status["phase"].(string)
		return phase == "Running" || phase == "Succeeded"

	default:
		// Check standard conditions
		conditions, _ := status["conditions"].([]interface{})
		for _, c := range conditions {
			if cond, ok := c.(map[string]interface{}); ok {
				if cond["type"] == "Ready" {
					return cond["status"] == "True"
				}
			}
		}
		return true // optimistic for unknown types
	}
}
