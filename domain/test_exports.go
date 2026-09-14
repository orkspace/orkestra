package domain

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func UnstructuredForTest() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "demo.orkestra.io/v1alpha1",
			"kind":       "Website",
			"metadata": map[string]interface{}{
				"name":      "example",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"url": "https://example.com",
			},
		},
	}
}

func GVKForTest() string {
	return "demo.orkestra.io/v1alpha1, Kind=Website"
}

func KeyForTest() string {
	return "default/example"
}

func NamespaceForTest() string {
	return "default"
}

func NameForTest() string {
	return "example"
}

func URLForTest() string {
	return "https://example.com"
}
