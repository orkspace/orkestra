package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithHPA(crdName string, hpas ...orktypes.HPATemplateSource) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {
			OperatorBox: orktypes.OperatorBoxConfig{
				OnCreate: &orktypes.HookTemplates{
					HorizontalPodAutoscalers: hpas,
				},
			},
		},
	})
}

func TestValidateHPABehaviorProfiles_NoCRDs(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{})
	assert.NoError(t, k.validateHPABehaviorProfiles())
}

func TestValidateHPABehaviorProfiles_NoProfile(t *testing.T) {
	k := katalogWithHPA("app", orktypes.HPATemplateSource{Name: "hpa"})
	assert.NoError(t, k.validateHPABehaviorProfiles())
}

func TestValidateHPABehaviorProfiles_ValidProfile_Web(t *testing.T) {
	k := katalogWithHPA("app", orktypes.HPATemplateSource{
		Name:     "hpa",
		Behavior: &orktypes.HPABehavior{Profile: "web"},
	})
	assert.NoError(t, k.validateHPABehaviorProfiles())
}

func TestValidateHPABehaviorProfiles_ValidProfile_API(t *testing.T) {
	k := katalogWithHPA("app", orktypes.HPATemplateSource{
		Behavior: &orktypes.HPABehavior{Profile: "api"},
	})
	assert.NoError(t, k.validateHPABehaviorProfiles())
}

func TestValidateHPABehaviorProfiles_ValidProfile_LatencySensitive(t *testing.T) {
	k := katalogWithHPA("app", orktypes.HPATemplateSource{
		Behavior: &orktypes.HPABehavior{Profile: "latency-sensitive"},
	})
	assert.NoError(t, k.validateHPABehaviorProfiles())
}

func TestValidateHPABehaviorProfiles_ValidProfile_Batch(t *testing.T) {
	k := katalogWithHPA("app", orktypes.HPATemplateSource{
		Behavior: &orktypes.HPABehavior{Profile: "batch"},
	})
	assert.NoError(t, k.validateHPABehaviorProfiles())
}

func TestValidateHPABehaviorProfiles_ValidProfile_CostOptimized(t *testing.T) {
	k := katalogWithHPA("app", orktypes.HPATemplateSource{
		Behavior: &orktypes.HPABehavior{Profile: "cost-optimized"},
	})
	assert.NoError(t, k.validateHPABehaviorProfiles())
}

func TestValidateHPABehaviorProfiles_UnknownProfile(t *testing.T) {
	k := katalogWithHPA("app", orktypes.HPATemplateSource{
		Name:     "hpa",
		Behavior: &orktypes.HPABehavior{Profile: "aggressive"},
	})
	err := k.validateHPABehaviorProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown behavior.profile")
	assert.Contains(t, err.Error(), "aggressive")
	assert.Contains(t, err.Error(), "web")
}

func TestValidateHPABehaviorProfiles_MixedWithScaleUp(t *testing.T) {
	k := katalogWithHPA("app", orktypes.HPATemplateSource{
		Name: "hpa",
		Behavior: &orktypes.HPABehavior{
			Profile: "web",
			ScaleUp: &orktypes.HPAScalingRules{StabilizationWindowSeconds: 30},
		},
	})
	err := k.validateHPABehaviorProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scaleUp/scaleDown")
}

func TestValidateHPABehaviorProfiles_TemplateExprSkipped(t *testing.T) {
	k := katalogWithHPA("app", orktypes.HPATemplateSource{
		Behavior: &orktypes.HPABehavior{Profile: "{{ .Spec.HPA }}"},
	})
	assert.NoError(t, k.validateHPABehaviorProfiles())
}
