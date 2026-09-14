package validate

import (
	"testing"

	"github.com/orkspace/orkestra/pkg/katalog"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers

func katalogWithProfiles(reg orktypes.ProfileRegistry) *executor {
	return newExec(&katalog.Katalog{Profiles: reg})
}

func katalogWithProfilesAndNP(reg orktypes.ProfileRegistry, crdName string, nps ...orktypes.NetworkPolicyTemplateSource) *executor {
	k := katalog.NewKatalogForTest(map[string]orktypes.CRDEntry{
		crdName: {
			OperatorBox: orktypes.OperatorBoxConfig{
				OnCreate: &orktypes.HookTemplates{
					NetworkPolicies: nps,
				},
			},
		},
	})
	k.Profiles = reg
	return newExec(k)
}

func katalogWithProfilesAndRQ(reg orktypes.ProfileRegistry, crdName string, rqs ...orktypes.ResourceQuotaTemplateSource) *executor {
	k := katalog.NewKatalogForTest(map[string]orktypes.CRDEntry{
		crdName: {
			OperatorBox: orktypes.OperatorBoxConfig{
				OnCreate: &orktypes.HookTemplates{
					ResourceQuotas: rqs,
				},
			},
		},
	})
	k.Profiles = reg
	return newExec(k)
}

// ── validateUserProfiles ──────────────────────────────────────────────────────

func TestValidateUserProfiles_Empty(t *testing.T) {
	k := katalogWithProfiles(orktypes.ProfileRegistry{})
	assert.NoError(t, k.validateUserProfiles())
}

func TestValidateUserProfiles_ValidNetworkPolicy(t *testing.T) {
	k := katalogWithProfiles(orktypes.ProfileRegistry{
		NetworkPolicies: []orktypes.NetworkPolicyProfileDef{
			{Name: "allow-monitoring", PolicyTypes: []string{"Ingress"}},
		},
	})
	assert.NoError(t, k.validateUserProfiles())
}

func TestValidateUserProfiles_ValidResourceQuota(t *testing.T) {
	k := katalogWithProfiles(orktypes.ProfileRegistry{
		ResourceQuotas: []orktypes.ResourceQuotaProfileDef{
			{Name: "team-medium", Hard: map[string]string{"pods": "30", "cpu": "6"}},
		},
	})
	assert.NoError(t, k.validateUserProfiles())
}

func TestValidateUserProfiles_ValidHPA(t *testing.T) {
	k := katalogWithProfiles(orktypes.ProfileRegistry{
		HPA: []orktypes.HPAProfileDef{
			{Name: "aggressive-scale", MinReplicas: "2", MaxReplicas: "20"},
		},
	})
	assert.NoError(t, k.validateUserProfiles())
}

func TestValidateUserProfiles_ValidPDB(t *testing.T) {
	k := katalogWithProfiles(orktypes.ProfileRegistry{
		PDB: []orktypes.PDBProfileDef{
			{Name: "strict", MinAvailable: "2"},
		},
	})
	assert.NoError(t, k.validateUserProfiles())
}

func TestValidateUserProfiles_ValidRollingUpdate(t *testing.T) {
	k := katalogWithProfiles(orktypes.ProfileRegistry{
		RollingUpdate: []orktypes.RollingUpdateProfileDef{
			{Name: "canary", MaxSurge: "1", MaxUnavailable: "0"},
		},
	})
	assert.NoError(t, k.validateUserProfiles())
}

func TestValidateUserProfiles_TemplateExpressionInHard(t *testing.T) {
	k := katalogWithProfiles(orktypes.ProfileRegistry{
		ResourceQuotas: []orktypes.ResourceQuotaProfileDef{
			{Name: "dynamic", Hard: map[string]string{"pods": "{{ .spec.maxPods }}"}},
		},
	})
	assert.NoError(t, k.validateUserProfiles())
}

func TestValidateUserProfiles_DuplicateNetworkPolicyName(t *testing.T) {
	k := katalogWithProfiles(orktypes.ProfileRegistry{
		NetworkPolicies: []orktypes.NetworkPolicyProfileDef{
			{Name: "allow-monitoring"},
			{Name: "allow-monitoring"},
		},
	})
	err := k.validateUserProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate profile name")
	assert.Contains(t, err.Error(), "allow-monitoring")
}

func TestValidateUserProfiles_DuplicateResourceQuotaName(t *testing.T) {
	k := katalogWithProfiles(orktypes.ProfileRegistry{
		ResourceQuotas: []orktypes.ResourceQuotaProfileDef{
			{Name: "team-medium", Hard: map[string]string{"pods": "10"}},
			{Name: "team-medium", Hard: map[string]string{"pods": "20"}},
		},
	})
	err := k.validateUserProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate profile name")
}

func TestValidateUserProfiles_MissingName(t *testing.T) {
	k := katalogWithProfiles(orktypes.ProfileRegistry{
		PDB: []orktypes.PDBProfileDef{
			{MinAvailable: "1"},
		},
	})
	err := k.validateUserProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing a name")
}

func TestValidateUserProfiles_ShadowingBuiltinIsAllowed(t *testing.T) {
	// Shadowing a built-in produces a warning but is not an error.
	k := katalogWithProfiles(orktypes.ProfileRegistry{
		NetworkPolicies: []orktypes.NetworkPolicyProfileDef{
			{Name: "deny-all", PolicyTypes: []string{"Ingress", "Egress"}},
		},
	})
	assert.NoError(t, k.validateUserProfiles())
}

// ── user profile used in networkPolicy reference ──────────────────────────────

func TestValidateNetworkPolicyProfiles_UserDefinedProfileAccepted(t *testing.T) {
	reg := orktypes.ProfileRegistry{
		NetworkPolicies: []orktypes.NetworkPolicyProfileDef{
			{Name: "allow-monitoring", PolicyTypes: []string{"Ingress"}},
		},
	}
	k := katalogWithProfilesAndNP(reg, "app", orktypes.NetworkPolicyTemplateSource{
		Name:    "np",
		Profile: "allow-monitoring",
	})
	require.NoError(t, k.validateUserProfiles())
	assert.NoError(t, k.validateNetworkPolicyProfiles())
}

func TestValidateNetworkPolicyProfiles_UnknownProfileStillRejected(t *testing.T) {
	k := katalogWithProfilesAndNP(orktypes.ProfileRegistry{}, "app", orktypes.NetworkPolicyTemplateSource{
		Name:    "np",
		Profile: "custom-unknown",
	})
	assert.Error(t, k.validateNetworkPolicyProfiles())
}

func TestValidateNetworkPolicyProfiles_UserProfileShadowsBuiltin(t *testing.T) {
	reg := orktypes.ProfileRegistry{
		NetworkPolicies: []orktypes.NetworkPolicyProfileDef{
			{Name: "deny-all", PolicyTypes: []string{"Ingress"}},
		},
	}
	k := katalogWithProfilesAndNP(reg, "app", orktypes.NetworkPolicyTemplateSource{
		Name:    "np",
		Profile: "deny-all",
	})
	require.NoError(t, k.validateUserProfiles())
	assert.NoError(t, k.validateNetworkPolicyProfiles())
}

// ── user profile used in resourceQuota reference ──────────────────────────────

func TestValidateResourceQuotaProfiles_UserDefinedProfileAccepted(t *testing.T) {
	reg := orktypes.ProfileRegistry{
		ResourceQuotas: []orktypes.ResourceQuotaProfileDef{
			{Name: "team-medium", Hard: map[string]string{"pods": "30"}},
		},
	}
	k := katalogWithProfilesAndRQ(reg, "app", orktypes.ResourceQuotaTemplateSource{
		Name:    "rq",
		Profile: "team-medium",
	})
	require.NoError(t, k.validateUserProfiles())
	assert.NoError(t, k.validateResourceQuotaProfiles())
}

func TestValidateResourceQuotaProfiles_UnknownProfileStillRejected(t *testing.T) {
	k := katalogWithProfilesAndRQ(orktypes.ProfileRegistry{}, "app", orktypes.ResourceQuotaTemplateSource{
		Name:    "rq",
		Profile: "custom-unknown",
	})
	assert.Error(t, k.validateResourceQuotaProfiles())
}
