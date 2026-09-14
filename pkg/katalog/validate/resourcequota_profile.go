// ResourceQuota Profile Validation
//
// ResourceQuota profiles are named presets that expand into a complete set of
// hard resource limits at reconcile time.
//
// Validation enforces:
//
// 1. Known profile names:
//    Allowed: small, medium, large, xlarge.
//
// 2. Profile-only usage:
//    profile cannot appear alongside an explicit hard map — profiles are
//    atomic presets.
//
// 3. Template expressions:
//    Profile values containing "{{" are skipped at load time and validated
//    at reconcile time instead.

package validate

import (
	"fmt"
	orktypes "github.com/orkspace/orkestra/pkg/types"

	"github.com/orkspace/orkestra/pkg/profiles"
)

func (e *executor) validateResourceQuotaProfiles() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		for _, entry := range crd.CollectResourceQuotaProfileEntries() {
			if orktypes.IsTemplate(entry.Profile) {
				continue
			}
			if !e.isUserResourceQuotaProfile(entry.Profile) && !profiles.IsValidResourceQuotaProfile(entry.Profile) {
				return fmt.Errorf(
					"%s crd %q: resourceQuota %q (phase %s) has unknown profile %q — "+
						"allowed: small, medium, large, xlarge",
					failureMark(), crdName, entry.ResourceName, entry.Phase, entry.Profile,
				)
			}
			if entry.Mixed {
				return fmt.Errorf(
					"%s crd %q: resourceQuota %q (phase %s) declares both profile (%q) and "+
						"explicit hard limits — use one or the other, not both",
					failureMark(), crdName, entry.ResourceName, entry.Phase, entry.Profile,
				)
			}
		}
	}
	return nil
}
