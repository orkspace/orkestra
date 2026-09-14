package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithNetworkPolicy(crdName string, nps ...orktypes.NetworkPolicyTemplateSource) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {
			OperatorBox: orktypes.OperatorBoxConfig{
				OnCreate: &orktypes.HookTemplates{
					NetworkPolicies: nps,
				},
			},
		},
	})
}

func TestValidateNetworkPolicyProfiles_NoCRDs(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{})
	assert.NoError(t, k.validateNetworkPolicyProfiles())
}

func TestValidateNetworkPolicyProfiles_NoProfile(t *testing.T) {
	k := katalogWithNetworkPolicy("app", orktypes.NetworkPolicyTemplateSource{Name: "np"})
	assert.NoError(t, k.validateNetworkPolicyProfiles())
}

func TestValidateNetworkPolicyProfiles_ValidProfile_DenyAll(t *testing.T) {
	k := katalogWithNetworkPolicy("app", orktypes.NetworkPolicyTemplateSource{
		Name:    "np",
		Profile: "deny-all",
	})
	assert.NoError(t, k.validateNetworkPolicyProfiles())
}

func TestValidateNetworkPolicyProfiles_ValidProfile_AllowSameNamespace(t *testing.T) {
	k := katalogWithNetworkPolicy("app", orktypes.NetworkPolicyTemplateSource{
		Name:    "np",
		Profile: "allow-same-namespace",
	})
	assert.NoError(t, k.validateNetworkPolicyProfiles())
}

func TestValidateNetworkPolicyProfiles_ValidProfile_AllowDNSEgress(t *testing.T) {
	k := katalogWithNetworkPolicy("app", orktypes.NetworkPolicyTemplateSource{
		Name:    "np",
		Profile: "allow-dns-egress",
	})
	assert.NoError(t, k.validateNetworkPolicyProfiles())
}

func TestValidateNetworkPolicyProfiles_ValidProfile_DenyAllIngress(t *testing.T) {
	k := katalogWithNetworkPolicy("app", orktypes.NetworkPolicyTemplateSource{
		Profile: "deny-all-ingress",
	})
	assert.NoError(t, k.validateNetworkPolicyProfiles())
}

func TestValidateNetworkPolicyProfiles_ValidProfile_DenyAllEgress(t *testing.T) {
	k := katalogWithNetworkPolicy("app", orktypes.NetworkPolicyTemplateSource{
		Profile: "deny-all-egress",
	})
	assert.NoError(t, k.validateNetworkPolicyProfiles())
}

func TestValidateNetworkPolicyProfiles_UnknownProfile(t *testing.T) {
	k := katalogWithNetworkPolicy("app", orktypes.NetworkPolicyTemplateSource{
		Name:    "np",
		Profile: "allow-everything",
	})
	err := k.validateNetworkPolicyProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown profile")
	assert.Contains(t, err.Error(), "allow-everything")
	assert.Contains(t, err.Error(), "deny-all")
}

func TestValidateNetworkPolicyProfiles_MixedWithIngress(t *testing.T) {
	k := katalogWithNetworkPolicy("app", orktypes.NetworkPolicyTemplateSource{
		Name:    "np",
		Profile: "deny-all",
		Ingress: []orktypes.NetworkPolicyIngressRule{{}},
	})
	err := k.validateNetworkPolicyProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ingress/egress/policyTypes")
}

func TestValidateNetworkPolicyProfiles_MixedWithEgress(t *testing.T) {
	k := katalogWithNetworkPolicy("app", orktypes.NetworkPolicyTemplateSource{
		Name:    "np",
		Profile: "deny-all",
		Egress:  []orktypes.NetworkPolicyEgressRule{{}},
	})
	err := k.validateNetworkPolicyProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ingress/egress/policyTypes")
}

func TestValidateNetworkPolicyProfiles_TemplateExprSkipped(t *testing.T) {
	k := katalogWithNetworkPolicy("app", orktypes.NetworkPolicyTemplateSource{
		Profile: "{{ .Spec.NetworkProfile }}",
	})
	assert.NoError(t, k.validateNetworkPolicyProfiles())
}
