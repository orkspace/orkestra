// Security Profile Validation
//
// Security profiles are named presets that expand into concrete security
// context fields at katalog load time. Profile and explicit fields are mutually
// exclusive — mixing them creates ambiguity and is rejected at load time.
//
// Validation enforces:
//
//  1. Profile-only usage:
//     securityContext.profile cannot appear alongside explicit security fields.
//     podSecurity.profile cannot appear alongside explicit pod security fields.
//
//  2. Known profile names:
//     Allowed: baseline, restricted, hardened.
//
//  3. Template expressions:
//     Profile values containing "{{" are skipped at load time and validated
//     at reconcile time instead.

package validate

import (
	"fmt"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"strings"

	"github.com/orkspace/orkestra/pkg/profiles"
)

// validateSecurityProfiles ensures that security profiles are used correctly
// across all template resources in every hook phase.
func (e *executor) validateSecurityProfiles() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		for _, entry := range crd.CollectSecurityProfileEntries() {
			if orktypes.IsTemplate(entry.Profile) {
				continue
			}
			_, csUser := e.k.Profiles.LookupContainerSecurity(entry.Profile)
			_, psUser := e.k.Profiles.LookupPodSecurity(entry.Profile)
			if !csUser && !psUser && !profiles.IsValidSecurityProfile(entry.Profile) {
				return fmt.Errorf(
					"%s crd %q: %s %q (phase %s) has unknown %s security profile %q — "+
						"allowed: baseline, restricted, hardened, or a user-defined profile declared in profiles.containerSecurity / profiles.podSecurity",
					failureMark(), crdName, entry.Resource, entry.ResourceName, entry.Phase, entry.Kind, entry.Profile,
				)
			}
			if entry.Mixed {
				return fmt.Errorf(
					"%s crd %q: %s %q (phase %s) declares both a %s security profile (%q) and "+
						"explicit security fields — use one or the other, not both",
					failureMark(), crdName, entry.Resource, entry.ResourceName, entry.Phase, entry.Kind, entry.Profile,
				)
			}
		}
	}
	return nil
}

// validateSecurityCapabilities checks that every capability name declared in
// securityContext.capabilities.add and .drop is a known Linux capability.
// The special value "ALL" is accepted in both lists.
// Template expressions are skipped.
func (e *executor) validateSecurityCapabilities() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		for _, entry := range crd.CollectCapabilityEntries() {
			if orktypes.IsTemplate(entry.Value) {
				continue
			}
			upper := strings.ToUpper(entry.Value)
			if upper == "ALL" {
				continue
			}
			if !isKnownLinuxCapability(upper) {
				return fmt.Errorf(
					"%s crd %q: %s %q (phase %s) capabilities.%s contains unknown capability %q — "+
						"must be a standard Linux capability name (e.g. NET_BIND_SERVICE, SYS_ADMIN) or ALL",
					failureMark(), crdName, entry.Resource, entry.ResourceName, entry.Phase, entry.Side, entry.Value,
				)
			}
		}
	}
	return nil
}

// knownLinuxCapabilities is the canonical set of Linux capability names.
// Source: include/uapi/linux/capability.h — covers capabilities up to kernel 6.x.
// https://github.com/torvalds/linux/blob/master/include/uapi/linux/capability.h
var knownLinuxCapabilities = map[string]struct{}{
	"AUDIT_CONTROL":      {},
	"AUDIT_READ":         {},
	"AUDIT_WRITE":        {},
	"BLOCK_SUSPEND":      {},
	"BPF":                {},
	"CHECKPOINT_RESTORE": {},
	"CHOWN":              {},
	"DAC_OVERRIDE":       {},
	"DAC_READ_SEARCH":    {},
	"FOWNER":             {},
	"FSETID":             {},
	"IPC_LOCK":           {},
	"IPC_OWNER":          {},
	"KILL":               {},
	"LEASE":              {},
	"LINUX_IMMUTABLE":    {},
	"MAC_ADMIN":          {},
	"MAC_OVERRIDE":       {},
	"MKNOD":              {},
	"NET_ADMIN":          {},
	"NET_BIND_SERVICE":   {},
	"NET_BROADCAST":      {},
	"NET_RAW":            {},
	"PERFMON":            {},
	"SETFCAP":            {},
	"SETGID":             {},
	"SETPCAP":            {},
	"SETUID":             {},
	"SYS_ADMIN":          {},
	"SYS_BOOT":           {},
	"SYSLOG":             {},
	"SYS_CHROOT":         {},
	"SYS_MODULE":         {},
	"SYS_NICE":           {},
	"SYS_PACCT":          {},
	"SYS_PTRACE":         {},
	"SYS_RAWIO":          {},
	"SYS_RESOURCE":       {},
	"SYS_TIME":           {},
	"SYS_TTY_CONFIG":     {},
	"WAKE_ALARM":         {},
}

func isKnownLinuxCapability(name string) bool {
	_, ok := knownLinuxCapabilities[name]
	return ok
}
