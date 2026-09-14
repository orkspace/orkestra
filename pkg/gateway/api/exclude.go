package api

import (
	"encoding/json"
	"strings"

	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ApplyExclusions strips excluded paths from a response object.
// Called at the resource GET/list level, not during payload evaluation.
//
// This mutates the response map in-place, removing any paths listed in
// serve.config.response.exclude.
//
// Each entry in the exclude list is a template expression that can resolve to:
//   - A single path: "metadata.managedFields"
//   - A comma-separated string: "metadata.managedFields,status.observedGeneration"
//   - A list from toList: {{ toList (getAnnotation . "exclude-fields") }}
//
// The function handles all three cases.
func ApplyExclusions(
	response map[string]interface{},
	crd *orktypes.CRDEntry,
	alias string,
	notes orktypes.NoteRegistry,
) {
	if crd == nil {
		return
	}
	cfg := crd.ServeResponseConfigFor(alias)
	if cfg == nil || !cfg.HasExclude() {
		return
	}

	resolver := orktmpl.NewResolverFromMap(response).WithUserNotes(notes)

	for _, expr := range cfg.Exclude {
		if expr == "" {
			continue
		}

		resolved, err := resolver.Resolve(expr)
		if err != nil {
			continue // silent — exclusion failures never break the response
		}

		// resolved is a string because Resolve returns string.
		// But toList returns a []string that gets converted to a string
		// representation like "[metadata.managedFields status.observedGeneration]".
		//
		// Handle both cases:
		//   1. Comma-separated string: "a,b,c"
		//   2. JSON array string: "[a b c]" or "[a, b, c]"
		//   3. Single path: "a"
		//
		// We need to parse the resolved string into a list of paths.

		var paths []string

		// Try to parse as JSON array
		trimmed := strings.TrimSpace(resolved)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			var list []string
			if err := json.Unmarshal([]byte(trimmed), &list); err == nil {
				paths = list
			} else {
				// If JSON parsing fails, it might be a Go string representation
				// like "[a b c]" — remove brackets and split by space
				content := strings.Trim(trimmed, "[]")
				if content != "" {
					for _, p := range strings.Split(content, " ") {
						p = strings.TrimSpace(p)
						if p != "" {
							paths = append(paths, p)
						}
					}
				}
			}
		} else {
			// Comma-separated or single path
			for _, p := range strings.Split(trimmed, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					paths = append(paths, p)
				}
			}
		}

		// Apply all resolved paths
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p != "" {
				deleteNestedPath(response, p)
			}
		}
	}
}
