package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithWatch(crdName string, box orktypes.OperatorBoxConfig) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {OperatorBox: box},
	})
}

func TestValidateWatchEntries_Empty(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{})
	assert.NoError(t, k.validateWatchEntries())
}

func TestValidateWatchEntries_Valid(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "apps/v1", Kind: "Deployment"},
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "default", Name: "shared-config"},
				{APIVersion: "v1", Kind: "Node", On: []string{"update"}},
			},
		},
	})
	assert.NoError(t, k.validateWatchEntries())
}

func TestValidateWatchEntries_MissingAPIVersion(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{Kind: "Deployment"},
			},
		},
	})
	err := k.validateWatchEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiVersion must not be empty")
}

func TestValidateWatchEntries_MissingKind(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "apps/v1"},
			},
		},
	})
	err := k.validateWatchEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind must not be empty")
}

func TestValidateWatchEntries_InvalidOnValue(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "apps/v1", Kind: "Deployment", On: []string{"modified"}},
			},
		},
	})
	err := k.validateWatchEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "modified")
	assert.Contains(t, err.Error(), "create, update, delete")
}

func TestValidateWatchEntries_DuplicateEntry(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "apps/v1", Kind: "Deployment"},
				{APIVersion: "apps/v1", Kind: "Deployment"},
			},
		},
	})
	err := k.validateWatchEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate watch entry")
}

func TestValidateWatchEntries_DuplicateWithDifferentNamespace(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "ns-a"},
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "ns-b"},
			},
		},
	})
	assert.NoError(t, k.validateWatchEntries())
}

func TestValidateWatchEntries_ValidAllOnValues(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "v1", Kind: "Node", On: []string{"create", "update", "delete"}},
			},
		},
	})
	assert.NoError(t, k.validateWatchEntries())
}

func TestValidateSentinels_Valid(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		PreReconcile: &orktypes.PreReconcileConfig{
			Sentinels: []string{"generationChanged", "labelsChanged"},
		},
	})
	assert.NoError(t, k.validateWatchEntries())
}

func TestValidateSentinels_Unknown(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		PreReconcile: &orktypes.PreReconcileConfig{
			Sentinels: []string{"generationChanged", "specChanged"},
		},
	})
	err := k.validateWatchEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "specChanged")
	assert.Contains(t, err.Error(), "generationChanged, labelsChanged, annotationsChanged")
}

func TestValidateSentinels_AllValid(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		PreReconcile: &orktypes.PreReconcileConfig{
			Sentinels: []string{"generationChanged", "labelsChanged", "annotationsChanged"},
		},
	})
	assert.NoError(t, k.validateWatchEntries())
}

func TestValidateSentinels_EnqueueGateValid(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		PreReconcile: &orktypes.PreReconcileConfig{
			Sentinels: []string{"generationChanged", "labelsChanged", "annotationsChanged"},
			EnqueueGate: &orktypes.GateConditions{
				Sentinels: []string{"generationChanged", "labelsChanged"},
			},
		},
	})
	assert.NoError(t, k.validateWatchEntries())
}

func TestValidateSentinels_EnqueueGateInValid(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		PreReconcile: &orktypes.PreReconcileConfig{
			Sentinels: []string{"generationChanged", "labelsChanged", "annotationsChanged"},
			EnqueueGate: &orktypes.GateConditions{
				Sentinels: []string{"generationChanging", "labelsChanged"},
			},
		},
	})
	assert.Error(t, k.validateWatchEntries())
}

func TestValidateSentinels_ReconcileGateValid(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		PreReconcile: &orktypes.PreReconcileConfig{
			Sentinels: []string{"generationChanged", "labelsChanged", "annotationsChanged"},
			ReconcileGate: &orktypes.GateConditions{
				Sentinels: []string{"generationChanged", "labelsChanged"},
			},
		},
	})
	assert.NoError(t, k.validateWatchEntries())
}

func TestValidateSentinels_ReconcileGateInValid(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		PreReconcile: &orktypes.PreReconcileConfig{
			Sentinels: []string{"generationChanged", "labelsChanged", "annotationsChanged"},
			ReconcileGate: &orktypes.GateConditions{
				Sentinels: []string{"generationChanging", "labelsChanged"},
			},
		},
	})
	assert.Error(t, k.validateWatchEntries())
}

func TestValidateGateTemplate_DeclaredSentinelParsesOK(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		PreReconcile: &orktypes.PreReconcileConfig{
			Sentinels: []string{"generationChanged"},
			EnqueueGate: &orktypes.GateConditions{
				When: []orktypes.Condition{
					{Field: `{{ generationChanged }}`, Equals: "true"},
				},
			},
		},
	})
	assert.NoError(t, k.validateWatchEntries())
}

func TestValidateGateTemplate_UndeclaredSentinelFails(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		PreReconcile: &orktypes.PreReconcileConfig{
			Sentinels: []string{"generationChanged"},
			EnqueueGate: &orktypes.GateConditions{
				When: []orktypes.Condition{
					{Field: `{{ labelsChanged }}`, Equals: "true"},
				},
			},
		},
	})
	err := k.validateWatchEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid template")
}

func TestValidateKeyFrom_ValidLabel(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "v1", Kind: "ConfigMap", KeyFrom: &orktypes.WatchKeyFrom{Label: "app.kubernetes.io/cr-owner"}},
			},
		},
	})
	require.NoError(t, k.validateWatchEntries())
}

func TestValidateKeyFrom_ValidName(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "v1", Kind: "Node", KeyFrom: &orktypes.WatchKeyFrom{Name: "my-singleton"}},
			},
		},
	})
	require.NoError(t, k.validateWatchEntries())
}

func TestValidateKeyFrom_BothLabelAndName(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "v1", Kind: "ConfigMap", KeyFrom: &orktypes.WatchKeyFrom{Label: "some-label", Name: "some-name"}},
			},
		},
	})
	err := k.validateWatchEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of label or name")
}

func TestValidateKeyFrom_NeitherLabelNorName(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "v1", Kind: "ConfigMap", KeyFrom: &orktypes.WatchKeyFrom{}},
			},
		},
	})
	err := k.validateWatchEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "neither label nor name")
}

func TestValidateKeyFrom_NamespaceWithLabelRejected(t *testing.T) {
	k := katalogWithWatch("myapp", orktypes.OperatorBoxConfig{
		Observe: &orktypes.Observe{
			Watch: []orktypes.WatchEntry{
				{APIVersion: "v1", Kind: "ConfigMap", KeyFrom: &orktypes.WatchKeyFrom{Label: "some-label", Namespace: "default"}},
			},
		},
	})
	err := k.validateWatchEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace has no effect")
}
