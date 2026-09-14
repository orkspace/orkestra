package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithRequeue(crdName string, rq *orktypes.RequeueConfig) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {OperatorBox: orktypes.OperatorBoxConfig{
			Reconciler: &orktypes.ReconcilerConfig{Requeue: rq},
		}},
	})
}

func TestValidateRequeue_NoConfig(t *testing.T) {
	k := katalogWithRequeue("myapp", nil)
	assert.NoError(t, k.validateRequeue())
}

func TestValidateRequeue_EmptyAfter(t *testing.T) {
	k := katalogWithRequeue("myapp", &orktypes.RequeueConfig{After: ""})
	assert.NoError(t, k.validateRequeue())
}

func TestValidateRequeue_ValidDuration(t *testing.T) {
	for _, after := range []string{"30s", "5m", "1h", "500ms"} {
		k := katalogWithRequeue("myapp", &orktypes.RequeueConfig{After: after})
		assert.NoError(t, k.validateRequeue(), "after=%q", after)
	}
}

func TestValidateRequeue_ValidTemplate(t *testing.T) {
	k := katalogWithRequeue("myapp", &orktypes.RequeueConfig{
		After: `{{ .spec.checkInterval | default "60s" }}`,
	})
	assert.NoError(t, k.validateRequeue())
}

func TestValidateRequeue_InvalidAfter(t *testing.T) {
	k := katalogWithRequeue("myapp", &orktypes.RequeueConfig{After: "not-a-duration"})
	err := k.validateRequeue()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requeue.after")
	assert.Contains(t, err.Error(), "not-a-duration")
}

func TestValidateRequeue_InvalidAfter_Bare_Number(t *testing.T) {
	k := katalogWithRequeue("myapp", &orktypes.RequeueConfig{After: "60"})
	err := k.validateRequeue()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requeue.after")
}
