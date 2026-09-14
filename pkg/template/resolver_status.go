// pkg/template/resolver_status.go
package template

import (
	"fmt"
	"strconv"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ResolveStatusFields evaluates template expressions in declarative status field
// declarations and returns a map[string]interface{} ready to merge into the
// CR's status patch.
//
// Paths are relative to status and support dot-notation for nesting:
//
//	"phase"          → { "phase": "Running" }
//	"database.host"  → { "database": { "host": "..." } }
//	"ready"          → { "ready": "true" }
//
// The returned map is the body of the status subresource patch — it is merged
// into the existing status rather than replacing it. Fields not declared here
// are untouched.
//
// Errors in individual field expressions are collected and returned together
// so the caller sees all problems in one reconcile, not one at a time.
func (r *Resolver) ResolveStatusFields(fields []orktypes.StatusFieldSpec) (map[string]interface{}, error) {
	if len(fields) == 0 {
		return nil, nil
	}

	result := map[string]interface{}{}
	var errs []string

	for _, f := range fields {
		if f.Path == "" {
			errs = append(errs, "status field with empty path — skipped")
			continue
		}

		// ── Evaluate when: conditions ──────────────────────────────────────
		// evaluateConditions lives in this package (resolver_conditions.go).
		// r.data already includes .children.* if WithChildren was called.
		if len(f.When) > 0 && !evaluateConditions(r, f.When) {
			if f.ClearOnFalse {
				if err := setNestedStatusField(result, f.Path, ""); err != nil {
					errs = append(errs, fmt.Sprintf("status.%s: clearOnFalse: %v", f.Path, err))
				}
			}
			continue
		}

		// ── Resolve template expression ─────────────────────────────────────
		raw, err := r.Resolve(f.Value)
		if err != nil {
			errs = append(errs, fmt.Sprintf("status.%s: %v", f.Path, err))
			continue
		}

		// ── Apply type casting (default: string) ───────────────────────
		typedValue, err := castStatusValue(raw, f.Type)
		if err != nil {
			errs = append(errs, fmt.Sprintf("status.%s: %v", f.Path, err))
			continue
		}

		// ── Write into nested map ───────────────────────────────────────────
		if err := setNestedStatusField(result, f.Path, typedValue); err != nil {
			errs = append(errs, fmt.Sprintf("status.%s: %v", f.Path, err))
		}
	}

	if len(errs) > 0 {
		return result, fmt.Errorf("resolving status fields:\n  %s",
			strings.Join(errs, "\n  "))
	}

	return result, nil
}

// setNestedStatusField sets a value at a dot-notation path inside a
// map[string]interface{}, creating intermediate maps as needed.
//
// "phase"          → dst["phase"] = value
// "database.host"  → dst["database"]["host"] = value
// "a.b.c"          → dst["a"]["b"]["c"] = value
//
// If an intermediate path segment already contains a non-map value, it is
// replaced with a map. This avoids panics from type-assertion failures.
func setNestedStatusField(dst map[string]interface{}, path string, value interface{}) error {
	parts := strings.SplitN(path, ".", 2)

	if len(parts) == 1 {
		// Terminal — set directly
		dst[path] = value
		return nil
	}

	// Intermediate — recurse into or create the child map
	key := parts[0]
	rest := parts[1]

	var child map[string]interface{}
	if existing, ok := dst[key]; ok {
		child, ok = existing.(map[string]interface{})
		if !ok {
			// Existing value is a scalar — replace with map
			child = map[string]interface{}{}
		}
	} else {
		child = map[string]interface{}{}
	}

	dst[key] = child
	return setNestedStatusField(child, rest, value)
}

func castStatusValue(raw string, typ string) (interface{}, error) {
	switch strings.ToLower(typ) {
	case "", "string", "str", "default":
		return raw, nil

	case "int", "integer":
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected int, got %q: %w", raw, err)
		}
		return v, nil

	case "float":
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("expected float, got %q: %w", raw, err)
		}
		return v, nil

	case "bool", "boolean":
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("expected bool, got %q: %w", raw, err)
		}
		return v, nil

	case "auto":
		// Try int
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i, nil
		}
		// Try float
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f, nil
		}
		// Try bool
		if b, err := strconv.ParseBool(raw); err == nil {
			return b, nil
		}
		// Fallback to string
		return raw, nil

	default:
		return nil, fmt.Errorf("unknown type %q", typ)
	}
}
