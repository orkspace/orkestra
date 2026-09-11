package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func eventForReference(field, apiVersion, kind, name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			field: map[string]interface{}{
				"apiVersion": apiVersion,
				"kind":       kind,
				"name":       name,
				"namespace":  namespace,
			},
		},
	}
}

func eventForDottedReference(apiVersion, kind, name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"database": map[string]interface{}{
					"target": map[string]interface{}{
						"apiVersion": apiVersion,
						"kind":       kind,
						"name":       name,
						"namespace":  namespace,
					},
				},
			},
		},
	}
}

func TestManagedResource_MatchesEventReference(t *testing.T) {
	event := eventForReference(
		"regarding",
		"databases.example.com/v1",
		"Database",
		"my-db",
		"default",
	)

	tests := []struct {
		name     string
		filter   *ManagedResource
		expected bool
	}{
		{
			name:     "nil receiver matches everything",
			filter:   nil,
			expected: true,
		},
		{
			name:     "empty filter matches everything",
			filter:   &ManagedResource{},
			expected: true,
		},
		{
			name: "apiVersion matches",
			filter: &ManagedResource{
				APIVersion: "databases.example.com/v1",
			},
			expected: true,
		},
		{
			name: "kind matches",
			filter: &ManagedResource{
				Kind: "Database",
			},
			expected: true,
		},
		{
			name: "name matches",
			filter: &ManagedResource{
				Name: "my-db",
			},
			expected: true,
		},
		{
			name: "namespace matches",
			filter: &ManagedResource{
				Namespace: "default",
			},
			expected: true,
		},
		{
			name: "all fields match",
			filter: &ManagedResource{
				APIVersion: "databases.example.com/v1",
				Kind:       "Database",
				Name:       "my-db",
				Namespace:  "default",
			},
			expected: true,
		},
		{
			name: "empty fields act as wildcards",
			filter: &ManagedResource{
				Kind: "Database",
			},
			expected: true,
		},
		{
			name: "apiVersion and kind match",
			filter: &ManagedResource{
				APIVersion: "databases.example.com/v1",
				Kind:       "Database",
			},
			expected: true,
		},
		{
			name: "name and namespace match",
			filter: &ManagedResource{
				Name:      "my-db",
				Namespace: "default",
			},
			expected: true,
		},
		{
			name: "apiVersion mismatch",
			filter: &ManagedResource{
				APIVersion: "v1",
			},
			expected: false,
		},
		{
			name: "kind mismatch",
			filter: &ManagedResource{
				Kind: "Deployment",
			},
			expected: false,
		},
		{
			name: "name mismatch",
			filter: &ManagedResource{
				Name: "other-db",
			},
			expected: false,
		},
		{
			name: "namespace mismatch",
			filter: &ManagedResource{
				Namespace: "other",
			},
			expected: false,
		},
		{
			name: "apiVersion mismatch with otherwise matching fields",
			filter: &ManagedResource{
				APIVersion: "v1",
				Kind:       "Database",
				Name:       "my-db",
				Namespace:  "default",
			},
			expected: false,
		},
		{
			name: "kind mismatch with otherwise matching fields",
			filter: &ManagedResource{
				APIVersion: "databases.example.com/v1",
				Kind:       "Deployment",
				Name:       "my-db",
				Namespace:  "default",
			},
			expected: false,
		},
		{
			name: "name mismatch with otherwise matching fields",
			filter: &ManagedResource{
				APIVersion: "databases.example.com/v1",
				Kind:       "Database",
				Name:       "other-db",
				Namespace:  "default",
			},
			expected: false,
		},
		{
			name: "namespace mismatch with otherwise matching fields",
			filter: &ManagedResource{
				APIVersion: "databases.example.com/v1",
				Kind:       "Database",
				Name:       "my-db",
				Namespace:  "other",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.filter.Matches(event, "regarding"))
		})
	}
}

func TestManagedResource_MatchesRelatedReference(t *testing.T) {
	event := eventForReference(
		"related",
		"v1",
		"ConfigMap",
		"shared-config",
		"default",
	)

	tests := []struct {
		name     string
		filter   *ManagedResource
		expected bool
	}{
		{
			name: "all fields match",
			filter: &ManagedResource{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "shared-config",
				Namespace:  "default",
			},
			expected: true,
		},
		{
			name: "kind matches",
			filter: &ManagedResource{
				Kind: "ConfigMap",
			},
			expected: true,
		},
		{
			name: "name matches",
			filter: &ManagedResource{
				Name: "shared-config",
			},
			expected: true,
		},
		{
			name: "namespace matches",
			filter: &ManagedResource{
				Namespace: "default",
			},
			expected: true,
		},
		{
			name: "kind mismatch",
			filter: &ManagedResource{
				Kind: "Secret",
			},
			expected: false,
		},
		{
			name: "name mismatch",
			filter: &ManagedResource{
				Name: "other-config",
			},
			expected: false,
		},
		{
			name: "namespace mismatch",
			filter: &ManagedResource{
				Namespace: "kube-system",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.filter.Matches(event, "related"))
		})
	}
}

func TestManagedResource_MatchesDottedReference(t *testing.T) {
	event := eventForDottedReference(
		"databases.example.com/v1",
		"Database",
		"my-db",
		"default",
	)

	tests := []struct {
		name     string
		filter   *ManagedResource
		expected bool
	}{
		{
			name: "all fields match",
			filter: &ManagedResource{
				APIVersion: "databases.example.com/v1",
				Kind:       "Database",
				Name:       "my-db",
				Namespace:  "default",
			},
			expected: true,
		},
		{
			name: "name and namespace match",
			filter: &ManagedResource{
				Name:      "my-db",
				Namespace: "default",
			},
			expected: true,
		},
		{
			name: "kind mismatch",
			filter: &ManagedResource{
				Kind: "Deployment",
			},
			expected: false,
		},
		{
			name: "name mismatch",
			filter: &ManagedResource{
				Name: "other-db",
			},
			expected: false,
		},
		{
			name: "namespace mismatch",
			filter: &ManagedResource{
				Namespace: "other",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.filter.Matches(event, "spec.database.target"))
		})
	}
}

func TestManagedResource_MatchesMissingReference(t *testing.T) {
	event := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"reason": "DatabaseReady",
		},
	}

	tests := []struct {
		name     string
		filter   *ManagedResource
		expected bool
	}{
		{
			name:     "nil receiver matches everything",
			filter:   nil,
			expected: true,
		},
		{
			name:     "empty filter matches missing reference",
			filter:   &ManagedResource{},
			expected: true,
		},
		{
			name: "apiVersion does not match missing reference",
			filter: &ManagedResource{
				APIVersion: "databases.example.com/v1",
			},
			expected: false,
		},
		{
			name: "kind does not match missing reference",
			filter: &ManagedResource{
				Kind: "Database",
			},
			expected: false,
		},
		{
			name: "name does not match missing reference",
			filter: &ManagedResource{
				Name: "my-db",
			},
			expected: false,
		},
		{
			name: "namespace does not match missing reference",
			filter: &ManagedResource{
				Namespace: "default",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.filter.Matches(event, "regarding"))
		})
	}
}

func TestManagedResource_MatchesEmptyReference(t *testing.T) {
	event := eventForReference(
		"regarding",
		"",
		"",
		"",
		"",
	)

	tests := []struct {
		name     string
		filter   *ManagedResource
		expected bool
	}{
		{
			name:     "nil receiver matches empty reference",
			filter:   nil,
			expected: true,
		},
		{
			name:     "empty filter matches empty reference",
			filter:   &ManagedResource{},
			expected: true,
		},
		{
			name: "non-empty apiVersion does not match empty reference",
			filter: &ManagedResource{
				APIVersion: "v1",
			},
			expected: false,
		},
		{
			name: "non-empty kind does not match empty reference",
			filter: &ManagedResource{
				Kind: "ConfigMap",
			},
			expected: false,
		},
		{
			name: "non-empty name does not match empty reference",
			filter: &ManagedResource{
				Name: "my-cm",
			},
			expected: false,
		},
		{
			name: "non-empty namespace does not match empty reference",
			filter: &ManagedResource{
				Namespace: "default",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.filter.Matches(event, "regarding"))
		})
	}
}
