package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithDeployment(crdName string, deps ...orktypes.DeploymentTemplateSource) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {
			OperatorBox: orktypes.OperatorBoxConfig{
				OnCreate: &orktypes.HookTemplates{
					Deployments: deps,
				},
			},
		},
	})
}

func TestValidateRollingUpdateProfiles_NoCRDs(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{})
	assert.NoError(t, k.validateRollingUpdateProfiles())
}

func TestValidateRollingUpdateProfiles_NoProfile(t *testing.T) {
	k := katalogWithDeployment("app", orktypes.DeploymentTemplateSource{Name: "deploy"})
	assert.NoError(t, k.validateRollingUpdateProfiles())
}

func TestValidateRollingUpdateProfiles_ValidProfile_Safe(t *testing.T) {
	k := katalogWithDeployment("app", orktypes.DeploymentTemplateSource{
		Name:          "deploy",
		RollingUpdate: &orktypes.RollingUpdateBehavior{Profile: "safe"},
	})
	assert.NoError(t, k.validateRollingUpdateProfiles())
}

func TestValidateRollingUpdateProfiles_ValidProfile_Fast(t *testing.T) {
	k := katalogWithDeployment("app", orktypes.DeploymentTemplateSource{
		RollingUpdate: &orktypes.RollingUpdateBehavior{Profile: "fast"},
	})
	assert.NoError(t, k.validateRollingUpdateProfiles())
}

func TestValidateRollingUpdateProfiles_ValidProfile_BlueGreen(t *testing.T) {
	k := katalogWithDeployment("app", orktypes.DeploymentTemplateSource{
		RollingUpdate: &orktypes.RollingUpdateBehavior{Profile: "blue-green"},
	})
	assert.NoError(t, k.validateRollingUpdateProfiles())
}

func TestValidateRollingUpdateProfiles_UnknownProfile(t *testing.T) {
	k := katalogWithDeployment("app", orktypes.DeploymentTemplateSource{
		Name:          "deploy",
		RollingUpdate: &orktypes.RollingUpdateBehavior{Profile: "canary"},
	})
	err := k.validateRollingUpdateProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
	assert.Contains(t, err.Error(), "canary")
}

func TestValidateRollingUpdateProfiles_MixedWithMaxSurge(t *testing.T) {
	k := katalogWithDeployment("app", orktypes.DeploymentTemplateSource{
		Name: "deploy",
		RollingUpdate: &orktypes.RollingUpdateBehavior{
			Profile:  "safe",
			MaxSurge: "1",
		},
	})
	err := k.validateRollingUpdateProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maxSurge/maxUnavailable")
}

func TestValidateRollingUpdateProfiles_MixedWithMaxUnavailable(t *testing.T) {
	k := katalogWithDeployment("app", orktypes.DeploymentTemplateSource{
		Name: "deploy",
		RollingUpdate: &orktypes.RollingUpdateBehavior{
			Profile:        "fast",
			MaxUnavailable: "0",
		},
	})
	err := k.validateRollingUpdateProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maxSurge/maxUnavailable")
}

func TestValidateRollingUpdateProfiles_TemplateExprSkipped(t *testing.T) {
	k := katalogWithDeployment("app", orktypes.DeploymentTemplateSource{
		RollingUpdate: &orktypes.RollingUpdateBehavior{Profile: "{{ .Spec.RollingProfile }}"},
	})
	assert.NoError(t, k.validateRollingUpdateProfiles())
}

func TestValidateRollingUpdateProfiles_StatefulSet(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				OnCreate: &orktypes.HookTemplates{
					StatefulSets: []orktypes.StatefulSetTemplateSource{
						{Name: "db", RollingUpdate: &orktypes.RollingUpdateBehavior{Profile: "safe"}},
					},
				},
			},
		},
	})
	assert.NoError(t, k.validateRollingUpdateProfiles())
}
