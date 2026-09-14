package katalog

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithDeprecation(d *orktypes.KatalogDeprecation) *Katalog {
	if d == nil {
		return NewEmptyKatalog()
	}
	return &Katalog{lifecycle: &orktypes.KatalogLifecycle{Deprecation: d}}
}

func TestCheckDeprecationPolicy_NilBlock(t *testing.T) {
	k := katalogWithDeprecation(nil)
	assert.NoError(t, k.CheckDeprecationPolicy())
}

func TestCheckDeprecationPolicy_NoTimeline_Deprecated(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
	})
	err := k.CheckDeprecationPolicy()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deprecated")
	assert.Contains(t, err.Error(), "lifecycle:")
}

func TestCheckDeprecationPolicy_WarningWindow(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
		Timeline: &orktypes.DeprecationTimeline{
			From: "2020-01-01", // in the past — window is open
			To:   "2099-01-01",
		},
	})
	err := k.CheckDeprecationPolicy()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lifecycle:")
}

func TestCheckDeprecationPolicy_BeforeFrom_NoAcceptNeeded(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
		Timeline: &orktypes.DeprecationTimeline{
			From: "2099-01-01", // not yet reached
			To:   "2099-06-01",
		},
	})
	assert.NoError(t, k.CheckDeprecationPolicy())
}

func TestCheckDeprecationPolicy_EOL(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
		Timeline: &orktypes.DeprecationTimeline{
			From: "2020-01-01",
			To:   "2020-06-01", // past EOL
		},
	})
	err := k.CheckDeprecationPolicy()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "end-of-life")
	assert.Contains(t, err.Error(), "lifecycle:")
}

func TestCheckDeprecationPolicy_MigrationTargetInError(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message:    "use v2",
		MigratedTo: "my-pattern:v2.0.0",
	})
	err := k.CheckDeprecationPolicy()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "my-pattern:v2.0.0")
}
