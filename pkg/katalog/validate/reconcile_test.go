package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithQueue(crdName string, q *orktypes.Queue) *executor {
	if q.Empty() {
		q = &orktypes.Queue{}
	}
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {OperatorBox: orktypes.OperatorBoxConfig{
			Reconciler: &orktypes.ReconcilerConfig{Queue: *q},
		}},
	})
}

func TestValidateQueue_Nil(t *testing.T) {
	k := katalogWithQueue("app", nil)
	assert.NoError(t, k.validateQueue())
}

func TestValidateQueue_OnLimitValid(t *testing.T) {
	k := katalogWithQueue("app", &orktypes.Queue{
		MaxDepth: 100,
		Cfg: &orktypes.QueueBehaviour{
			OnLimit: &orktypes.QueueBehaviourSetting{
				Drop: boolPtr(true),
			},
		},
	})
	assert.NoError(t, k.validateQueue())
}

func TestValidateQueue_UnLimitedValid(t *testing.T) {
	k := katalogWithQueue("app", &orktypes.Queue{MaxDepth: 0})
	assert.NoError(t, k.validateQueue())
}

func TestValidateQueue_LimitedValid(t *testing.T) {
	k := katalogWithQueue("app", &orktypes.Queue{
		MaxDepth: 100,
		Cfg: &orktypes.QueueBehaviour{
			OnLimit: &orktypes.QueueBehaviourSetting{
				Drop: boolPtr(true),
			},
		},
	})
	assert.NoError(t, k.validateQueue())
}

func TestValidateQueue_UnLimitedInValid(t *testing.T) {
	k := katalogWithQueue("app", &orktypes.Queue{
		MaxDepth: 0,
		Cfg: &orktypes.QueueBehaviour{
			OnLimit: &orktypes.QueueBehaviourSetting{
				Drop: boolPtr(true),
			},
		},
	})

	err := k.validateQueue()
	require.Error(t, err)
	assert.ErrorContains(t, err, "'queue.behaviour' configuration is only valid when 'queue.maxDepth' is greater than 0")
}

func TestValidateQueue_OnLimitDropValid(t *testing.T) {
	k := katalogWithQueue("app", &orktypes.Queue{
		MaxDepth: 100,
		Cfg: &orktypes.QueueBehaviour{
			OnLimit: &orktypes.QueueBehaviourSetting{
				Drop: boolPtr(false),
			},
		},
	})
	assert.NoError(t, k.validateQueue())
}

func TestValidateQueue_OnLimitWithValueInValid(t *testing.T) {
	k := katalogWithQueue("app", &orktypes.Queue{
		MaxDepth: 100,
		Cfg: &orktypes.QueueBehaviour{
			OnLimit: &orktypes.QueueBehaviourSetting{
				Drop:  boolPtr(true),
				Value: 5,
			},
		},
	})

	err := k.validateQueue()
	require.Error(t, err)
	assert.ErrorContains(t, err, "is not allowed in onLimit configuration")
}

func TestValidateQueue_onThresholdWarning(t *testing.T) {
	k := katalogWithQueue("app", &orktypes.Queue{
		MaxDepth: 100,
		Cfg: &orktypes.QueueBehaviour{
			OnThreshold: &orktypes.QueueBehaviourSetting{
				Drop:  boolPtr(false),
				Value: 5,
			},
		},
	})

	err := k.validateQueue()
	assert.NoError(t, err)

	entry := k.k.EnabledCRDs()["app"]

	if !entry.Warnings.HasWarnings() {
		t.Fatal("expected crd to have warning")
	}

	// Check if drop is set correctly
	cfg := entry.QueueConfig().Behaviour()
	// got := cfg.onThreshold.ShouldDrop()
	got := *cfg.OnThreshold.Drop

	if !got {
		t.Fatalf("expected onThreshold.drop to be true. got: %v", got)
	}
}
