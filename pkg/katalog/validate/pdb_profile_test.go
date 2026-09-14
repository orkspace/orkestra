package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithPDB(crdName string, pdbs ...orktypes.PDBTemplateSource) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {
			OperatorBox: orktypes.OperatorBoxConfig{
				OnCreate: &orktypes.HookTemplates{
					PodDisruptionBudgets: pdbs,
				},
			},
		},
	})
}

func TestValidatePDBBehaviorProfiles_NoCRDs(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{})
	assert.NoError(t, k.validatePDBBehaviorProfiles())
}

func TestValidatePDBBehaviorProfiles_NoProfile(t *testing.T) {
	k := katalogWithPDB("app", orktypes.PDBTemplateSource{Name: "pdb"})
	assert.NoError(t, k.validatePDBBehaviorProfiles())
}

func TestValidatePDBBehaviorProfiles_ValidProfile_ZeroDowntime(t *testing.T) {
	k := katalogWithPDB("app", orktypes.PDBTemplateSource{
		Name:     "pdb",
		Behavior: &orktypes.PDBBehavior{Profile: "zero-downtime"},
	})
	assert.NoError(t, k.validatePDBBehaviorProfiles())
}

func TestValidatePDBBehaviorProfiles_ValidProfile_Rolling(t *testing.T) {
	k := katalogWithPDB("app", orktypes.PDBTemplateSource{
		Behavior: &orktypes.PDBBehavior{Profile: "rolling"},
	})
	assert.NoError(t, k.validatePDBBehaviorProfiles())
}

func TestValidatePDBBehaviorProfiles_ValidProfile_Relaxed(t *testing.T) {
	k := katalogWithPDB("app", orktypes.PDBTemplateSource{
		Behavior: &orktypes.PDBBehavior{Profile: "relaxed"},
	})
	assert.NoError(t, k.validatePDBBehaviorProfiles())
}

func TestValidatePDBBehaviorProfiles_UnknownProfile(t *testing.T) {
	k := katalogWithPDB("app", orktypes.PDBTemplateSource{
		Name:     "pdb",
		Behavior: &orktypes.PDBBehavior{Profile: "strict"},
	})
	err := k.validatePDBBehaviorProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown behavior.profile")
	assert.Contains(t, err.Error(), "strict")
	assert.Contains(t, err.Error(), "zero-downtime")
}

func TestValidatePDBBehaviorProfiles_MixedWithMinAvailable(t *testing.T) {
	k := katalogWithPDB("app", orktypes.PDBTemplateSource{
		Name: "pdb",
		Behavior: &orktypes.PDBBehavior{
			Profile:      "rolling",
			MinAvailable: "1",
		},
	})
	err := k.validatePDBBehaviorProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minAvailable/maxUnavailable")
}

func TestValidatePDBBehaviorProfiles_MixedWithMaxUnavailable(t *testing.T) {
	k := katalogWithPDB("app", orktypes.PDBTemplateSource{
		Name: "pdb",
		Behavior: &orktypes.PDBBehavior{
			Profile:        "relaxed",
			MaxUnavailable: "1",
		},
	})
	err := k.validatePDBBehaviorProfiles()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minAvailable/maxUnavailable")
}

func TestValidatePDBBehaviorProfiles_TemplateExprSkipped(t *testing.T) {
	k := katalogWithPDB("app", orktypes.PDBTemplateSource{
		Behavior: &orktypes.PDBBehavior{Profile: "{{ .Spec.PDBProfile }}"},
	})
	assert.NoError(t, k.validatePDBBehaviorProfiles())
}
