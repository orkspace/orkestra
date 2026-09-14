package validate

import (
	"fmt"

	"github.com/orkspace/orkestra/pkg/profiles"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateUserProfiles checks the profiles: block declared on the katalog.
//
// Enforces:
//  1. No duplicate names within a class.
//  2. Warns (does not error) when a user profile shadows a built-in name.
//  3. Each profile entry must have a non-empty name.
func (e *executor) validateUserProfiles() error {
	reg := e.k.Profiles
	if reg.Empty() {
		return nil
	}

	type check struct {
		class     string
		names     []string
		isBuiltin func(string) bool
	}

	checks := []check{
		{
			class:     "networkPolicies",
			names:     npDefNames(reg.NetworkPolicies),
			isBuiltin: profiles.IsValidNetworkPolicyProfile,
		},
		{
			class:     "resourceQuotas",
			names:     rqDefNames(reg.ResourceQuotas),
			isBuiltin: profiles.IsValidResourceQuotaProfile,
		},
		{
			class:     "limitRanges",
			names:     lrDefNames(reg.LimitRanges),
			isBuiltin: nil,
		},
		{
			class:     "hpa",
			names:     hpaDefNames(reg.HPA),
			isBuiltin: profiles.IsValidHPAProfile,
		},
		{
			class:     "pdb",
			names:     pdbDefNames(reg.PDB),
			isBuiltin: profiles.IsValidPDBProfile,
		},
		{
			class:     "rollingUpdate",
			names:     ruDefNames(reg.RollingUpdate),
			isBuiltin: profiles.IsValidRollingUpdateProfile,
		},
		{
			class:     "reconciler",
			names:     reconcilerDefNames(reg.Reconciler),
			isBuiltin: profiles.IsValidReconcilerProfile,
		},
		{
			class:     "resources",
			names:     resourceDefNames(reg.Resources),
			isBuiltin: profiles.IsValidResourceProfile,
		},
		{
			class:     "probes",
			names:     probeDefNames(reg.Probes),
			isBuiltin: profiles.IsValidProbeProfile,
		},
		{
			class:     "containerSecurity",
			names:     containerSecurityDefNames(reg.ContainerSecurity),
			isBuiltin: profiles.IsValidSecurityProfile,
		},
		{
			class:     "podSecurity",
			names:     podSecurityDefNames(reg.PodSecurity),
			isBuiltin: profiles.IsValidSecurityProfile,
		},
	}

	for _, c := range checks {
		seen := make(map[string]bool, len(c.names))
		for _, name := range c.names {
			if name == "" {
				return fmt.Errorf("%s profiles.%s: profile entry is missing a name", failureMark(), c.class)
			}
			if seen[name] {
				return fmt.Errorf("%s profiles.%s: duplicate profile name %q — names must be unique within a class", failureMark(), c.class, name)
			}
			seen[name] = true
			if c.isBuiltin != nil && c.isBuiltin(name) {
				warning := fmt.Sprintf("profiles.%s %q shadows a built-in Orkestra profile — the user-defined version will be used instead",
					c.class, name)
				e.k.Warnings.AddWarning(warning)
			}
		}
	}
	return nil
}

// isUserNetworkPolicyProfile reports whether name is in the katalog's user registry.
func (e *executor) isUserNetworkPolicyProfile(name string) bool {
	_, found := e.k.Profiles.LookupNetworkPolicy(name)
	return found
}
func (e *executor) isUserResourceQuotaProfile(name string) bool {
	_, found := e.k.Profiles.LookupResourceQuota(name)
	return found
}
func (e *executor) isUserLimitRangeProfile(name string) bool {
	_, found := e.k.Profiles.LookupLimitRange(name)
	return found
}
func (e *executor) isUserHPAProfile(name string) bool {
	_, found := e.k.Profiles.LookupHPA(name)
	return found
}
func (e *executor) isUserPDBProfile(name string) bool {
	_, found := e.k.Profiles.LookupPDB(name)
	return found
}
func (e *executor) isUserRollingUpdateProfile(name string) bool {
	_, found := e.k.Profiles.LookupRollingUpdate(name)
	return found
}
func npDefNames(defs []orktypes.NetworkPolicyProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
func rqDefNames(defs []orktypes.ResourceQuotaProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
func lrDefNames(defs []orktypes.LimitRangeProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
func hpaDefNames(defs []orktypes.HPAProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
func pdbDefNames(defs []orktypes.PDBProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
func ruDefNames(defs []orktypes.RollingUpdateProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
func reconcilerDefNames(defs []orktypes.ReconcilerProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
func resourceDefNames(defs []orktypes.ResourceProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
func probeDefNames(defs []orktypes.ProbeProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
func containerSecurityDefNames(defs []orktypes.ContainerSecurityProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
func podSecurityDefNames(defs []orktypes.PodSecurityProfileDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
