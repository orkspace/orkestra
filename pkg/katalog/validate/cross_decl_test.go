package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type crdInfo struct {
	name    string
	group   string
	kind    string
	version string
	label   map[string]string
	box     orktypes.OperatorBoxConfig
}

func crdForTest(info *crdInfo) orktypes.CRDEntry {
	if info == nil {
		info = &crdInfo{
			name:    "web",
			kind:    "Web",
			group:   "web.io",
			version: "v1",
		}
	}

	return orktypes.CRDEntry{
		Name: info.name,
		APITypes: orktypes.APITypes{
			Kind:    info.kind,
			Group:   info.group,
			Version: info.version,
		}, Labels: info.label, OperatorBox: info.box,
	}
}

func TestValidateCrossDecl_Nil(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{},
			},
		},
	})
	err := k.validateCrossDecl()
	assert.NoError(t, err)
}

func TestValidateCrossDecl_ValidWithCRDOnly(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app1"}},
			},
		},
		"app1": {},
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	assert.NoError(t, err)
}

func TestValidateCrossDecl_ValidMultipleWithCRDOnly(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": crdForTest(&crdInfo{name: "app", box: orktypes.OperatorBoxConfig{
			Cross: []orktypes.CrossCRDDeclaration{{CRD: "app1"}, {CRD: "app2"}},
		}}),
		"app1": crdForTest(&crdInfo{name: "app1", box: orktypes.OperatorBoxConfig{
			Cross: []orktypes.CrossCRDDeclaration{{CRD: "app"}, {CRD: "app2"}},
		}}),
		"app2": crdForTest(&crdInfo{name: "app2"}),
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	assert.NoError(t, err)
}

func TestValidateCrossDecl_ValidLabelsOnly(t *testing.T) {
	sel := map[string]string{"test": "hello"}
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{
					{
						LabelSelector: sel,
						As:            "ap",
					},
				},
			},
		},
		"app1": crdForTest(&crdInfo{label: sel}),
	})

	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	assert.NoError(t, err)
}

func TestValidateCrossDecl_InvalidLabelsAndCRD(t *testing.T) {
	sel := map[string]string{"test": "hello"}
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{
					{
						LabelSelector: sel,
						As:            "ap",
						CRD:           "app1",
					},
				},
			},
		},
		"app1": crdForTest(&crdInfo{label: sel}),
	})

	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "only one of 'crd:' or 'labels:' is required.")
}

func TestValidateCrossDecl_InvalidLabelsOnly(t *testing.T) {
	sel := map[string]string{"test": "hello"}
	sel2 := map[string]string{"test": "hello2"}
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": crdForTest(&crdInfo{name: "app", box: orktypes.OperatorBoxConfig{
			Cross: []orktypes.CrossCRDDeclaration{
				{
					LabelSelector: sel,
					As:            "ap",
				},
			},
		}}),
		"app2": crdForTest(&crdInfo{label: sel2}),
	})

	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "not found in any CRD in the katalog")
}

func TestValidateCrossDecl_InvalidNoCRDAndLabels(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{As: "no-crd-or-label"}},
			},
		},
	})
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "crd or labels required")
}

func TestValidateCrossDecl_InvalidLabelsNoAs(t *testing.T) {
	sel := map[string]string{"test": "hello"}
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{
					{
						LabelSelector: sel,
					},
				},
			},
		},
		"app1": crdForTest(&crdInfo{label: sel}),
	})

	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "'as' is required for cross referencing")
}

func TestValidateCrossDecl_InvalidSelfReferencing(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app"}},
			},
		},
	})
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "cross: a CRD cannot reference itself")
}

func TestValidateCrossDecl_WithInvalidCRD(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app-unknown"}},
			},
		},
	})
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "\"app-unknown\" not found")
}

func TestValidateCrossDecl_MultipleWithInvalidCRD(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app-unknown"}, {CRD: "app2"}, {CRD: "app-unknown3"}},
			},
		},
		"app2": crdForTest(&crdInfo{name: "app2"}),
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "\"app-unknown\" not found")
}
func TestValidateCrossDecl_ValidWithAs(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app1", As: "ap"}},
			},
		},
		"app1": crdForTest(&crdInfo{name: "app1"}),
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	assert.NoError(t, err)
}

func TestValidateCrossDecl_InvalidAs(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{
					{CRD: "app1", As: "ap/"},
					{CRD: "app2", As: "ap"},
				},
			},
		},
		"app1": crdForTest(&crdInfo{name: "app1"}),
		"app2": crdForTest(&crdInfo{name: "app2"}),
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid 'as' name")
}

func TestValidateCrossDecl_InvalidDuplicateAs(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{
					{CRD: "app1", As: "ap"},
					{CRD: "app2", As: "ap"},
				},
			},
		},
		"app1": crdForTest(&crdInfo{name: "app1"}),
		"app2": crdForTest(&crdInfo{name: "app2"}),
		"web": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{
					{CRD: "app1", As: "ap"},
					{CRD: "app2", As: "ap"},
				},
			},
		},
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	require.Error(t, err)
}

func TestValidateCrossDecl_ValidProtocols(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app4", Source: &orktypes.CrossSource{
					Protocol: "info",
				}}},
			},
		},
		"app1": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app", Source: &orktypes.CrossSource{Protocol: "metrics"}}},
			},
		},
		"app2": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app", Source: &orktypes.CrossSource{Protocol: "health"}}},
			},
		},
		"app3": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app", Source: &orktypes.CrossSource{Protocol: "health"}}},
			},
		},
		"app4": {},
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	assert.NoError(t, err)
}

func TestValidateCrossDecl_InvalidProtocols(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app4", Source: &orktypes.CrossSource{
					Protocol: "info",
				}}},
			},
		},
		"app1": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app", Source: &orktypes.CrossSource{Protocol: "health-style"}}},
			},
		},
		"app2": {},
		"app4": {},
	})

	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid ONCOP protocol")
}
func TestValidateCrossDecl_ValidTokenAuth(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app1", Source: &orktypes.CrossSource{
					Auth: &orktypes.Auth{
						Token: "test-token",
					},
				}}},
			},
		},
		"app1": crdForTest(&crdInfo{name: "app1"}),
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	assert.NoError(t, err)
}

func TestValidateCrossDecl_ValidSecretRefAuth(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app1", Source: &orktypes.CrossSource{
					Auth: &orktypes.Auth{
						SecretRef: &orktypes.APISecretRef{
							Name:      "test-token",
							Namespace: "test-ns",
							Key:       "new-token",
						},
					},
				}}},
			},
		},
		"app1": crdForTest(&crdInfo{name: "app1"}),
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	assert.NoError(t, err)
}

func TestValidateCrossDecl_InvalidAuthWithTokenAndSecretRef(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app1", Source: &orktypes.CrossSource{
					Auth: &orktypes.Auth{
						Token: "test-token",
						SecretRef: &orktypes.APISecretRef{
							Name:      "test-token",
							Namespace: "test-ns",
							Key:       "new-token",
						},
					},
				}}},
			},
		},
		"app1": crdForTest(&crdInfo{name: "app1"}),
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "declared both token and secretRef. Only one is allowed")
}

func TestValidateCrossDecl_InvalidSecretRefAuthNoName(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app1", Source: &orktypes.CrossSource{
					Auth: &orktypes.Auth{
						SecretRef: &orktypes.APISecretRef{
							Namespace: "test-ns",
							Key:       "new-token",
						},
					},
				}}},
			},
		},
		"app1": crdForTest(&crdInfo{name: "app1"}),
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "name is required for secretRef")
}

func TestValidateCrossDecl_InvalidSecretRefAuthNoKey(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app1", Source: &orktypes.CrossSource{
					Auth: &orktypes.Auth{
						SecretRef: &orktypes.APISecretRef{
							Name:      "test-token",
							Namespace: "test-ns",
						},
					},
				}}},
			},
		},
		"app1": crdForTest(&crdInfo{name: "app1"}),
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	require.Error(t, err)
	assert.ErrorContains(t, err, "key is required for secretRef")
}

func TestValidateCrossDecl_WarnAuthWithSecretRefNoNamespace(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {
			OperatorBox: orktypes.OperatorBoxConfig{
				Cross: []orktypes.CrossCRDDeclaration{{CRD: "app1", Source: &orktypes.CrossSource{
					Auth: &orktypes.Auth{
						SecretRef: &orktypes.APISecretRef{
							Name: "test-token",
							Key:  "new-token",
						},
					},
				}}},
			},
		},
		"app1": crdForTest(&crdInfo{name: "app1"}),
	})
	k.k.BuildLookupIndexes()
	err := k.validateCrossDecl()
	assert.NoError(t, err)

	entry := k.k.EnabledCRDs()["app"]
	if !entry.Warnings.HasWarnings() {
		t.Fatalf("expected crd to have warning")
	}
	assert.Contains(t, entry.Warnings.String(), "Defaults to orkestra namespace")
}
