package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithResourceQuota(crdName string, rqs ...orktypes.ResourceQuotaTemplateSource) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {
			OperatorBox: orktypes.OperatorBoxConfig{
				OnCreate: &orktypes.HookTemplates{
					ResourceQuotas: rqs,
				},
			},
		},
	})
}

func TestValidateResourceQuotaProfiles_NoCRDs(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{})
	assert.NoError(t, k.validateResourceQuotaProfiles())
}

func TestValidateResourceQuotaProfiles_NoProfile(t *testing.T) {
	k := katalogWithResourceQuota("app", orktypes.ResourceQuotaTemplateSource{Name: "quota"})
	assert.NoError(t, k.validateResourceQuotaProfiles())
}

func TestValidateResourceQuotaProfiles_ValidProfile_Small(t *testing.T) {
	k := katalogWithResourceQuota("app", orktypes.ResourceQuotaTemplateSource{
		Name:    "quota",
		Profile: "small",
	})
	assert.NoError(t, k.validateResourceQuotaProfiles())
}

func TestValidateResourceQuotaProfiles_ValidProfile_Medium(t *testing.T) {
	k := katalogWithResourceQuota("app", orktypes.ResourceQuotaTemplateSource{
		Profile: "medium",
	})
	assert.NoError(t, k.validateResourceQuotaProfiles())
}

func TestValidateResourceQuotaProfiles_ValidProfile_Large(t *testing.T) {
	k := katalogWithResourceQuota("app", orktypes.ResourceQuotaTemplateSource{
		Profile: "large",
	})
	assert.NoError(t, k.validateResourceQuotaProfiles())
}

func TestValidateResourceQuotaProfiles_ValidProfile_XLarge(t *testing.T) {
	k := katalogWithResourceQuota("app", orktypes.ResourceQuotaTemplateSource{
		Profile: "xlarge",
	})
	assert.NoError(t, k.validateResourceQuotaProfiles())
}

func TestValidateResourceQuotaProfiles_UnknownProfile(t *testing.T) {
	k := katalogWithResourceQuota("app", orktypes.ResourceQuotaTemplateSource{
		Name:    "quota",
		Profile: "massive",
	})
	err := k.validateResourceQuotaProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown profile")
	assert.Contains(t, err.Error(), "massive")
	assert.Contains(t, err.Error(), "small, medium, large, xlarge")
}

func TestValidateResourceQuotaProfiles_MixedWithHard(t *testing.T) {
	k := katalogWithResourceQuota("app", orktypes.ResourceQuotaTemplateSource{
		Name:    "quota",
		Profile: "small",
		Hard:    map[string]string{"pods": "5"},
	})
	err := k.validateResourceQuotaProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "explicit hard limits")
}

func TestValidateResourceQuotaProfiles_TemplateExprSkipped(t *testing.T) {
	k := katalogWithResourceQuota("app", orktypes.ResourceQuotaTemplateSource{
		Profile: "{{ .Spec.QuotaProfile }}",
	})
	assert.NoError(t, k.validateResourceQuotaProfiles())
}

func TestValidateResourceQuotaProfiles_MultipleEntries_OneInvalid(t *testing.T) {
	k := katalogWithResourceQuota("app",
		orktypes.ResourceQuotaTemplateSource{Name: "q1", Profile: "small"},
		orktypes.ResourceQuotaTemplateSource{Name: "q2", Profile: "unknown-tier"},
	)
	err := k.validateResourceQuotaProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown-tier")
}
