package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithPreReconcile(pr orktypes.PreReconcileConfig) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			APITypes: orktypes.APITypes{
				Kind:    "Application",
				Version: "v1",
				Group:   "test.orkestra.katalog",
			},
			OperatorBox: orktypes.OperatorBoxConfig{
				PreReconcile: &pr,
			},
		},
	})
}

func TestValidatePreReconcile_NoConfig(t *testing.T) {
	k := katalogWithPreReconcile(orktypes.PreReconcileConfig{})
	assert.NoError(t, k.validatePreReconcile())
}

func TestValidatePreReconcile_Valid(t *testing.T) {
	k := katalogWithPreReconcile(orktypes.PreReconcileConfig{
		ReconcileGate: &orktypes.GateConditions{
			EventAware: true,
		},
	})
	assert.NoError(t, k.validatePreReconcile())
}

func TestValidatePreReconcile_Invalid(t *testing.T) {
	k := katalogWithPreReconcile(orktypes.PreReconcileConfig{
		EnqueueGate: &orktypes.GateConditions{
			EventAware: true,
		},
	})
	err := k.validatePreReconcile()
	require.Error(t, err)
	assert.ErrorContains(t, err, "'event aware' is only valid in reconcileGate")
}
