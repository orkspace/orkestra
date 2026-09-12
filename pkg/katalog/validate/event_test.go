package validate

import (
	"testing"

	"github.com/orkspace/orkestra/domain"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// katalogWithObserve builds an executor with a single CRD containing the
// supplied observe configuration.
func katalogWithObserve(crdName string, observe *orktypes.Observe) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {OperatorBox: orktypes.OperatorBoxConfig{
			Observe: observe,
		},
		}})
}

func TestValidateEventEntries_ObserveNil(t *testing.T) {
	k := katalogWithObserve("myapp", nil)
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_Empty(t *testing.T) {
	k := katalogWithObserve("myapp", &orktypes.Observe{})
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_Valid(t *testing.T) {
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"dbReady": {
				Reason: "DatabaseReady",
			},
			"sync": {
				Action: "Sync",
				Type:   "Normal",
			},
			"operator": {
				ReportingController: "example.com/operator",
				ReportingInstance:   "operator-1",
			},
			"regarding": {
				Regarding: &domain.ManagedResource{
					Kind: "Database",
					Name: "my-db",
				},
			},
			"related": {
				Related: &domain.ManagedResource{
					Kind: "ConfigMap",
					Name: "config",
				},
			},
			"namespace": {
				Namespace: "default",
			},
			"keyFrom": {
				KeyFrom: &orktypes.WatchKeyFrom{Name: "myapp"},
			},
		},
	})
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_NoMatcher(t *testing.T) {
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"empty": {},
		},
	})
	err := k.validateEventEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event entry must declare at least one matching or routing field")
}

func TestValidateEventEntries_DifferentMatchers(t *testing.T) {
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"ready":  {Reason: "DatabaseReady"},
			"failed": {Reason: "DatabaseFailed"},
			"sync":   {Action: "Sync"},
			"warn":   {Type: "Warning"},
		},
	})
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_Regarding(t *testing.T) {
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"database": {
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
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"config": {
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
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"dbReady": {
				Reason:  "DatabaseReady",
				KeyFrom: &orktypes.WatchKeyFrom{Label: "app.kubernetes.io/cr-owner"},
			},
		},
	})
	require.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_KeyFromValidName(t *testing.T) {
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"dbReady": {
				Reason:  "DatabaseReady",
				KeyFrom: &orktypes.WatchKeyFrom{Name: "myapp"},
			},
		},
	})
	require.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_KeyFromBothLabelAndName(t *testing.T) {
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"dbReady": {
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
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"dbReady": {
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
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"dbReady": {
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

func TestValidateEventEntries_OnValid(t *testing.T) {
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"dbReady": {
				Reason: "DatabaseReady",
				On:     []string{"create", "update", "delete"},
			},
		},
	})
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_OnInvalid(t *testing.T) {
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"dbReady": {
				Reason: "DatabaseReady",
				On:     []string{"create", "modify"},
			},
		},
	})
	err := k.validateEventEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown on: value(s) [modify]")
}

func TestValidateEventEntries_OnAllValid(t *testing.T) {
	k := katalogWithObserve("myapp", &orktypes.Observe{
		Events: map[string]*orktypes.EventEntry{
			"dbReady": {
				Reason: "DatabaseReady",
				On:     orktypes.ValidObserveEvents(),
			},
		},
	})
	assert.NoError(t, k.validateEventEntries())
}

func TestValidateEventEntries_EventNameCamelCase(t *testing.T) {
	tests := []struct {
		name      string
		eventName string
		valid     bool
	}{
		{
			name:      "lower camel case",
			eventName: "dbReady",
			valid:     true,
		},
		{
			name:      "multiple words",
			eventName: "databaseReady",
			valid:     true,
		},
		{
			name:      "acronym",
			eventName: "dbAPIReady",
			valid:     true,
		},
		{
			name:      "hyphenated",
			eventName: "db-ready",
			valid:     false,
		},
		{
			name:      "snake case",
			eventName: "db_ready",
			valid:     true,
		},
		{
			name:      "with space",
			eventName: "Db Ready",
			valid:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observe := &orktypes.Observe{
				Events: map[string]*orktypes.EventEntry{
					tt.eventName: {
						Reason: "DatabaseReady",
					},
				},
			}

			err := validateCRDEventEntries(
				"app",
				orktypes.CRDEntry{
					OperatorBox: orktypes.OperatorBoxConfig{
						Observe: observe,
					},
				},
			)

			if tt.valid && err != nil {
				t.Fatalf("expected valid event name, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("expected invalid event name")
			}
		})
	}
}
