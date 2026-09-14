package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orkspace/orkestra/pkg/katalog"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithPublish(pub *orktypes.PublishConfig) *executor {
	return newExec(&katalog.Katalog{Publish: pub})
}

// ── validatePublish ──────────────────────────────────────────────────────────

func TestValidatePublish_Nil(t *testing.T) {
	k := katalogWithPublish(nil)
	assert.NoError(t, k.validatePublish())
}

func TestValidatePublish_EmptyConfig(t *testing.T) {
	k := katalogWithPublish(&orktypes.PublishConfig{})
	assert.NoError(t, k.validatePublish())
}

// ── signing ──────────────────────────────────────────────────────────────────

func TestValidatePublish_Signing_NoIdentities(t *testing.T) {
	k := katalogWithPublish(&orktypes.PublishConfig{
		Signing: &orktypes.SigningConfig{Verify: true},
	})
	assert.NoError(t, k.validatePublish())
}

func TestValidatePublish_Signing_ValidIdentities(t *testing.T) {
	k := katalogWithPublish(&orktypes.PublishConfig{
		Signing: &orktypes.SigningConfig{
			ExpectedIdentities: []string{
				"github.com/myorg/myrepo/.github/workflows/release.yaml@refs/heads/main",
				"gitlab.com/mygroup/myproject//release@refs/heads/main",
			},
		},
	})
	assert.NoError(t, k.validatePublish())
}

func TestValidatePublish_Signing_EmptyIdentityFirst(t *testing.T) {
	k := katalogWithPublish(&orktypes.PublishConfig{
		Signing: &orktypes.SigningConfig{
			ExpectedIdentities: []string{""},
		},
	})
	err := k.validatePublish()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expectedIdentities[0]")
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestValidatePublish_Signing_EmptyIdentityMiddle(t *testing.T) {
	k := katalogWithPublish(&orktypes.PublishConfig{
		Signing: &orktypes.SigningConfig{
			ExpectedIdentities: []string{
				"github.com/myorg/myrepo/.github/workflows/release.yaml@refs/heads/main",
				"",
			},
		},
	})
	err := k.validatePublish()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expectedIdentities[1]")
	assert.Contains(t, err.Error(), "must not be empty")
}

// ── publish.tests.intent ─────────────────────────────────────────────────────

func TestValidatePublish_Tests_IntentNil(t *testing.T) {
	k := katalogWithPublish(&orktypes.PublishConfig{
		Tests: &orktypes.PublishTestsConfig{},
	})
	assert.NoError(t, k.validatePublish())
}

func TestValidatePublish_Tests_IntentFalse(t *testing.T) {
	k := katalogWithPublish(&orktypes.PublishConfig{
		Tests: &orktypes.PublishTestsConfig{Intent: boolPtr(false)},
	})
	assert.NoError(t, k.validatePublish())
}

func TestValidatePublish_Tests_IntentTrue_NoGateway(t *testing.T) {
	k := katalogWithPublish(&orktypes.PublishConfig{
		Tests: &orktypes.PublishTestsConfig{Intent: boolPtr(true)},
	})
	err := k.validatePublish()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "intent")
	assert.Contains(t, err.Error(), "gateway.api.enabled")
}

func TestValidatePublish_Tests_IntentTrue_GatewayNoAPIBlock(t *testing.T) {
	k := katalogWithPublish(&orktypes.PublishConfig{
		Tests: &orktypes.PublishTestsConfig{Intent: boolPtr(true)},
	})
	k.k.Gateway = &orktypes.GatewayConfig{}
	err := k.validatePublish()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway.api.enabled")
}

func TestValidatePublish_Tests_IntentTrue_GatewayAPIDisabled(t *testing.T) {
	k := katalogWithPublish(&orktypes.PublishConfig{
		Tests: &orktypes.PublishTestsConfig{Intent: boolPtr(true)},
	})
	k.k.Gateway = &orktypes.GatewayConfig{
		API: &orktypes.GatewayAPIConfig{Enabled: false},
	}
	err := k.validatePublish()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway.api.enabled")
}

func TestValidatePublish_Tests_IntentTrue_NoIntentFiles(t *testing.T) {
	dir := t.TempDir()
	k := katalogWithPublish(&orktypes.PublishConfig{
		Tests: &orktypes.PublishTestsConfig{Intent: boolPtr(true)},
	})
	k.k.Gateway = &orktypes.GatewayConfig{
		API: &orktypes.GatewayAPIConfig{Enabled: true},
	}
	k.k.SetKatalogDirForTest(dir)
	err := k.validatePublish()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "intent.yaml")
	assert.Contains(t, err.Error(), "intent.json")
}

func TestValidatePublish_Tests_IntentTrue_WithIntentYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "intent.yaml"), []byte("{}"), 0o644))
	k := katalogWithPublish(&orktypes.PublishConfig{
		Tests: &orktypes.PublishTestsConfig{Intent: boolPtr(true)},
	})
	k.k.Gateway = &orktypes.GatewayConfig{
		API: &orktypes.GatewayAPIConfig{Enabled: true},
	}
	k.k.SetKatalogDirForTest(dir)
	assert.NoError(t, k.validatePublish())
}

func TestValidatePublish_Tests_IntentTrue_WithIntentJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "intent.json"), []byte("{}"), 0o644))
	k := katalogWithPublish(&orktypes.PublishConfig{
		Tests: &orktypes.PublishTestsConfig{Intent: boolPtr(true)},
	})
	k.k.Gateway = &orktypes.GatewayConfig{
		API: &orktypes.GatewayAPIConfig{Enabled: true},
	}
	k.k.SetKatalogDirForTest(dir)
	assert.NoError(t, k.validatePublish())
}
