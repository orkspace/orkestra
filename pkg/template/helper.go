package template

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/orkspace/orkestra/domain"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ResolveMap evaluates template expressions in each value of a map[string]string —
// used for labels, annotations, and match selectors alike.
// Keys are never template expressions — only values are resolved.
//
// Example:
//
//	name: {{ .metadata.name }}
//	app: {{ .metadata.labels.app }}
func (r *Resolver) ResolveMap(m map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(m))
	for k, v := range m {
		v, err := r.Resolve(v)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", k, err)
		}
		resolved[k] = v
	}
	return resolved, nil
}

// ResolveStringSlice resolves template expressions in each element of a string slice.
// Each element is resolved independently — one failing element does not affect others.
// Used for toNamespaces where each namespace may be a template expression.
//
// Example:
//
//	input:  ["{{ .metadata.namespace }}", "monitoring", "{{ .spec.extraNamespace }}"]
//	output: ["my-app", "monitoring", "platform"]
func (r *Resolver) ResolveStringSlice(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}

	resolved := make([]string, 0, len(values))
	for i, v := range values {
		rv, err := r.Resolve(v)
		if err != nil {
			return nil, fmt.Errorf("index %d: %w", i, err)
		}
		// Skip empty results — a template that resolves to "" means
		// the field was not set on the CR. This avoids not creating a namespace named "".
		if rv != "" {
			resolved = append(resolved, rv)
		}
	}
	return resolved, nil
}

// ResolveEnvFromRefs resolves Name/Prefix/Suffix template expressions in each
// EnvFromRef. Keys and Optional are not template expressions — copied as-is.
func (r *Resolver) ResolveEnvFromRefs(refs []orktypes.EnvFromRef, field string) ([]orktypes.EnvFromRef, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	resolved := make([]orktypes.EnvFromRef, 0, len(refs))
	for _, ref := range refs {
		name, err := r.Resolve(ref.Name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		prefix, err := r.Resolve(ref.Prefix)
		if err != nil {
			return nil, fmt.Errorf("%s: prefix: %w", field, err)
		}
		suffix, err := r.Resolve(ref.Suffix)
		if err != nil {
			return nil, fmt.Errorf("%s: suffix: %w", field, err)
		}
		resolved = append(resolved, orktypes.EnvFromRef{
			Name:     name,
			Prefix:   prefix,
			Suffix:   suffix,
			Optional: ref.Optional,
			Keys:     ref.Keys,
		})
	}
	return resolved, nil
}

// ── Internal ──────────────────────────────────────────────────────────────────

// objectToMap converts any domain.Object to map[string]interface{} for template execution.
//
// Two paths, one result:
//
//	*unstructured.Unstructured — already a complete map. Used directly with no
//	allocation. This is the common path for all declarative (default: true) operators.
//
//	Typed objects — marshaled to JSON and back. The JSON round-trip uses the
//	struct's json tags to produce the same map shape as the unstructured path:
//	spec fields, status fields, and metadata all accessible as .spec.*, .status.*,
//	.metadata.* in templates.
//
// The JSON round-trip on typed objects was the key insight that unified the two modes.
// Template expressions, conditions, status fields, and cross-CRD reads all work
// identically regardless of whether the operator uses typed or unstructured mode.
// The 5% of patterns that still require Go hooks do so because of business logic
// complexity — not because of any templating limitation.
func objectToMap(obj domain.Object) (map[string]interface{}, error) {
	// Unstructured — already a map, use directly
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return u.Object, nil
	}

	// Typed — marshal to JSON, unmarshal to map.
	// This gives the complete object — spec, status, metadata — as
	// map[string]interface{}, exactly like unstructured mode.
	// The JSON round-trip preserves all field names from json struct tags.
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("objectToMap: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("objectToMap: %w", err)
	}
	return result, nil
}

// resolveRawValue navigates a dot-notation path extracted from a template
// expression and returns the raw value — preserving the original type
// ([]interface{}, map, string, float64, bool) rather than stringifying it.
//
// Input: "{{ .spec.targetNamespaces }}" → navigates data["spec"]["targetNamespaces"]
// Returns nil when the path does not exist.
func resolveRawValue(data map[string]interface{}, expr string) interface{} {
	// Extract the path from "{{ .spec.targetNamespaces }}"
	path := strings.TrimSpace(expr)
	path = strings.TrimPrefix(path, "{{")
	path = strings.TrimSuffix(path, "}}")
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, ".")

	if path == "" {
		return nil
	}

	parts := strings.Split(path, ".")
	var current interface{} = data
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}
	return current
}

// ValidResolverName reports whether name is a valid identifier that the
// resolver can interpret. A valid name:
//   - is non-empty
//   - starts with a letter or underscore
//   - contains only letters, digits, and underscores
//
// This matches Go's identifier rules, which is what the template engine
// uses to resolve `{{ .events.<name> }}` expressions.
func ValidResolverName(name string) error {
	if name == "" {
		return fmt.Errorf("event name must not be empty")
	}

	for i, r := range name {
		switch {
		case unicode.IsLetter(r):
			// ok
		case r == '_':
			// ok
		case unicode.IsDigit(r) && i > 0:
			// ok
		default:
			return fmt.Errorf(
				"event name %q contains invalid character %q at position %d; must be a valid identifier (letters, digits, underscores; cannot start with a digit)",
				name, r, i,
			)
		}
	}

	return nil
}
