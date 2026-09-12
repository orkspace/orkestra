// pkg/runtime/informer/observe/resolve.go
package observe

import (
	"strings"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

// resolveWatchKeys resolves the primary CR key(s) from a watched resource event.
//
// Resolution order:
//  1. keyFrom.label  — label on the watched object carries the key
//  2. keyFrom.name   — fixed primary CR name declared in the watch entry
//  3. ownerReference — owner of the watched object matches the primary CRD
//  4. broadcast      — no match found; enqueue all known primary CRs
//
// This function only resolves identity. It does not evaluate enqueue gates or
// write to the workqueue.
func (o *Observer) resolveWatchKeys(opts watchOptions, obj interface{}) []string {
	u, ok := domain.ToUnstructured(obj)
	if !ok {
		return nil
	}

	primaryKind := opts.crd.Kind()

	// 1. keyFrom.label
	if kf := opts.entry.KeyFrom; kf != nil && kf.Label != "" {
		key, ok := u.GetLabels()[kf.Label]
		if ok && key != "" {
			logger.Debug().
				Str("primary", primaryKind).
				Str("key", key).
				Str("label", kf.Label).
				Msg("observe: resolved via keyFrom.label")

			return []string{key}
		}
	}

	// 2. keyFrom.name
	if kf := opts.entry.KeyFrom; kf != nil && kf.Name != "" {
		return []string{kf.Key()}
	}

	// 3. ownerReference
	primaryAPIVersion := opts.crd.APIVersion()

	for _, ref := range u.GetOwnerReferences() {
		if ref.APIVersion != primaryAPIVersion ||
			ref.Kind != primaryKind {
			continue
		}

		key := ref.Name

		if namespace := u.GetNamespace(); namespace != "" {
			key = namespace + "/" + ref.Name
		}

		logger.Debug().
			Str("primary", primaryKind).
			Str("key", key).
			Str("watched", u.GetKind()).
			Msg("observe: resolved via ownerReference")

		return []string{key}
	}

	// 4. broadcast
	if !opts.broadcastAllowed {
		return nil
	}

	registered := o.deps.Informer.Registered()

	entry, ok := registered[opts.crd.GVKString()]
	if !ok || entry == nil {
		return nil
	}

	var keys []string

	for _, item := range entry.Informer.GetIndexer().List() {
		object, ok := item.(metav1.Object)
		if !ok {
			continue
		}

		key, err := cache.MetaNamespaceKeyFunc(object)
		if err != nil {
			continue
		}

		keys = append(keys, key)
	}

	return keys
}

func splitWatchField(field string) []string {
	field = strings.TrimPrefix(field, ".")

	if field == "" {
		return nil
	}

	return strings.Split(field, ".")
}

// ResolveGVR resolves a ManagedResource into a concrete GroupVersionResource
// using the configured Katalog.
func (o *Observer) resolveGVR(r domain.ManagedResource) (schema.GroupVersionResource, bool) {
	return o.deps.Katalog.ResolveGVR(r)
}
