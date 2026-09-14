package katalog

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const profileMotifPath = "../registry/motif/testdata/with-profiles.yaml"

// expandKatalogImports merges profiles from spec.imports into k.Profiles.
func TestExpandKatalogImports_MergesProfiles(t *testing.T) {
	k := &Katalog{
		Spec: orktypes.KatalogSpec{
			Imports: []orktypes.MotifImport{
				{Motif: profileMotifPath},
			},
		},
		enabledCRDs: map[string]orktypes.CRDEntry{},
	}

	require.NoError(t, k.expandKatalogImports())
	assert.Len(t, k.Profiles.Resources, 1)
	assert.Equal(t, "org-standard", k.Profiles.Resources[0].Name)
}

// Profiles from spec.crds[name].imports are ignored — only resources/admission merge there.
func TestExpandMotifImports_DoesNotMergeProfiles(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				Imports: []orktypes.MotifImport{
					{Motif: profileMotifPath},
				},
				OperatorBox: orktypes.OperatorBoxConfig{},
			},
		},
	}

	require.NoError(t, k.expandMotifImports())
	assert.True(t, k.Profiles.Empty(), "profiles must not be merged from CRD-level imports")

	// Resources from the motif are still merged into the CRD's onReconcile.
	entry := k.enabledCRDs["app"]
	require.NotNil(t, entry.OperatorBox.OnReconcile)
	assert.NotEmpty(t, entry.OperatorBox.OnReconcile.Deployments)
}

// Both import levels used together: profiles from spec.imports, resources from CRD imports.
func TestExpandImports_BothLevels(t *testing.T) {
	k := &Katalog{
		Spec: orktypes.KatalogSpec{
			Imports: []orktypes.MotifImport{
				{Motif: profileMotifPath},
			},
		},
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				Imports: []orktypes.MotifImport{
					{Motif: profileMotifPath},
				},
			},
		},
	}

	require.NoError(t, k.expandKatalogImports())
	require.NoError(t, k.expandMotifImports())

	// Profiles come from spec.imports.
	assert.Len(t, k.Profiles.Resources, 1)

	// Resources come from CRD-level import.
	entry := k.enabledCRDs["app"]
	require.NotNil(t, entry.OperatorBox.OnReconcile)
	assert.NotEmpty(t, entry.OperatorBox.OnReconcile.Deployments)
}
