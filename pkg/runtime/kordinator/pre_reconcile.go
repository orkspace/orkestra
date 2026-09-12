package kordinator

import (
	"context"

	"github.com/orkspace/orkestra/domain"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// objectFromCache retrieves the live CR from the informer cache for the given
// key. Returns nil if the entry has no informer or the key is not found.
func (k *Kontroller) objectFromCache(entry RegistryEntry, key string) *unstructured.Unstructured {
	if entry.Informer == nil {
		return nil
	}
	raw, exists, err := entry.Informer.GetIndexer().GetByKey(key)
	if err != nil || !exists || raw == nil {
		return nil
	}
	obj, ok := raw.(*unstructured.Unstructured)
	if !ok {
		return nil
	}
	return obj
}

// evaluatePreReconcileCheck evaluates the preReconcile.when/or gate for the
// given CR. Returns (true, reason) when gated — reconciler must not be called.
// Returns (false, "") when conditions pass.
//
// Delegates to k.kat.EvaluatePreReconcile which holds the full resolver chain
// (profiles, notes, serve intent) — no duplication of eval logic here.
func (k *Kontroller) evaluatePreReconcileCheck(
	ctx context.Context,
	obj *unstructured.Unstructured,
	crdName string,
	sentinels map[string]string,
) (gated bool, reason string) {
	if k.kat == nil || obj == nil {
		return false, ""
	}
	allowed, reason := k.kat.EvaluatePreReconcile(ctx, crdName, obj, k.kube.Clientset(), domain.EvaluateOptions{Sentinels: sentinels})
	return !allowed, reason
}
