package common

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func InjectAnnotationToObject(obj *unstructured.Unstructured, raw map[string]interface{}, annotation string) {
	b, err := json.Marshal(raw)
	if err != nil {
		return
	}
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = make(map[string]string, 1)
	}
	ann[annotation] = string(b)
	obj.SetAnnotations(ann)
}

func ResolveAnnotationFromObject(obj map[string]interface{}, annotation string) map[string]interface{} {
	meta, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		return nil
	}
	annotations, ok := meta["annotations"].(map[string]interface{})
	if !ok {
		return nil
	}
	annJSON, ok := annotations[annotation].(string)
	if !ok || annJSON == "" {
		return nil
	}
	var ann map[string]interface{}
	if err := json.Unmarshal([]byte(annJSON), &ann); err != nil {
		return nil
	}
	return ann
}
