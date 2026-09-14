// LimitRange Profile Validation
//
// LimitRange profiles are user-defined named presets that expand into a
// list of LimitRangeItems at reconcile time. There are no built-in presets —
// every limitRange profile must be declared in the katalog or an imported motif.
//
// Validation enforces:
//
// 1. Known profile names:
//    Profile must appear in profiles.limitRanges or an imported motif.
//
// 2. Profile-only usage:
//    profile cannot appear alongside an explicit limits list.
//
// 3. Template expressions:
//    Profile values containing "{{" are skipped at load time.

package validate

import (
	"fmt"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

func (e *executor) validateLimitRangeProfiles() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		for _, entry := range crd.CollectLimitRangeProfileEntries() {
			if orktypes.IsTemplate(entry.Profile) {
				continue
			}
			if !e.isUserLimitRangeProfile(entry.Profile) {
				return fmt.Errorf(
					"%s crd %q: LimitRange %q (phase %s) has unknown profile %q — "+
						"define it in profiles.limitRanges",
					failureMark(), crdName, entry.ResourceName, entry.Phase, entry.Profile,
				)
			}
			if entry.Mixed {
				return fmt.Errorf(
					"%s crd %q: LimitRange %q (phase %s) declares both profile (%q) and "+
						"explicit limits — use one or the other, not both",
					failureMark(), crdName, entry.ResourceName, entry.Phase, entry.Profile,
				)
			}
		}
	}
	return nil
}
