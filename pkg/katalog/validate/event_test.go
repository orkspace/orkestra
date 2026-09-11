package validate

import (
	"testing"

	"github.com/orkspace/orkestra/domain"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithEvents(crdName string, box orktypes.OperatorBoxConfig) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {OperatorBox: box},
	})
}

func TestValidateEventEntries_Empty(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{})
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_Valid(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{Reason: "DatabaseReady"},
			{Action: "Sync", Type: "Normal"},
			{ReportingController: "example.com/operator", ReportingInstance: "operator-1"},
			{Regarding: &domain.ManagedResource{Kind: "Database", Name: "my-db"}},
			{Related: &domain.ManagedResource{Kind: "ConfigMap", Name: "config"}},
			{Namespace: "default", Name: "my-event"},
			{KeyFrom: &orktypes.WatchKeyFrom{Name: "myapp"}},
		},
	})
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_NoMatcher(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{},
		},
	})
	err := k.validateEventEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event entry must declare at least one matching or routing field")
}

func TestValidateEventEntries_DuplicateEntry(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{Reason: "DatabaseReady", Type: "Normal"},
			{Reason: "DatabaseReady", Type: "Normal"},
		},
	})
	err := k.validateEventEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate event entry")
}

func TestValidateEventEntries_DifferentMatchers(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{Reason: "DatabaseReady"},
			{Reason: "DatabaseFailed"},
			{Action: "Sync"},
			{Type: "Warning"},
		},
	})
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_Regarding(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{
				Regarding: &domain.ManagedResource{
					APIVersion: "databases.example.com/v1",
					Kind:       "Database",
					Name:       "my-db",
					Namespace:  "default",
				},
			},
		},
	})
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_Related(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{
				Related: &domain.ManagedResource{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       "shared-config",
				},
			},
		},
	})
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_KeyFromValidLabel(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{
				Reason:  "DatabaseReady",
				KeyFrom: &orktypes.WatchKeyFrom{Label: "app.kubernetes.io/cr-owner"},
			},
		},
	})
	require.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_KeyFromValidName(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{
				Reason:  "DatabaseReady",
				KeyFrom: &orktypes.WatchKeyFrom{Name: "myapp"},
			},
		},
	})
	require.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_KeyFromBothLabelAndName(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{
				Reason: "DatabaseReady",
				KeyFrom: &orktypes.WatchKeyFrom{
					Label: "some-label",
					Name:  "some-name",
				},
			},
		},
	})
	err := k.validateEventEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of label or name")
}

func TestValidateEventEntries_KeyFromNeitherLabelNorName(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{
				Reason:  "DatabaseReady",
				KeyFrom: &orktypes.WatchKeyFrom{},
			},
		},
	})
	err := k.validateEventEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "neither label nor name")
}

func TestValidateEventEntries_KeyFromNamespaceWithLabelRejected(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{
				Reason: "DatabaseReady",
				KeyFrom: &orktypes.WatchKeyFrom{
					Label:     "some-label",
					Namespace: "default",
				},
			},
		},
	})
	err := k.validateEventEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace has no effect")
}

func TestValidateEventEntries_NameAndNamespace(t *testing.T) {
	k := katalogWithEvents("myapp", orktypes.OperatorBoxConfig{
		Events: []orktypes.EventEntry{
			{
				Name:      "my-event",
				Namespace: "default",
			},
		},
	})
	assert.NoError(t, k.validateEventEntries())
}
