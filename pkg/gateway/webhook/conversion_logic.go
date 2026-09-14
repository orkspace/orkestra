// webhook/conversion_logic.go — CRD version conversion logic.
package webhook

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/note"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// applyConversion converts obj from its current version to targetAPIVersion.
// targetAPIVersion is the full apiVersion string from the ConversionReview request.
func applyConversion(
	obj map[string]interface{},
	rules *orktypes.ConversionRules,
	targetAPIVersion string,
) (map[string]interface{}, error) {
	sourceAPIVersion, ok := obj["apiVersion"].(string)
	if !ok || sourceAPIVersion == "" {
		return nil, fmt.Errorf("object missing apiVersion")
	}

	_, sourceVersion := splitAPIVersion(sourceAPIVersion)
	_, targetVersion := splitAPIVersion(targetAPIVersion)

	if sourceVersion == targetVersion {
		out := copyMap(obj)
		out["apiVersion"] = targetAPIVersion
		return out, nil
	}

	path := rules.FindPath(sourceVersion, targetVersion)
	if path == nil {
		return nil, fmt.Errorf(
			"no conversion path declared for %s → %s in kind %q.\n"+
				"Add a path to the Katalog:\n"+
				"  conversion:\n"+
				"    paths:\n"+
				"      - from: %s\n"+
				"        to: %s\n"+
				"        spec:\n"+
				"          # fields in %s format",
			sourceVersion, targetVersion, rules.Kind,
			sourceVersion, targetVersion, targetVersion,
		)
	}

	resolver := orktmpl.NewResolverFromMap(obj)
	convertedSpec, err := resolveMap(resolver, path.Spec)
	if err != nil {
		return nil, fmt.Errorf("converting spec from %s to %s: %w", sourceVersion, targetVersion, err)
	}

	out := copyMap(obj)
	out["apiVersion"] = targetAPIVersion
	out["spec"] = convertedSpec

	return out, nil
}

func resolveMap(r *orktmpl.Resolver, src map[string]interface{}) (map[string]interface{}, error) {
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		resolved, err := resolveValue(r, v)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		dst[k] = resolved
	}
	return dst, nil
}

func resolveValue(r *orktmpl.Resolver, v interface{}) (interface{}, error) {
	switch tv := v.(type) {
	case string:
		resolved, err := r.Resolve(tv)
		if err != nil {
			return nil, err
		}
		// cronToMap returns CronMapSentinel + JSON so that the conversion spec
		// can use {{ cronToMap .spec.schedule }} as a one-liner shorthand.
		// Detect the sentinel and parse back to map[string]interface{}.
		if strings.HasPrefix(resolved, note.CronMapSentinel) {
			payload := resolved[len(note.CronMapSentinel):]
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(payload), &m); err != nil {
				return nil, fmt.Errorf("cronToMap: invalid JSON payload: %w", err)
			}
			return m, nil
		}
		if orktypes.IsTemplate(tv) {
			if resolved == "" {
				return nil, nil
			}
			return orktypes.TryCoerceString(resolved), nil
		}
		return resolved, nil
	case map[string]interface{}:
		return resolveMap(r, tv)
	case []interface{}:
		arr := make([]interface{}, len(tv))
		for i, item := range tv {
			resolved, err := resolveValue(r, item)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			arr[i] = resolved
		}
		return arr, nil
	default:
		return v, nil
	}
}

func copyMap(src map[string]interface{}) map[string]interface{} {
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func splitAPIVersion(apiVersion string) (group, version string) {
	i := strings.LastIndex(apiVersion, "/")
	if i < 0 {
		return "", apiVersion
	}
	return apiVersion[:i], apiVersion[i+1:]
}

func joinAPIVersion(group, version string) string {
	if group == "" {
		return version
	}
	return group + "/" + version
}
