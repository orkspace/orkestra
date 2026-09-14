// pkg/reconciler/run_mutation.go
package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/metrics"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// MutationResult holds the outcome of applying mutation rules.
type MutationResult struct {
	Applied int
	Changes []MutationChange
}

// MutationChange describes one field mutation that was applied.
type MutationChange struct {
	Field    string
	OldValue string
	NewValue interface{}
	Type     string // "default" or "override"
}

// runMutation applies mutation rules to the CR and patches it if any values changed.
//
// Rule semantics:
//   - default:  set the field only when it is absent or empty in the CR
//   - override: always set the field, regardless of current value
//
// Type preservation:
//
//	YAML `default: 2`    → int64(2)  in patch → API server receives integer
//	YAML `default: "2"`  → string    in patch → use only for string fields
//	YAML `default: true` → bool      in patch → API server receives boolean
//
//	Template expressions (containing "{{") always produce strings — use them
//	only on string fields. For typed fields (int, bool), use literal YAML values.
//
// Mutation failures are non-fatal — the reconcile continues and the next cycle
// retries. The object is not patched if no rules produce a change.
func runMutation(
	ctx context.Context,
	kube kubeclient.Interface,
	obj domain.Object,
	resolver *orktmpl.Resolver,
	cfg *orktypes.MutationConfig,
	gvr schema.GroupVersionResource,
	crdName string,
) (*MutationResult, error) {
	result := &MutationResult{}

	if cfg == nil || len(cfg.Rules) == 0 {
		return result, nil
	}

	data := resolver.Data()
	var err error

	// patch accumulates all field changes.
	// Keys are the top-level field names (e.g. "spec").
	// setNestedPatch builds the nested structure from dot-notation paths.
	patch := map[string]interface{}{}
	hasPatch := false

	for _, rule := range cfg.Rules {
		if !rule.Fires.FiresAtReconcile() {
			continue
		}
		if !orktypes.EvaluateConditions(data, rule.When, rule.Or, resolver.TemplateEvaluator()) {
			continue
		}

		// Resolve template expression in the field path so notes and CR fields
		// can be used to target dynamic paths (e.g. "spec.{{ container }}.image").
		targetField := rule.Field
		if orktypes.IsTemplate(targetField) {
			if resolved, err := resolver.Resolve(targetField); err == nil {
				targetField = resolved
			}
		}

		// currentVal is the string representation of the current field value.
		// ResolveScalarField uses ScalarToString — integers come back as "2", bools as "true".
		currentVal, found := orktypes.ResolveScalarField(data, targetField)

		// ── Determine the mutation type and raw desired value ─────────────────
		// rawVal preserves the YAML-native type (int64, bool, string).
		// mutationType is recorded for metrics and change log.
		var rawVal interface{}
		var mutationType string

		rawVal, mutationType, err = ResolveRuleValue(rule, found, currentVal, resolver)
		if err != nil {
			return nil, fmt.Errorf("mutation: field %q: %w", targetField, err)
		}
		if rawVal == nil {
			continue // rule did not apply (default with existing value, or no value declared)
		}

		// ── Skip if unchanged ─────────────────────────────────────────────────
		// Compare as strings — currentVal is already a string from resolveField.
		// fmt.Sprintf("%v", rawVal) gives "2" for int64(2), "true" for bool(true).
		if fmt.Sprintf("%v", rawVal) == currentVal {
			continue
		}

		// ── Accumulate patch ──────────────────────────────────────────────────
		setNestedPatch(patch, targetField, rawVal)
		hasPatch = true

		result.Changes = append(result.Changes, MutationChange{
			Field:    targetField,
			OldValue: currentVal,
			NewValue: rawVal,
			Type:     mutationType,
		})

		metrics.RecordMutationFieldDetail(crdName, targetField, mutationType)

		logger.Debug().
			Str("crd", crdName).
			Str("name", obj.GetName()).
			Str("field", targetField).
			Str("old", currentVal).
			Str("new", fmt.Sprintf("%v", rawVal)).
			Str("type", mutationType).
			Msg("mutation: rule applied")
	}

	if !hasPatch {
		return result, nil
	}

	// ── Apply patch ───────────────────────────────────────────────────────────
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("mutation: marshalling patch: %w", err)
	}

	ns := obj.GetNamespace()
	_, patchErr := kube.DynamicClient().
		Resource(gvr).
		Namespace(ns).
		Patch(ctx, obj.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{})

	if patchErr != nil {
		if errors.IsConflict(patchErr) {
			logger.Debug().
				Str("crd", crdName).
				Str("name", obj.GetName()).
				Msg("mutation: resource version conflict — will retry on next reconcile")
			return result, nil
		}
		return nil, fmt.Errorf("mutation: patching %s/%s: %w", ns, obj.GetName(), patchErr)
	}

	result.Applied = len(result.Changes)
	metrics.RecordMutationTotal(crdName)

	logger.Info().
		Str("crd", crdName).
		Str("name", obj.GetName()).
		Int("fieldsChanged", result.Applied).
		Msg("mutation: rules applied")

	return result, nil
}

// resolveRuleValue determines the value to set for one mutation rule.
//
// Returns:
//   - rawVal:       the typed value to write (int64, bool, string, or nil)
//   - mutationType: "default" or "override" for metrics
//   - err:          non-nil if a template expression failed to resolve
//
// Returns (nil, "", nil) when the rule does not apply — caller should skip.
func ResolveRuleValue(
	rule orktypes.MutationRule,
	found bool,
	currentVal string,
	resolver *orktmpl.Resolver,
) (rawVal interface{}, mutationType string, err error) {

	switch {
	case rule.Override != nil:
		// Override — always apply, regardless of current value
		mutationType = "override"
		rawVal, err = resolveTypedValue(rule.Override, rule.ValueType, resolver)

	case rule.Default != nil:
		// Default — apply only when the field is absent or empty
		if found && currentVal != "" {
			return nil, "", nil // field already has a value — skip
		}
		mutationType = "default"
		rawVal, err = resolveTypedValue(rule.Default, rule.ValueType, resolver)

	default:
		return nil, "", nil // rule declares neither Default nor Override
	}

	return rawVal, mutationType, err
}

// resolveTypedValue converts a mutation rule value to the desired type.
//
// valueType can be "int", "bool", "float", "string", or empty (defaults to "string").
// The value may be a literal YAML type (int64, bool, string) or a template result (always string).
//
// Returns the typed value suitable for JSON patch, or an error if conversion fails.
func resolveTypedValue(val interface{}, valueType string, resolver *orktmpl.Resolver) (interface{}, error) {
	// First resolve template if needed
	strVal, isStr := val.(string)
	if isStr && orktypes.IsTemplate(strVal) {
		resolved, err := resolver.Resolve(strVal)
		if err != nil {
			return nil, fmt.Errorf("resolving template expression %q: %w", strVal, err)
		}
		val = resolved
		// Template result is always a string; we will convert later according to valueType.
	}

	// Apply type conversion based on valueType
	switch valueType {
	case "int", "integer":
		switch v := val.(type) {
		case int64:
			return v, nil
		case int:
			return int64(v), nil
		case float64:
			return int64(v), nil
		case string:
			i, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("cannot convert %q to int64: %w", v, err)
			}
			return i, nil
		default:
			return nil, fmt.Errorf("cannot convert %T to int64", val)
		}

	case "bool", "boolean":
		switch v := val.(type) {
		case bool:
			return v, nil
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("cannot convert %q to bool: %w", v, err)
			}
			return b, nil
		default:
			return nil, fmt.Errorf("cannot convert %T to bool", val)
		}

	case "float", "number":
		switch v := val.(type) {
		case float64:
			return v, nil
		case int64:
			return float64(v), nil
		case int:
			return float64(v), nil
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("cannot convert %q to float64: %w", v, err)
			}
			return f, nil
		default:
			return nil, fmt.Errorf("cannot convert %T to float64", val)
		}

	default: // "string" or empty
		// Return as string – convert any non-string to its string representation
		if s, ok := val.(string); ok {
			return s, nil
		}
		return fmt.Sprintf("%v", val), nil
	}
}

// setNestedPatch sets a value at a dot-notation path inside a patch map,
// creating intermediate maps as needed.
//
// "spec.replicas" with int64(2)   → {"spec": {"replicas": 2}}
// "spec.port"     with int64(8080) → {"spec": {"port": 8080}}
// "spec.env"      with "production" → {"spec": {"env": "production"}}
//
// The native Go type is preserved — JSON marshal produces the correct
// JSON type for the Kubernetes API server.
func setNestedPatch(patch map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	current := patch

	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value // preserve native type
			return
		}
		if _, ok := current[part]; !ok {
			current[part] = map[string]interface{}{}
		}
		next, ok := current[part].(map[string]interface{})
		if !ok {
			// Existing value is not a map — replace with map
			current[part] = map[string]interface{}{}
			next = current[part].(map[string]interface{})
		}
		current = next
	}
}
