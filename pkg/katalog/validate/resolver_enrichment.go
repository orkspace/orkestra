package validate

import (
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/children"
)

// validateEnrich fails fast when:
//   - both enrichAll and enrich are set on the same CRD (mutually exclusive)
//   - an unrecognised target appears in enrich
func (e *executor) validateEnrich() error {
	for name, crd := range e.k.Enabled() {
		if crd.EnrichAll && len(crd.Enrich) > 0 {
			return fmt.Errorf(
				"%s crd %q: enrichAll and enrich are mutually exclusive — use enrichAll: true OR enrich: [...]",
				failureMark(), name,
			)
		}

		supportedGroups := children.SupportedEnrichmentGroups()

		for _, target := range crd.Enrich {
			if !children.IsValidEnrichmentTarget(target.Key) {
				return fmt.Errorf(
					"%s crd %q: unknown enrich target %q — supported targets:%s",
					failureMark(), name,
					target.Key,
					formatEnrichmentGroups(supportedGroups),
				)
			}
		}

	}
	return nil
}

// helper
func formatEnrichmentGroups(groups map[string][]string) string {
	var b strings.Builder

	for kind, list := range groups {
		b.WriteString("\n  ")
		b.WriteString(kind)
		b.WriteString(":\n    ")
		b.WriteString(strings.Join(list, ", "))
	}

	return b.String()
}
