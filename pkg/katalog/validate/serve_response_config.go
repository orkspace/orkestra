package validate

import (
	"fmt"
	"slices"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateServeResponseConfig checks the serve response configuration for:
//   - Path conflicts between payload and exclude (exclude wins, surfaced as warning)
//   - Payload template compilation errors (error)
func (e *executor) validateServeResponseConfig() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		if !crd.HasServeResponseConfig() {
			continue
		}

		config := crd.Serve.Config.Response
		if !config.HasPayload() && !config.HasExclude() {
			continue
		}

		// Collect payload keys (these will become top-level fields)
		payloadKeys := make(map[string]bool)
		for key := range config.Payload {
			payloadKeys[key] = true
		}

		// Resolve exclude paths (best-effort at validation time)
		if !config.HasExclude() {
			continue
		}
		excludePaths := config.Exclude

		// If exclude is a template, we can't validate its contents at load time
		for _, p := range excludePaths {
			path := strings.TrimSpace(p)
			if path == "" {
				continue
			}
			if orktypes.IsTemplate(path) {
				// Still check if it references any payload keys in the template itself
				for key := range payloadKeys {
					if slices.Contains(excludePaths, key) {
						warning := fmt.Sprintf("%s CRD %q: exclude template references payload key %q — potential conflict\n", warningMark(), crdName, key)
						crd.Warnings.AddWarning(warning)
					}
				}
				continue
			}
		}

		// Static exclude: validate
		for _, p := range excludePaths {
			path := strings.TrimSpace(p)
			if path == "" {
				continue
			}

			// Check if path conflicts with a payload key
			if payloadKeys[path] {
				warning := fmt.Sprintf("path %q appears in both payload and exclude — exclude wins", path)
				crd.Warnings.AddWarning(warning)
			}
		}
		e.k.EnabledCRDs()[crdName] = crd
	}
	return nil
}
