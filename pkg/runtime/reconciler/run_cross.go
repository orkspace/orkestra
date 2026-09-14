// pkg/reconciler/run_cross.go
//
// Cross-CRD HTTP fallback — fetches a CR detail from another Orkestra instance.
//
// The primary path (informer cache, zero API calls) lives in run_template_reconcile.go
// in ReadCrossFromDeclarations. That function calls fetchCrossViaHTTP when the
// informer is unavailable (cross-binary, cross-cluster).
//
// The full informer-cache path requires threading the Kordinator registry into
// GenericReconciler. ReadCrossFromInformer is called instead of fetchCrossViaHTTP for same-binary CRDs.
package reconciler

import (
	"context"
	"fmt"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils/common"
)

// fetchCrossViaHTTP fetches a CR's detail from an Orkestra CR endpoint.
// The endpoint should be the /katalog/{crd}/cr/{namespace}/{name} URL
// which is already built and running on every Orkestra instance.
//
// Returns nil on any error — callers treat nil as "not found".
// Delegates to common.FetchCrossViaHTTP
func fetchCrossViaHTTP(ctx context.Context, cs kubernetes.Interface, source *orktypes.CrossSource) map[string]interface{} {
	_, result := common.FetchCrossViaHTTP(ctx, cs, source)
	return result
}

// ReadCrossFromInformerByName reads one CR from an informer cache by namespace/name key.
// Zero API server calls — pure in-memory map lookup.
//
// key is "namespace/name" for namespaced CRDs or "name" for cluster-scoped.
// sourceCrossAccess is the CrossAccess field of the target CRD entry — nil means
// allowed (default). When false, returns notFoundCrossResult() without reading.
//
// Returns a consistent map shape regardless of whether the CR was found —
// callers use .found == "true" to gate their logic.
func ReadCrossFromInformerByName(
	indexer cache.Indexer,
	key string,
	sourceCrossAccess *bool,
) map[string]interface{} {
	if sourceCrossAccess != nil && !*sourceCrossAccess {
		return notFoundCrossResult()
	}

	raw, exists, err := indexer.GetByKey(key)
	if err != nil || !exists {
		return notFoundCrossResult()
	}

	objMap, err := rawToMap(raw)
	if err != nil {
		return notFoundCrossResult()
	}

	return buildCrossResultFromMap(objMap)
}

// ReadCrossFromInformerByLabel reads the first CR from an informer cache whose
// labels contain labelKey=labelValue.
//
// Returns the first match. When multiple CRs share the label, the first
// returned by List() is used — List() order is not guaranteed. If you need
// a specific CR, use name-based lookup (ReadCrossFromInformer) instead.
func ReadCrossFromInformerByLabel(
	indexer cache.Indexer,
	labelKey, labelValue string,
) map[string]interface{} {
	for _, raw := range indexer.List() {
		objMap, err := rawToMap(raw)
		if err != nil {
			continue
		}
		labels, _ := objMap["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
		if fmt.Sprint(labels[labelKey]) == labelValue {
			return buildCrossResultFromMap(objMap)
		}
	}
	return notFoundCrossResult()
}

// buildCrossResultFromMap extracts the fields relevant for cross-CRD observation.
// Includes: found, name, namespace, spec, status, labels, annotations.
// Does NOT include managed fields — those are Kubernetes internal metadata
// for server-side apply tracking and have no meaningful use in templates.
func buildCrossResultFromMap(objMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, 6)
	result["found"] = "true"
	result["name"] = metaField(objMap, "name")
	result["namespace"] = metaField(objMap, "namespace")

	if spec, ok := objMap["spec"].(map[string]interface{}); ok && spec != nil {
		result["spec"] = spec
	} else {
		result["spec"] = map[string]interface{}{}
	}

	if status, ok := objMap["status"].(map[string]interface{}); ok && status != nil {
		result["status"] = status
	} else {
		result["status"] = map[string]interface{}{}
	}

	meta, _ := objMap["metadata"].(map[string]interface{})
	if rawLabels, ok := meta["labels"].(map[string]interface{}); ok && len(rawLabels) > 0 {
		result["labels"] = rawLabels
	}
	if rawAnnotations, ok := meta["annotations"].(map[string]interface{}); ok && len(rawAnnotations) > 0 {
		result["annotations"] = rawAnnotations
	}

	return result
}

func notFoundCrossResult() map[string]interface{} {
	return map[string]interface{}{
		"found":  "false",
		"spec":   map[string]interface{}{},
		"status": map[string]interface{}{},
	}
}

// crossKey builds the informer cache key for a CR.
// Namespaced: "namespace/name"
// Cluster-scoped: "name"
func crossKey(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return fmt.Sprintf("%s/%s", namespace, name)
}
