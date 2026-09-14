package orkadapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/utils"
	"github.com/rs/zerolog/log"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	sigs "sigs.k8s.io/controller-runtime/pkg/client"
)

// List implements the controller-runtime List contract.
//
// Reads are cache-first when Orkestra has an informer store for the requested
// object type. If the requested semantics cannot be satisfied from the cache,
// List falls back to the live Kubernetes API.
//
// CRUD operations intentionally do not live here. They are delegated through
// kubeclient.Interface. List remains adapter-owned because controller-runtime
// list semantics include cache/index behaviour that does not map cleanly onto
// one native kubeclient operation.
func (a *ctrlClientAdapter) List(ctx context.Context, list sigs.ObjectList, opts ...sigs.ListOption) error {
	if list == nil {
		return fmt.Errorf("ctrlclient List: object list must not be nil")
	}

	lo := &sigs.ListOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt.ApplyToList(lo)
		}
	}

	gvk, err := a.listGVK(list)
	if err != nil {
		return err
	}

	// Pagination is an API-server concern and cannot be reproduced from an
	// informer store. Raw options may also contain semantics unavailable to
	// the local cache, so use the live API in those cases.
	if lo.Limit == 0 &&
		lo.Continue == "" &&
		lo.Raw == nil {
		if items, ok, _ := a.listFromStore(gvk, lo); ok {
			return a.assembleStoreList(list, items)
		}

		log.Debug().
			Str("gvk", gvk.String()).
			Str("namespace", lo.Namespace).
			Msg("ctrlclient.List: cache miss — live API call")
	}

	return a.listFromAPI(ctx, list, gvk, lo)
}

// listGVK resolves the item GVK represented by an ObjectList.
func (a *ctrlClientAdapter) listGVK(list sigs.ObjectList) (schema.GroupVersionKind, error) {
	gvks, _, err := a.k.Scheme().ObjectKinds(list)
	if err != nil {
		return schema.GroupVersionKind{}, fmt.Errorf("ctrlclient List: unknown type %T: %w", list, err)
	}

	if len(gvks) == 0 {
		return schema.GroupVersionKind{}, fmt.Errorf("ctrlclient List: no GVK registered for %T", list)
	}

	gvk := gvks[0]

	if gvk.Kind == "" {
		return schema.GroupVersionKind{}, fmt.Errorf("ctrlclient List: empty kind for %T", list)
	}

	if !strings.HasSuffix(gvk.Kind, "List") {
		return schema.GroupVersionKind{}, fmt.Errorf("ctrlclient List: %s is not a list kind", gvk)
	}

	return gvk, nil
}

// listFromStore attempts to satisfy the request from the informer cache.
func (a *ctrlClientAdapter) listFromStore(listGVK schema.GroupVersionKind, lo *sigs.ListOptions) ([]*unstructured.Unstructured, bool, string) {
	getStoreFor := a.k.GetStoreFor()
	if getStoreFor == nil {
		return nil, false, "storeFor not wired"
	}

	itemGVK := schema.GroupVersionKind{
		Group:   listGVK.Group,
		Version: listGVK.Version,
		Kind:    strings.TrimSuffix(listGVK.Kind, "List"),
	}

	if itemGVK.Kind == listGVK.Kind {
		return nil, false, "not a list type"
	}

	store := getStoreFor(itemGVK)
	if store == nil {
		return nil, false,
			"no informer store for " + itemGVK.String()
	}

	// Field selectors require an index. Without one, fall back to the API
	// rather than pretending the informer cache can satisfy the query.
	if lo.FieldSelector != nil && !lo.FieldSelector.Empty() {
		indexerFor := a.k.GetIndexerFor()
		if indexerFor == nil {
			return nil, false,
				"field selector present but indexerFor not wired"
		}

		indexer := indexerFor(itemGVK)
		if indexer == nil {
			return nil, false,
				"field selector present but no indexer for " +
					itemGVK.String()
		}

		registered := indexer.GetIndexers()
		reqs := lo.FieldSelector.Requirements()

		for i, req := range reqs {
			if _, ok := registered[req.Field]; !ok {
				continue
			}

			raws, err := indexer.ByIndex(req.Field, req.Value)
			if err != nil {
				return nil, false,
					"ByIndex error: " + err.Error()
			}

			remaining := append(
				reqs[:i:i],
				reqs[i+1:]...,
			)

			result := make(
				[]*unstructured.Unstructured,
				0,
				len(raws),
			)

			for _, raw := range raws {
				u, ok := raw.(*unstructured.Unstructured)
				if !ok {
					return nil, false, fmt.Sprintf(
						"index item is %T, not *unstructured.Unstructured",
						raw,
					)
				}

				if lo.Namespace != "" && u.GetNamespace() != lo.Namespace {
					continue
				}

				if lo.LabelSelector != nil &&
					!lo.LabelSelector.Matches(labels.Set(u.GetLabels())) {
					continue
				}

				if !utils.MatchesFieldRequirements(u, remaining) {
					continue
				}
				result = append(result, u.DeepCopy())
			}

			return result, true, ""
		}

		return nil, false, "no registered index covers field selector " + lo.FieldSelector.String()
	}

	var selector labels.Selector
	if lo.LabelSelector != nil {
		selector = lo.LabelSelector
	} else {
		selector = labels.Everything()
	}

	result := make([]*unstructured.Unstructured, 0, len(store.List()))

	for _, raw := range store.List() {
		u, ok := raw.(*unstructured.Unstructured)
		if !ok {
			return nil, false, fmt.Sprintf("store item is %T, not *unstructured.Unstructured", raw)
		}

		if lo.Namespace != "" && u.GetNamespace() != lo.Namespace {
			continue
		}

		if !selector.Matches(labels.Set(u.GetLabels())) {
			continue
		}
		result = append(result, u.DeepCopy())
	}

	return result, true, ""
}

// listFromAPI performs a live Kubernetes List.
//
// This is intentionally the only List path that reaches the dynamic client.
// CRUD operations remain behind kubeclient.Interface.
func (a *ctrlClientAdapter) listFromAPI(ctx context.Context, list sigs.ObjectList, gvk schema.GroupVersionKind, lo *sigs.ListOptions) error {
	mapping, err := a.k.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("ctrlclient List: no REST mapping for %s: %w", gvk, err)
	}

	// Use controller-runtime's conversion so Limit, Continue, selectors and
	// Raw ListOptions are preserved rather than manually reconstructing them.
	listOpts := *lo.AsListOptions()

	resource := a.k.DynamicClient().Resource(mapping.Resource)

	var ul *unstructured.UnstructuredList

	if lo.Namespace != "" {
		ul, err = resource.Namespace(lo.Namespace).List(ctx, listOpts)
	} else {
		ul, err = resource.List(ctx, listOpts)
	}

	if err != nil {
		return err
	}

	return a.assembleAPIList(list, ul)
}

// assembleAPIList converts a dynamic Kubernetes list into the concrete
// controller-runtime ObjectList supplied by the caller.
func (a *ctrlClientAdapter) assembleAPIList(list sigs.ObjectList, ul *unstructured.UnstructuredList) error {
	items := make([]interface{}, len(ul.Items))

	for i := range ul.Items {
		out := map[string]interface{}{}

		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(ul.Items[i].Object, &out); err != nil {
			return fmt.Errorf("ctrlclient List: convert item %d: %w", i, err)
		}

		items[i] = out
	}

	listMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(list)
	if err != nil {
		return fmt.Errorf("ctrlclient List: marshal list shell: %w", err)
	}

	// Preserve server metadata such as resourceVersion, continue and
	// remainingItemCount.
	for k, v := range ul.Object {
		if k != "items" {
			listMap[k] = v
		}
	}

	listMap["items"] = items

	if err := runtime.DefaultUnstructuredConverter.
		FromUnstructured(listMap, list); err != nil {
		return fmt.Errorf("ctrlclient List: decode result into %T: %w", list, err)
	}

	return nil
}

// assembleStoreList converts informer-cache objects into the concrete
// controller-runtime ObjectList supplied by the caller.
func (a *ctrlClientAdapter) assembleStoreList(list sigs.ObjectList, items []*unstructured.Unstructured) error {
	rawItems := make([]interface{}, len(items))

	for i, u := range items {
		rawItems[i] = u.Object
	}

	listMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(list)
	if err != nil {
		return fmt.Errorf("ctrlclient List (cache): marshal list shell: %w", err)
	}

	listMap["items"] = rawItems

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(listMap, list); err != nil {
		return fmt.Errorf("ctrlclient List (cache): decode result into %T: %w", list, err)
	}

	return nil
}
