// pkg/reconciler/normalize.go
//
// applyNormalize — canonical spec transformation.
//
// Called as the first step of reconcileImpl, before mutation and validation.
// Returns a deep copy of the CR with normalize.spec fields replaced by their
// rendered values. The informer cache object is never modified.
//
// Pipeline position:
//
//	informer cache → DeepCopy → applyNormalize → mutation → validation → runTemplateReconcile
//
// After applyNormalize, every downstream step sees the normalized spec.
// The raw CR in etcd is unchanged — normalize is purely an in-memory operation.
//
// Example — CronJob accepting both string and map schedule:
//
//	normalize:
//	  spec:
//	    schedule: >
//	      {{ if eq (typeOf .spec.schedule) "map" }}
//	        {{ cronExpr .spec.schedule.minute .spec.schedule.hour .spec.schedule.dayOfMonth .spec.schedule.month .spec.schedule.dayOfWeek }}
//	      {{ else }}
//	        {{ .spec.schedule }}
//	      {{ end }}
//
// After normalize, .spec.schedule is always a cron string.
// onReconcile templates use {{ .spec.schedule }} directly — no branching needed.
package reconciler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"gopkg.in/yaml.v3"
)

// applyNormalize applies the CRD's normalize.spec templates to a deep copy
// of the CR, returning the normalized copy for all downstream reconcile steps.
//
// Returns the original obj unchanged when:
//   - r.crd.Normalize is nil (no normalize block declared)
//   - r.crd.Normalize.Spec is empty
//
// Returns an error when:
//   - Template resolution fails (bad expression)
//   - Path navigation fails (segment is not a map)
//
// The returned object is always a deep copy — safe to mutate.
func (r *GenericReconciler[PTR]) applyNormalize(
	ctx context.Context,
	obj PTR,
) (PTR, *orktmpl.Resolver, []orktypes.NormalizeChange, error) {
	// No normalize block → just build a resolver and return
	if r.crd.Normalize == nil || len(r.crd.Normalize.Spec) == 0 {
		baseResolver, err := orktmpl.NewResolver(ctx, obj)
		if err != nil {
			return obj, nil, nil, fmt.Errorf("normalize: building base resolver: %w", err)
		}
		return obj, baseResolver, nil, nil
	}

	log := logger.FromContext(ctx)

	cloned := obj.DeepCopyObject().(PTR)

	// Resolver over pre-normalize spec (for template evaluation)
	baseResolver, err := orktmpl.NewResolver(ctx, cloned)
	if err != nil {
		return obj, nil, nil, fmt.Errorf("normalize: building resolver: %w", err)
	}

	type unstructuredGetter interface {
		UnstructuredContent() map[string]interface{}
	}
	ug, ok := any(cloned).(unstructuredGetter)
	if !ok {
		log.Debug().Msg("normalize: skipping — object is typed mode, normalize requires dynamic mode")
		return cloned, baseResolver, nil, nil
	}
	content := ug.UnstructuredContent()
	if content == nil {
		content = map[string]interface{}{}
	}

	audit := r.crd.Normalize.Audit
	var changes []orktypes.NormalizeChange

	for fieldPath, tpl := range r.crd.Normalize.Spec {
		rendered, err := baseResolver.Resolve(tpl)
		if err != nil {
			return obj, nil, nil, fmt.Errorf("normalize spec.%s: %w", fieldPath, err)
		}
		rendered = strings.TrimSpace(rendered)

		var parsed interface{}
		if declaredType, ok := r.crd.Normalize.Types[fieldPath]; ok {
			parsed, err = coerceNormalizedValue(rendered, declaredType)
			if err != nil {
				return obj, nil, nil, fmt.Errorf("normalize spec.%s: type %q: %w", fieldPath, declaredType, err)
			}
		} else {
			parsed = parseNormalizedValue(rendered)
		}

		if parsed == nil {
			log.Debug().Str("field", fieldPath).Msg("normalize: field omitted (nil)")
			continue
		}

		fullPath := "spec." + fieldPath
		var before interface{}
		if audit {
			before = nestedGet(content, fullPath)
		}
		if err := setNestedNormalized(content, fullPath, parsed); err != nil {
			return obj, nil, nil, fmt.Errorf("normalize spec.%s: setting field: %w", fieldPath, err)
		}
		if audit && fmt.Sprintf("%v", before) != fmt.Sprintf("%v", parsed) {
			changes = append(changes, orktypes.NormalizeChange{
				Field: fieldPath,
				From:  before,
				To:    parsed,
			})
		}
		log.Debug().
			Str("field", fieldPath).
			Str("rendered", rendered).
			Msg("normalize: field normalized")
	}

	normalized := cloned

	// Resolver over the normalized spec
	normalizedResolver, err := orktmpl.NewResolver(ctx, normalized)
	if err != nil {
		return obj, nil, nil, fmt.Errorf("normalize: building normalized resolver: %w", err)
	}

	return normalized, normalizedResolver, changes, nil
}

// parseNormalizedValue converts a rendered template string to the most
// appropriate Go type using YAML parsing rules.
//
// YAML type coercion:
//
//	""         → "" (empty string, not nil)
//	"3"        → int (64-bit) via yaml.Unmarshal
//	"3.14"     → float64
//	"true"     → bool
//	"null"     → nil (treated as empty string to avoid nil surprises)
//	"*/5 * * * *" → string (YAML cannot parse this as a number or bool)
//
// For cron strings specifically: YAML will not misparse them because they
// contain characters (*, /) that are not valid in YAML scalars for numbers.
func parseNormalizedValue(s string) interface{} {
	if s == "" {
		return ""
	}

	// Try YAML unmarshal — handles int, float, bool, null cleanly
	var v interface{}
	if err := yaml.Unmarshal([]byte(s), &v); err == nil && v != nil {
		// Guard: yaml unmarshals plain integers as int — convert to int64
		// for consistency with the unstructured map's expected types
		switch t := v.(type) {
		case int:
			return int64(t)
		case nil:
			return "" // treat null as empty string
		default:
			return t
		}
	}

	// YAML parsing failed or returned nil — treat as plain string
	return s
}

// coerceNormalizedValue casts a rendered template string to the declared type.
// Returns nil when the rendered string is empty — the field is omitted rather
// than written as an empty string, which would fail integer/boolean schema validation.
//
// Accepted types: "int", "integer", "bool", "boolean", "float", "number", "string".
func coerceNormalizedValue(rendered, typ string) (interface{}, error) {
	if rendered == "" {
		return nil, nil
	}
	switch strings.ToLower(typ) {
	case "int", "integer":
		i, err := strconv.ParseInt(rendered, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot convert %q to int: %w", rendered, err)
		}
		return i, nil
	case "bool", "boolean":
		b, err := strconv.ParseBool(rendered)
		if err != nil {
			return nil, fmt.Errorf("cannot convert %q to bool: %w", rendered, err)
		}
		return b, nil
	case "float", "number":
		f, err := strconv.ParseFloat(rendered, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot convert %q to float: %w", rendered, err)
		}
		return f, nil
	case "string", "":
		return rendered, nil
	default:
		return nil, fmt.Errorf("unknown type %q — use int, bool, float, or string", typ)
	}
}

// setNestedNormalized writes a value at a dot-notation path through a
// map[string]interface{}, creating intermediate maps as needed.
//
// "spec.schedule"              → obj["spec"]["schedule"] = value
// "spec.resources.limits.cpu" → obj["spec"]["resources"]["limits"]["cpu"] = value
//
// Returns an error when an intermediate segment is not a map.
func setNestedNormalized(obj map[string]interface{}, path string, value interface{}) error {
	parts := splitDotPath(path)
	if len(parts) == 0 {
		return fmt.Errorf("empty path")
	}

	current := obj
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		next, ok := current[part]
		if !ok {
			// Create missing intermediate map
			newMap := map[string]interface{}{}
			current[part] = newMap
			current = newMap
			continue
		}
		nextMap, ok := next.(map[string]interface{})
		if !ok {
			return fmt.Errorf("path segment %q is not a map (got %T)", part, next)
		}
		current = nextMap
	}

	current[parts[len(parts)-1]] = value
	return nil
}

// nestedGet reads the value at a dot-notation path through a
// map[string]interface{}. Returns nil when any segment is missing or not a map.
func nestedGet(obj map[string]interface{}, path string) interface{} {
	parts := splitDotPath(path)
	current := obj
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part]
		if !ok {
			return nil
		}
		m, ok := next.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m
	}
	return current[parts[len(parts)-1]]
}

// splitDotPath splits a dot-notation path into segments.
// "spec.schedule.minute" → ["spec", "schedule", "minute"]
func splitDotPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			parts = append(parts, path[start:i])
			start = i + 1
		}
	}
	return append(parts, path[start:])
}
