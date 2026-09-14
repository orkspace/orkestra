package validate

import (
	"testing"

	"time"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithRetryBackoff(crdName string, rec *orktypes.ReconcilerConfig, box orktypes.OperatorBoxConfig) *executor {
	box.Reconciler = rec
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {OperatorBox: box},
	})
}

func dur(d time.Duration) orktypes.Duration { return orktypes.Duration{Duration: d} }

// ── queue.retryBackoff ────────────────────────────────────────────────────────

func TestValidateRetryBackoff_NoConfig(t *testing.T) {
	k := katalogWithRetryBackoff("myapp", nil, orktypes.OperatorBoxConfig{})
	assert.NoError(t, k.validateRetryBackoff())
}

func TestValidateRetryBackoff_ValidShorthand(t *testing.T) {
	k := katalogWithRetryBackoff("myapp", &orktypes.ReconcilerConfig{
		Resync: dur(10 * time.Minute),
		Queue:  orktypes.Queue{RetryBackoff: &orktypes.RetryBackoffConfig{Initial: dur(5 * time.Second)}},
	}, orktypes.OperatorBoxConfig{})
	assert.NoError(t, k.validateRetryBackoff())
}

func TestValidateRetryBackoff_ValidFullForm(t *testing.T) {
	k := katalogWithRetryBackoff("myapp", &orktypes.ReconcilerConfig{
		Resync: dur(10 * time.Minute),
		Queue: orktypes.Queue{RetryBackoff: &orktypes.RetryBackoffConfig{
			Initial:     dur(500 * time.Millisecond),
			Max:         dur(30 * time.Second),
			Multiplier:  2.0,
			MaxAttempts: 3,
		}},
	}, orktypes.OperatorBoxConfig{})
	assert.NoError(t, k.validateRetryBackoff())
}

func TestValidateRetryBackoff_NegativeMultiplierErrors(t *testing.T) {
	k := katalogWithRetryBackoff("myapp", &orktypes.ReconcilerConfig{
		Queue: orktypes.Queue{RetryBackoff: &orktypes.RetryBackoffConfig{
			Initial:    dur(500 * time.Millisecond),
			Multiplier: -1.0,
		}},
	}, orktypes.OperatorBoxConfig{})
	err := k.validateRetryBackoff()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiplier must be >= 0")
}

func TestValidateRetryBackoff_MaxLessThanInitialErrors(t *testing.T) {
	k := katalogWithRetryBackoff("myapp", &orktypes.ReconcilerConfig{
		Queue: orktypes.Queue{RetryBackoff: &orktypes.RetryBackoffConfig{
			Initial: dur(30 * time.Second),
			Max:     dur(1 * time.Second),
		}},
	}, orktypes.OperatorBoxConfig{})
	err := k.validateRetryBackoff()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max")
	assert.Contains(t, err.Error(), "must be >= initial")
}

func TestValidateRetryBackoff_WorstCaseExceedsResyncWarns(t *testing.T) {
	// maxAttempts=5, initial=10s, multiplier=2 → delays: 10s+20s+40s+80s = 150s > 30s resync
	k := katalogWithRetryBackoff("myapp", &orktypes.ReconcilerConfig{
		Resync: dur(30 * time.Second),
		Queue: orktypes.Queue{RetryBackoff: &orktypes.RetryBackoffConfig{
			Initial:     dur(10 * time.Second),
			Max:         dur(5 * time.Minute),
			Multiplier:  2.0,
			MaxAttempts: 5,
		}},
	}, orktypes.OperatorBoxConfig{})
	assert.NoError(t, k.validateRetryBackoff())
	crd := k.k.EnabledCRDs()["myapp"]
	assert.Contains(t, crd.Warnings.String(), "worst-case delay")
}

func TestValidateRetryBackoff_WorstCaseWithinResyncNoWarning(t *testing.T) {
	// maxAttempts=3, initial=500ms, multiplier=2 → delays: 500ms+1s = 1.5s < 10m resync
	k := katalogWithRetryBackoff("myapp", &orktypes.ReconcilerConfig{
		Resync: dur(10 * time.Minute),
		Queue: orktypes.Queue{RetryBackoff: &orktypes.RetryBackoffConfig{
			Initial:     dur(500 * time.Millisecond),
			Max:         dur(30 * time.Second),
			Multiplier:  2.0,
			MaxAttempts: 3,
		}},
	}, orktypes.OperatorBoxConfig{})
	assert.NoError(t, k.validateRetryBackoff())
	crd := k.k.EnabledCRDs()["myapp"]
	assert.NotContains(t, crd.Warnings.String(), "worst-case delay")
}

// ── external[].retryBackoff ───────────────────────────────────────────────────

func TestValidateRetryBackoff_ExternalValidShorthand(t *testing.T) {
	k := katalogWithRetryBackoff("myapp", &orktypes.ReconcilerConfig{
		Resync: dur(10 * time.Minute),
	}, orktypes.OperatorBoxConfig{
		OnReconcile: &orktypes.HookTemplates{
			External: []orktypes.ExternalCallSpec{
				{Name: "health", URL: "http://svc/health", RetryBackoff: &orktypes.RetryBackoffConfig{
					Initial: dur(1 * time.Second),
				}},
			},
		},
	})
	assert.NoError(t, k.validateRetryBackoff())
}

func TestValidateRetryBackoff_ExternalNegativeMultiplierErrors(t *testing.T) {
	k := katalogWithRetryBackoff("myapp", nil, orktypes.OperatorBoxConfig{
		OnReconcile: &orktypes.HookTemplates{
			External: []orktypes.ExternalCallSpec{
				{Name: "health", URL: "http://svc/health", RetryBackoff: &orktypes.RetryBackoffConfig{
					Multiplier: -2.0,
				}},
			},
		},
	})
	err := k.validateRetryBackoff()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiplier must be >= 0")
}

func TestValidateRetryBackoff_ExternalWorstCaseExceedsResyncWarns(t *testing.T) {
	k := katalogWithRetryBackoff("myapp", &orktypes.ReconcilerConfig{
		Resync: dur(5 * time.Second),
	}, orktypes.OperatorBoxConfig{
		OnReconcile: &orktypes.HookTemplates{
			External: []orktypes.ExternalCallSpec{
				{Name: "db", URL: "postgres://svc/db", RetryBackoff: &orktypes.RetryBackoffConfig{
					Initial:     dur(3 * time.Second),
					Max:         dur(1 * time.Minute),
					Multiplier:  2.0,
					MaxAttempts: 4,
				}},
			},
		},
	})
	assert.NoError(t, k.validateRetryBackoff())
	crd := k.k.EnabledCRDs()["myapp"]
	assert.Contains(t, crd.Warnings.String(), "worst-case delay")
}
