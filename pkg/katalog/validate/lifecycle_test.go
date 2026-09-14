package validate

import (
	"testing"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/konfig"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers

func katalogWithLifecycle(lc *orktypes.KatalogLifecycle) *executor {
	return newExec(katalog.NewKatalogWithLifecycleForTest(konfig.KatalogKind(), lc))
}

func komposerWithLifecycle(lc *orktypes.KatalogLifecycle) *executor {
	return newExec(katalog.NewKatalogWithLifecycleForTest(konfig.KomposerKind(), lc))
}

func katalogWithPolicy(p *orktypes.KatalogPolicy) *executor {
	return newExec(katalog.NewKatalogWithPolicyForTest(konfig.KatalogKind(), p))
}

// ── maturity ─────────────────────────────────────────────────────────────────

func TestValidateLifecycle_NoLifecycle(t *testing.T) {
	k := katalogWithLifecycle(nil)
	assert.NoError(t, k.validateLifecycle())
}

func TestValidateLifecycle_StableMaturity(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Maturity: orktypes.MaturityStable,
	})
	assert.NoError(t, k.validateLifecycle())
	assert.False(t, k.k.Warnings.HasWarnings())
}

func TestValidateLifecycle_AlphaMaturityWarns(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Maturity: orktypes.MaturityAlpha,
	})
	assert.NoError(t, k.validateLifecycle())
	assert.True(t, k.k.Warnings.HasWarnings())
	assert.Contains(t, k.k.Warnings[0], "alpha")
}

func TestValidateLifecycle_BetaMaturityWarns(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Maturity: orktypes.MaturityBeta,
	})
	assert.NoError(t, k.validateLifecycle())
	assert.True(t, k.k.Warnings.HasWarnings())
	assert.Contains(t, k.k.Warnings[0], "beta")
}

// maturity: deprecated without a deprecation block is now a warning, not an error.
// The deprecation block is the primary signal.
func TestValidateLifecycle_DeprecatedWithoutBlockWarns(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Maturity: orktypes.MaturityDeprecated,
	})
	assert.NoError(t, k.validateLifecycle())
	assert.True(t, k.k.Warnings.HasWarnings())
	assert.Contains(t, k.k.Warnings[0], "lifecycle.deprecation is not set")
}

// A deprecation block alone is sufficient — maturity: deprecated is not required.
func TestValidateLifecycle_DeprecationBlockWithoutMaturityIsValid(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Deprecation: &orktypes.KatalogDeprecation{
			Message: "use v2",
		},
	})
	assert.NoError(t, k.validateLifecycle())
	assert.False(t, k.k.Warnings.HasWarnings())
}

func TestValidateLifecycle_DeprecatedWithDeprecationBlock(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Maturity: orktypes.MaturityDeprecated,
		Deprecation: &orktypes.KatalogDeprecation{
			Message: "use v2",
		},
	})
	assert.NoError(t, k.validateLifecycle())
	assert.False(t, k.k.Warnings.HasWarnings())
}

func TestValidateLifecycle_UnknownMaturity(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Maturity: orktypes.LifecycleMaturity("experimental"),
	})
	err := k.validateLifecycle()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid")
	assert.Contains(t, err.Error(), "experimental")
}

// ── compatibility ─────────────────────────────────────────────────────────────

func TestValidateLifecycle_ValidCompatibility(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Compatibility: &orktypes.LifecycleCompat{
			Kubernetes: ">=1.31",
			Orkestra:   ">=0.7.14",
		},
	})
	assert.NoError(t, k.validateLifecycle())
}

func TestValidateLifecycle_InvalidKubernetesRange(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Compatibility: &orktypes.LifecycleCompat{
			Kubernetes: "!!!invalid",
		},
	})
	err := k.validateLifecycle()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lifecycle.compatibility.kubernetes")
}

func TestValidateLifecycle_InvalidOrkestraRange(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Compatibility: &orktypes.LifecycleCompat{
			Orkestra: "!!!invalid",
		},
	})
	err := k.validateLifecycle()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lifecycle.compatibility.orkestra")
}

// ── katalog Deprecation ────────────────────────────────────────────────────

func katalogWithDeprecation(d *orktypes.KatalogDeprecation) *executor {
	if d == nil {
		return newExec(katalog.NewEmptyKatalog())
	}
	return newExec(katalog.NewKatalogWithLifecycleForTest("", &orktypes.KatalogLifecycle{
		Deprecation: d,
	}))
}

func TestValidateLifecycleDeprecation_Nil(t *testing.T) {
	k := katalogWithDeprecation(nil)
	assert.NoError(t, k.validateLifecycleDeprecation())
}

func TestValidateLifecycleDeprecation_MessageOnly(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
	})
	assert.NoError(t, k.validateLifecycleDeprecation())
}

func TestValidateLifecycleDeprecation_MissingMessage(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		MigratedTo: "mypattern:v2",
	})
	err := k.validateLifecycleDeprecation()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "message is required")
}

func TestValidateLifecycleDeprecation_ValidTimeline(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
		Timeline: &orktypes.DeprecationTimeline{
			From: "2026-01-01",
			To:   "2027-01-01",
		},
	})
	assert.NoError(t, k.validateLifecycleDeprecation())
}

func TestValidateLifecycleDeprecation_BadFromDate(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
		Timeline: &orktypes.DeprecationTimeline{
			From: "not-a-date",
			To:   "2027-01-01",
		},
	})
	err := k.validateLifecycleDeprecation()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeline.from")
	assert.Contains(t, err.Error(), "YYYY-MM-DD")
}

func TestValidateLifecycleDeprecation_BadToDate(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
		Timeline: &orktypes.DeprecationTimeline{
			From: "2026-01-01",
			To:   "01/01/2027",
		},
	})
	err := k.validateLifecycleDeprecation()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeline.to")
	assert.Contains(t, err.Error(), "YYYY-MM-DD")
}

func TestValidateLifecycleDeprecation_FromAfterTo(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
		Timeline: &orktypes.DeprecationTimeline{
			From: "2027-06-01",
			To:   "2027-01-01",
		},
	})
	err := k.validateLifecycleDeprecation()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "from")
	assert.Contains(t, err.Error(), "before")
	assert.Contains(t, err.Error(), "to")
}

func TestValidateLifecycleDeprecation_FromEqualTo(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
		Timeline: &orktypes.DeprecationTimeline{
			From: "2027-01-01",
			To:   "2027-01-01",
		},
	})
	err := k.validateLifecycleDeprecation()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before")
}

func TestValidateLifecycleDeprecation_TimelineFromOnly(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
		Timeline: &orktypes.DeprecationTimeline{
			From: "2026-01-01",
		},
	})
	assert.NoError(t, k.validateLifecycleDeprecation())
}

func TestValidateLifecycleDeprecation_TimelineToOnly(t *testing.T) {
	k := katalogWithDeprecation(&orktypes.KatalogDeprecation{
		Message: "use v2",
		Timeline: &orktypes.DeprecationTimeline{
			To: "2027-01-01",
		},
	})
	assert.NoError(t, k.validateLifecycleDeprecation())
}

// ── kind boundary — accept ────────────────────────────────────────────────────

func TestValidateLifecycle_AcceptOnKatalogIsError(t *testing.T) {
	k := katalogWithLifecycle(&orktypes.KatalogLifecycle{
		Accept: &orktypes.KomposerAccept{
			Patterns: []orktypes.KomposerAcceptEntry{{Name: "some-pattern"}},
		},
	})
	err := k.validateLifecycle()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lifecycle.accept is only valid in a Komposer")
}

func TestValidateLifecycle_AcceptOnKomposerIsValid(t *testing.T) {
	k := komposerWithLifecycle(&orktypes.KatalogLifecycle{
		Accept: &orktypes.KomposerAccept{
			Patterns: []orktypes.KomposerAcceptEntry{
				{Name: "old-operator"},
				{Name: "alpha-import", Author: "myorg"},
			},
		},
	})
	assert.NoError(t, k.validateLifecycle())
}

// ── accept.patterns version field ────────────────────────────────────────────

func TestValidateLifecycle_AcceptPatternVersionValid(t *testing.T) {
	k := komposerWithLifecycle(&orktypes.KatalogLifecycle{
		Accept: &orktypes.KomposerAccept{
			Patterns: []orktypes.KomposerAcceptEntry{
				{Name: "webapp-operator", Version: "=1.0.0"},
				{Name: "cache-operator", Version: ">=0.1.0, <1.0.0"},
			},
		},
	})
	assert.NoError(t, k.validateLifecycle())
}

func TestValidateLifecycle_AcceptPatternVersionInvalid(t *testing.T) {
	k := komposerWithLifecycle(&orktypes.KatalogLifecycle{
		Accept: &orktypes.KomposerAccept{
			Patterns: []orktypes.KomposerAcceptEntry{
				{Name: "webapp-operator", Version: "!!!invalid"},
			},
		},
	})
	err := k.validateLifecycle()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lifecycle.accept.patterns[\"webapp-operator\"].version")
}

func TestValidateLifecycle_AcceptPatternNoVersionIsValid(t *testing.T) {
	k := komposerWithLifecycle(&orktypes.KatalogLifecycle{
		Accept: &orktypes.KomposerAccept{
			Patterns: []orktypes.KomposerAcceptEntry{
				{Name: "webapp-operator"}, // no version = accept all versions
			},
		},
	})
	assert.NoError(t, k.validateLifecycle())
}

// ── KomposerAccept.Accepts helper ─────────────────────────────────────────────

func TestKomposerAccept_Accepts(t *testing.T) {
	a := &orktypes.KomposerAccept{
		Patterns: []orktypes.KomposerAcceptEntry{
			{Name: "old-operator"},
			{Name: "alpha-lib", Author: "myorg"},
		},
	}

	assert.True(t, a.Accepts("old-operator", ""))
	assert.True(t, a.Accepts("old-operator", "anyorg")) // no author filter when entry has no author
	assert.True(t, a.Accepts("alpha-lib", "myorg"))
	assert.False(t, a.Accepts("alpha-lib", "otherorg")) // author mismatch
	assert.False(t, a.Accepts("unknown-pattern", ""))
	assert.False(t, (*orktypes.KomposerAccept)(nil).Accepts("any", ""))
}

// ── policy ────────────────────────────────────────────────────────────────────

func TestValidatePolicy_NilPolicy(t *testing.T) {
	k := katalogWithPolicy(nil)
	assert.NoError(t, k.validatePolicy())
}

func TestValidatePolicy_ValidMinMaturity(t *testing.T) {
	for _, m := range []orktypes.LifecycleMaturity{
		orktypes.MaturityAlpha,
		orktypes.MaturityBeta,
		orktypes.MaturityStable,
	} {
		k := katalogWithPolicy(&orktypes.KatalogPolicy{
			Lifecycle: &orktypes.KatalogLifecyclePolicy{MinMaturity: m},
		})
		assert.NoError(t, k.validatePolicy(), "expected no error for minMaturity: %s", m)
	}
}

func TestValidatePolicy_DeprecatedFloorIsError(t *testing.T) {
	k := katalogWithPolicy(&orktypes.KatalogPolicy{
		Lifecycle: &orktypes.KatalogLifecyclePolicy{MinMaturity: orktypes.MaturityDeprecated},
	})
	err := k.validatePolicy()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy.lifecycle.minMaturity")
	assert.Contains(t, err.Error(), "deprecated")
}

func TestValidatePolicy_UnknownFloorIsError(t *testing.T) {
	k := katalogWithPolicy(&orktypes.KatalogPolicy{
		Lifecycle: &orktypes.KatalogLifecyclePolicy{MinMaturity: orktypes.LifecycleMaturity("production")},
	})
	err := k.validatePolicy()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy.lifecycle.minMaturity")
}
