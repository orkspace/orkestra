// NetworkPolicy Profile Validation
//
// NetworkPolicy profiles are named presets that expand into a complete set of
// ingress/egress rules and policy types at reconcile time.
//
// Validation enforces:
//
// 1. Known profile names:
//    Allowed: deny-all, deny-all-ingress, deny-all-egress,
//             allow-same-namespace, allow-dns-egress.
//
// 2. Profile-only usage:
//    profile cannot appear alongside explicit ingress, egress, or policyTypes
//    fields — profiles are atomic presets.
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

func (e *executor) validateNetworkPolicyProfiles() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		for _, entry := range crd.CollectNetworkPolicyProfileEntries() {
			if orktypes.IsTemplate(entry.Profile) {
				continue
			}
			if !e.isUserNetworkPolicyProfile(entry.Profile) && !profiles.IsValidNetworkPolicyProfile(entry.Profile) {
				return fmt.Errorf(
					"%s crd %q: networkPolicy %q (phase %s) has unknown profile %q — "+
						"allowed: deny-all, deny-all-ingress, deny-all-egress, allow-same-namespace, allow-dns-egress",
					failureMark(), crdName, entry.ResourceName, entry.Phase, entry.Profile,
				)
			}
			if entry.Mixed {
				return fmt.Errorf(
					"%s crd %q: networkPolicy %q (phase %s) declares both profile (%q) and "+
						"explicit ingress/egress/policyTypes — use one or the other, not both",
					failureMark(), crdName, entry.ResourceName, entry.Phase, entry.Profile,
				)
			}
		}
	}
	return nil
}
