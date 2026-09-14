package utils

import (
	"fmt"
	"strings"
)

// NestedSlice navigates a map[string]interface{} using dot-notation keys
// and returns the final slice value.
// Returns nil, false if any key in the path is missing or not a map.
func NestedSlice(obj map[string]interface{}, keys ...string) ([]interface{}, bool) {
	cur := obj
	for i, k := range keys {
		if i == len(keys)-1 {
			v, ok := cur[k].([]interface{})
			return v, ok
		}
		next, ok := cur[k].(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// NestedMap navigates a map[string]interface{} using dot-notation keys
// and returns the final map value.
// Returns nil, false if any key in the path is missing or not a map.
func NestedMap(obj map[string]interface{}, keys ...string) (map[string]interface{}, bool) {
	if len(keys) == 0 {
		return nil, false
	}
	cur := obj
	for _, k := range keys {
		next, ok := cur[k].(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// DeepCopyMap returns a shallow-to-one-level deep copy of a map[string]interface{}.
// Nested maps are also copied; slices and scalar values share the same pointer.
// Sufficient for our use case: we only modify top-level keys and nested map keys
// via deleteNestedPath — we never mutate slice elements or scalar values.
func DeepCopyMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		if nested, ok := v.(map[string]interface{}); ok {
			dst[k] = DeepCopyMap(nested)
		} else {
			dst[k] = v
		}
	}
	return dst
}

// SetNestedPath sets a value at a dot-notation path in a map.
// Creates intermediate maps as needed.
//
// Example:
//
//	SetNestedPath(spec, "app.repository", "myorg/payments-api")
//	→ spec["app"]["repository"] = "myorg/payments-api"
func SetNestedPath(m map[string]interface{}, path string, value interface{}) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}

	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return fmt.Errorf("empty path")
	}

	// Navigate to the parent
	current := m
	for i := 0; i < len(parts)-1; i++ {
		key := parts[i]
		if _, ok := current[key]; !ok {
			current[key] = make(map[string]interface{})
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			return fmt.Errorf("path segment %q is not a map (cannot set nested value)", key)
		}
		current = next
	}

	// Set the value at the final key
	current[parts[len(parts)-1]] = value
	return nil
}

// GetNestedPath retrieves a value from a dot-notation path in a map.
// Returns nil, false if the path doesn't exist.
func GetNestedPath(m map[string]interface{}, path string) (interface{}, bool) {
	if path == "" {
		return nil, false
	}

	parts := strings.Split(path, ".")
	current := m
	for i, key := range parts {
		val, ok := current[key]
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			return val, true
		}
		next, ok := val.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = next
	}
	return nil, false
}

// DeleteNestedPath removes a dot-notation path from a nested map in place.
// Silently does nothing when the path does not exist — partial paths are not
// errors. Supports arbitrary depth: "metadata.managedFields",
// "status.observedGeneration", "metadata.annotations.internal-key".
func DeleteNestedPath(obj map[string]interface{}, path string) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) == 0 || obj == nil {
		return
	}

	key := parts[0]
	if len(parts) == 1 {
		// Leaf — delete this key.
		delete(obj, key)
		return
	}

	// Intermediate — recurse into the nested map if it exists.
	if nested, ok := obj[key].(map[string]interface{}); ok {
		DeleteNestedPath(nested, parts[1])
	}
}

// IsNestedPath returns true if the path contains a dot.
func IsNestedPath(path string) bool {
	return strings.Contains(path, ".")
}

// SplitPath splits a dot-notation path into its parts, ignoring empty segments.
func SplitPath(path string) []string {
	var parts []string
	for _, p := range strings.Split(path, ".") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func SetFieldPath(obj map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	current := obj

	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		if _, ok := current[part]; !ok {
			current[part] = map[string]interface{}{}
		}
		next, ok := current[part].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[part] = next
		}
		current = next
	}
}
