package katalog

import (
	"testing"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/konfig"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func objForTest() domain.Object {
	return domain.UnstructuredForTest()
}

func katalogWithPreReconcile(pr orktypes.PreReconcileConfig) *Katalog {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			gvkForTest(): {
				APITypes: orktypes.APITypes{
					Kind:    "Application",
					Version: "v1",
					Group:   "test.orkestra.katalog",
				},
				OperatorBox: orktypes.OperatorBoxConfig{
					PreReconcile: &pr,
				},
			},
		},
	}

	k.SetDefaults(konfig.NewDefaultKonfig())
	k.SetGroupVersionKind()

	return k
}

func gvkForTest() string {
	return "test.orkestra.katalog/v1, Kind=Application"
}

func TestIsEventAware_InitialState(t *testing.T) {
	k := katalogWithPreReconcile(orktypes.PreReconcileConfig{
		ReconcileGate: &orktypes.GateConditions{
			EventAware: true,
		},
	})

	obj := objForTest()
	gvk := gvkForTest()

	box := k.effectiveBox(obj, gvk)
	require.NotNil(t, box)
	require.NotNil(t, box.PreReconcile)
	require.NotNil(t, box.PreReconcile.ReconcileGate)

	t.Logf("has gate: %v", box.PreReconcile.HasReconcileGate())
	t.Logf("event aware: %v", box.PreReconcile.ReconcileGate.IsEventAware())

	assert.True(t, k.IsEventAware(obj, gvk))
}

func TestIsEventAware(t *testing.T) {
	tests := []struct {
		name     string
		katalog  *Katalog
		gvk      string
		expected bool
	}{
		{
			name: "event aware gate",
			katalog: katalogWithPreReconcile(orktypes.PreReconcileConfig{
				ReconcileGate: &orktypes.GateConditions{
					EventAware: true,
				},
			}),
			gvk:      gvkForTest(),
			expected: true,
		},
		{
			name: "event aware disabled",
			katalog: katalogWithPreReconcile(orktypes.PreReconcileConfig{
				ReconcileGate: &orktypes.GateConditions{
					EventAware: false,
				},
			}),
			gvk:      gvkForTest(),
			expected: false,
		},
		{
			name: "reconcile gate without event awareness",
			katalog: katalogWithPreReconcile(orktypes.PreReconcileConfig{
				ReconcileGate: &orktypes.GateConditions{
					When: []orktypes.Condition{
						{
							Field:  "{{ .metadata.name }}",
							Equals: "app",
						},
					},
				},
			}),
			gvk:      gvkForTest(),
			expected: false,
		},
		{
			name:     "unknown gvk",
			katalog:  katalogWithPreReconcile(orktypes.PreReconcileConfig{}),
			gvk:      "does-not-exist",
			expected: false,
		},
		{
			name:     "nil katalog",
			katalog:  nil,
			gvk:      gvkForTest(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool

			if tt.katalog == nil {
				got = (*Katalog)(nil).IsEventAware(objForTest(), tt.gvk)
			} else {
				got = tt.katalog.IsEventAware(objForTest(), tt.gvk)
			}

			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestGetPreReconcileSentinels_ReturnsDeclared(t *testing.T) {
	k := katalogWithPreReconcile(orktypes.PreReconcileConfig{
		Sentinels: []string{
			"generationChanged",
			"labelsChanged",
		},
	})

	sentinels := k.GetPreReconcileSentinels(objForTest(), gvkForTest())

	assert.Equal(t, []string{"generationChanged", "labelsChanged"}, sentinels)
}

func TestGetPreReconcileSentinels_NoSentinels(t *testing.T) {
	k := katalogWithPreReconcile(orktypes.PreReconcileConfig{})

	sentinels := k.GetPreReconcileSentinels(objForTest(), gvkForTest())

	assert.Empty(t, sentinels)
}

func TestGetPreReconcileSentinels_Unknown(t *testing.T) {
	k := katalogWithPreReconcile(orktypes.PreReconcileConfig{
		Sentinels: []string{"generationChanged"},
	})

	sentinels := k.GetPreReconcileSentinels(objForTest(), "unknown")

	assert.Nil(t, sentinels)
}

func TestGetPreReconcileSentinels_NilKatalog(t *testing.T) {
	var k *Katalog

	sentinels := k.GetPreReconcileSentinels(objForTest(), gvkForTest())

	assert.Nil(t, sentinels)
}

func TestEffectiveBox_ResolvesTargetSpecificOperatorBox(t *testing.T) {
	targetEventAware := true
	crdEventAware := false

	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				APITypes: orktypes.APITypes{
					Kind:    "Application",
					Version: "v1",
					Group:   "test.orkestra.katalog",
				},
				OperatorBox: orktypes.OperatorBoxConfig{
					PreReconcile: &orktypes.PreReconcileConfig{
						ReconcileGate: &orktypes.GateConditions{
							EventAware: crdEventAware,
						},
					},
				},
				Serve: &orktypes.ServeConfig{
					Enabled: true,
					Target: orktypes.ServeTargetValue{
						Entries: map[string]*orktypes.ServeTargetConfig{
							"canary": {
								OperatorBox: &orktypes.OperatorBoxConfig{
									PreReconcile: &orktypes.PreReconcileConfig{
										ReconcileGate: &orktypes.GateConditions{
											EventAware: targetEventAware,
										},
									}},
							},
						},
					},
				},
			},
		},
	}

	require.NoError(t, k.SetGroupVersionKind())

	obj := objForTest()
	obj.SetAnnotations(map[string]string{"orkestra.orkspace.io/serve-target": "canary"})

	box := k.effectiveBox(obj, gvkForTest())

	require.NotNil(t, box)
	require.NotNil(t, box.PreReconcile)
	require.NotNil(t, box.PreReconcile.ReconcileGate)

	assert.True(t, box.PreReconcile.ReconcileGate.IsEventAware())
}
