package katalog

import (
	"testing"
	"time"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
)

// ── Deletion protection ───────────────────────────────────────────────────────

func TestIsDeletionProtectionEnabled(t *testing.T) {
	t.Run("block absent — disabled (no konfig)", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.IsDeletionProtectionEnabled())
	})

	t.Run("block declared, enabled omitted — default-on", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{DeletionProtection: &orktypes.DeletionProtectionConfig{}}}
		assert.True(t, k.IsDeletionProtectionEnabled())
	})

	t.Run("block declared, enabled: false — disabled", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			DeletionProtection: &orktypes.DeletionProtectionConfig{Enabled: boolPtr(false)},
		}}
		assert.False(t, k.IsDeletionProtectionEnabled())
	})
}

func TestDeletionProtectionServiceName(t *testing.T) {
	t.Run("YAML value takes precedence", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			DeletionProtection: &orktypes.DeletionProtectionConfig{ServiceName: "custom-svc"},
		}}
		assert.Equal(t, "custom-svc", k.DeletionProtectionServiceName())
	})

	t.Run("falls back to hard default when no konfig", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, orkGate, k.DeletionProtectionServiceName())
	})
}

func TestDeletionProtectionFailurePolicy(t *testing.T) {
	t.Run("YAML value takes precedence", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			DeletionProtection: &orktypes.DeletionProtectionConfig{FailurePolicy: "Ignore"},
		}}
		assert.Equal(t, "Ignore", k.DeletionProtectionFailurePolicy())
	})

	t.Run("falls back to hard default (Fail)", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, "Fail", k.DeletionProtectionFailurePolicy())
	})
}

func TestDeletionProtectionCleanupOnShutdown(t *testing.T) {
	t.Run("YAML true", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			DeletionProtection: &orktypes.DeletionProtectionConfig{CleanupOnShutdown: true},
		}}
		assert.True(t, k.DeletionProtectionCleanupOnShutdown())
	})

	t.Run("block absent — falls back to default (false, no konfig)", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.DeletionProtectionCleanupOnShutdown())
	})
}

func TestIsStrictModeEnabled(t *testing.T) {
	t.Run("no deletion protection block — false", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.IsStrictModeEnabled())
	})

	t.Run("strictMode: true", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			DeletionProtection: &orktypes.DeletionProtectionConfig{StrictMode: true},
		}}
		assert.True(t, k.IsStrictModeEnabled())
	})
}

// ── Namespace protection ──────────────────────────────────────────────────────

func TestIsNamespaceProtectionEnabled(t *testing.T) {
	t.Run("block absent — disabled (no konfig)", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.IsNamespaceProtectionEnabled())
	})

	t.Run("block declared, enabled omitted — default-on", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{NamespaceProtection: &orktypes.NamespaceProtectionConfig{}}}
		assert.True(t, k.IsNamespaceProtectionEnabled())
	})

	t.Run("block declared, enabled: false — disabled", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			NamespaceProtection: &orktypes.NamespaceProtectionConfig{Enabled: boolPtr(false)},
		}}
		assert.False(t, k.IsNamespaceProtectionEnabled())
	})
}

func TestNamespaceProtectionServiceName(t *testing.T) {
	t.Run("YAML value takes precedence", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			NamespaceProtection: &orktypes.NamespaceProtectionConfig{ServiceName: "ns-svc"},
		}}
		assert.Equal(t, "ns-svc", k.NamespaceProtectionServiceName())
	})

	t.Run("falls back to hard default when no konfig", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, orkGate, k.NamespaceProtectionServiceName())
	})
}

func TestNamespaceProtectionFailurePolicy(t *testing.T) {
	t.Run("YAML value takes precedence", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			NamespaceProtection: &orktypes.NamespaceProtectionConfig{FailurePolicy: "Ignore"},
		}}
		assert.Equal(t, "Ignore", k.NamespaceProtectionFailurePolicy())
	})

	t.Run("falls back to hard default (Fail)", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, "Fail", k.NamespaceProtectionFailurePolicy())
	})
}

func TestNamespaceProtectionCleanupOnShutdown(t *testing.T) {
	t.Run("YAML true", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			NamespaceProtection: &orktypes.NamespaceProtectionConfig{CleanupOnShutdown: true},
		}}
		assert.True(t, k.NamespaceProtectionCleanupOnShutdown())
	})

	t.Run("block absent — falls back to default (false, no konfig)", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.NamespaceProtectionCleanupOnShutdown())
	})
}

// ── Admission / conversion webhooks ───────────────────────────────────────────

func TestIsAdmissionEnabled(t *testing.T) {
	t.Run("block absent — disabled (no konfig)", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.IsAdmissionEnabled())
	})

	t.Run("declared but no explicit value — no default-on", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			Webhooks: &orktypes.WebhooksConfig{Admission: &orktypes.AdmissionWebhookToggle{}},
		}}
		assert.False(t, k.IsAdmissionEnabled())
	})

	t.Run("explicitly enabled", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{
			Webhooks: &orktypes.WebhooksConfig{Admission: &orktypes.AdmissionWebhookToggle{Enabled: boolPtr(true)}},
		}}
		assert.True(t, k.IsAdmissionEnabled())
	})
}

func TestIsConversionEnabled(t *testing.T) {
	t.Run("block absent — disabled (no konfig)", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.IsConversionEnabled())
	})

	t.Run("declared but no explicit value — no default-on", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{Conversion: &orktypes.ConversionConfig{}}}
		assert.False(t, k.IsConversionEnabled())
	})

	t.Run("explicitly enabled", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{Conversion: &orktypes.ConversionConfig{Enabled: boolPtr(true)}}}
		assert.True(t, k.IsConversionEnabled())
	})
}

func TestWebhooksServiceName(t *testing.T) {
	t.Run("YAML value takes precedence", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{Webhooks: &orktypes.WebhooksConfig{ServiceName: "wh-svc"}}}
		assert.Equal(t, "wh-svc", k.WebhooksServiceName())
	})

	t.Run("falls back to hard default when no konfig", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, orkGate, k.WebhooksServiceName())
	})
}

func TestWebhooksFailurePolicy(t *testing.T) {
	t.Run("YAML value takes precedence", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{Webhooks: &orktypes.WebhooksConfig{FailurePolicy: "Fail"}}}
		assert.Equal(t, "Fail", k.WebhooksFailurePolicy())
	})

	t.Run("falls back to hard default (Ignore)", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, "Ignore", k.WebhooksFailurePolicy())
	})
}

func TestWebhookCleanupOnShutdown(t *testing.T) {
	t.Run("YAML true", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{Webhooks: &orktypes.WebhooksConfig{CleanupOnShutdown: true}}}
		assert.True(t, k.WebhookCleanupOnShutdown())
	})

	t.Run("block absent — falls back to default (false, no konfig)", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.WebhookCleanupOnShutdown())
	})
}

func TestConversionWindow(t *testing.T) {
	t.Run("YAML value > 0 takes precedence", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{Conversion: &orktypes.ConversionConfig{ConversionWindow: 250}}}
		assert.Equal(t, 250, k.ConversionWindow())
	})

	t.Run("YAML zero — falls back to hard default (100)", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{Conversion: &orktypes.ConversionConfig{}}}
		assert.Equal(t, 100, k.ConversionWindow())
	})

	t.Run("block absent — falls back to hard default (100)", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, 100, k.ConversionWindow())
	})
}

// ── Gateway config ────────────────────────────────────────────────────────────

func TestIsGatewayEnabled(t *testing.T) {
	t.Run("nil gateway — disabled", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.IsGatewayEnabled())
	})

	t.Run("gateway present but every field zero — still enabled", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{}}
		assert.True(t, k.IsGatewayEnabled())
	})

	t.Run("gateway.enabled: true — enabled", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{Enabled: true}}
		assert.True(t, k.IsGatewayEnabled())
	})

	t.Run("gateway.endpoint set, enabled omitted — enabled", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{Endpoint: "https://gateway.internal"}}
		assert.True(t, k.IsGatewayEnabled())
	})

	t.Run("only gateway API declared, no enabled/endpoint — enabled", func(t *testing.T) {
		// The CI-only Gateway API client scenario: no runtime pairing, no
		// gateway.endpoint — just a CRUD surface for CI/Backstage/custom UIs
		// to call. The gateway: block still declares real config, so it
		// must count as enabled.
		k := &Katalog{Gateway: &orktypes.GatewayConfig{
			API: &orktypes.GatewayAPIConfig{Enabled: true},
		}}
		assert.True(t, k.IsGatewayEnabled())
	})
}

func TestIsGatewayAPIEnabled(t *testing.T) {
	t.Run("nil gateway", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.IsGatewayAPIEnabled())
	})

	t.Run("gateway present, gateway API nil", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{}}
		assert.False(t, k.IsGatewayAPIEnabled())
	})

	t.Run("gateway API present but not enabled", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{API: &orktypes.GatewayAPIConfig{}}}
		assert.False(t, k.IsGatewayAPIEnabled())
	})

	t.Run("gateway API enabled", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{API: &orktypes.GatewayAPIConfig{Enabled: true}}}
		assert.True(t, k.IsGatewayAPIEnabled())
	})
}

func TestHasGatewayAPISecretRefs(t *testing.T) {
	t.Run("gateway API disabled — false", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.HasGatewayAPISecretRefs())
	})

	t.Run("enabled, no tokens use secretRef", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{API: &orktypes.GatewayAPIConfig{
			Enabled: true,
			Auth:    orktypes.APIAuth{Tokens: []orktypes.APIToken{{Name: "ci", Token: "${ORK_CI_TOKEN}"}}},
		}}}
		assert.False(t, k.HasGatewayAPISecretRefs())
	})

	t.Run("enabled, one token uses secretRef", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{API: &orktypes.GatewayAPIConfig{
			Enabled: true,
			Auth: orktypes.APIAuth{Tokens: []orktypes.APIToken{
				{Name: "ci", Token: "${ORK_CI_TOKEN}"},
				{Name: "cc", SecretRef: &orktypes.APISecretRef{Name: "ork-apply-token", Key: "token"}},
			}},
		}}}
		assert.True(t, k.HasGatewayAPISecretRefs())
	})
}

func TestHasServeEnabled(t *testing.T) {
	t.Run("gateway API disabled — false", func(t *testing.T) {
		k := &Katalog{enabledCRDs: map[string]orktypes.CRDEntry{
			"Website": {Name: "Website", Serve: &orktypes.ServeConfig{Enabled: true}},
		}}
		assert.False(t, k.HasServeEnabled())
	})

	t.Run("gateway API enabled, no CRD opts into serve", func(t *testing.T) {
		k := &Katalog{
			Gateway:     &orktypes.GatewayConfig{API: &orktypes.GatewayAPIConfig{Enabled: true}},
			enabledCRDs: map[string]orktypes.CRDEntry{"Website": {Name: "Website"}},
		}
		assert.False(t, k.HasServeEnabled())
	})

	t.Run("gateway API enabled, one CRD opts into serve", func(t *testing.T) {
		k := &Katalog{
			Gateway: &orktypes.GatewayConfig{API: &orktypes.GatewayAPIConfig{Enabled: true}},
			enabledCRDs: map[string]orktypes.CRDEntry{
				"Website": {Name: "Website", Serve: &orktypes.ServeConfig{Enabled: true}},
			},
		}
		assert.True(t, k.HasServeEnabled())
	})
}

func TestIsStandaloneGateway(t *testing.T) {
	t.Run("nil gateway", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.IsStandaloneGateway())
	})

	t.Run("gateway present, standalone false", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{}}
		assert.False(t, k.IsStandaloneGateway())
	})

	t.Run("standalone: true", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{Standalone: true}}
		assert.True(t, k.IsStandaloneGateway())
	})
}

func TestGatewayEndpoint(t *testing.T) {
	t.Run("YAML value takes precedence", func(t *testing.T) {
		k := &Katalog{Gateway: &orktypes.GatewayConfig{Endpoint: "https://gateway.internal"}}
		assert.Equal(t, "https://gateway.internal", k.GatewayEndpoint())
	})

	t.Run("no gateway, no konfig — empty", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, "", k.GatewayEndpoint())
	})
}

func TestClusterName(t *testing.T) {
	t.Run("metadata.clusterName set", func(t *testing.T) {
		k := &Katalog{}
		k.metadata.ClusterName = "prod-us-east"
		assert.Equal(t, "prod-us-east", k.ClusterName())
	})

	t.Run("no metadata, no konfig — defaults to runtime name", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, "orkestra-runtime", k.ClusterName())
	})
}

// ── Gateway / certificate requirement ─────────────────────────────────────────

func TestNeedsCertificates(t *testing.T) {
	t.Run("nothing configured — false", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.NeedsCertificates())
	})

	t.Run("deletion protection enabled — true", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{DeletionProtection: &orktypes.DeletionProtectionConfig{}}}
		assert.True(t, k.NeedsCertificates())
	})

	t.Run("namespace protection enabled — true", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{NamespaceProtection: &orktypes.NamespaceProtectionConfig{}}}
		assert.True(t, k.NeedsCertificates())
	})
}

func TestNeedsGateway(t *testing.T) {
	t.Run("nothing configured — false", func(t *testing.T) {
		k := &Katalog{}
		assert.False(t, k.NeedsGateway())
	})

	t.Run("deletion protection enabled — needs gateway for certs", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{DeletionProtection: &orktypes.DeletionProtectionConfig{}}}
		assert.True(t, k.NeedsGateway())
	})
}

func TestIsHousekeeperEnabled(t *testing.T) {
	k := &Katalog{}
	assert.False(t, k.IsHousekeeperEnabled()) // no konfig — hard default false
}

func TestHousekeeperSyncInterval(t *testing.T) {
	k := &Katalog{}
	assert.Equal(t, 30*time.Second, k.HousekeeperSyncInterval()) // no konfig — hard default
}

// ── Certificate manager ───────────────────────────────────────────────────────

func TestCertAutoRotate(t *testing.T) {
	t.Run("YAML declared, autoRotate: false", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{CertManager: &orktypes.CertManagerConfig{AutoRotate: boolPtr(false)}}}
		assert.False(t, k.CertAutoRotate())
	})

	t.Run("block absent — falls back to hard default (true)", func(t *testing.T) {
		k := &Katalog{}
		assert.True(t, k.CertAutoRotate())
	})
}

func TestCertRotationThreshold(t *testing.T) {
	t.Run("YAML value parsed", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{CertManager: &orktypes.CertManagerConfig{RotationThreshold: "7d"}}}
		assert.Equal(t, 7*24*time.Hour, k.CertRotationThreshold())
	})

	t.Run("block absent — falls back to hard default (30d)", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, 30*24*time.Hour, k.CertRotationThreshold())
	})
}

func TestCertValidFor(t *testing.T) {
	t.Run("YAML value parsed", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{CertManager: &orktypes.CertManagerConfig{ValidFor: "6mo"}}}
		assert.Equal(t, 6*30*24*time.Hour, k.CertValidFor())
	})

	t.Run("block absent — falls back to hard default (1y)", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, 365*24*time.Hour, k.CertValidFor())
	})
}

func TestCertValidForStr(t *testing.T) {
	t.Run("YAML value takes precedence", func(t *testing.T) {
		k := &Katalog{Security: orktypes.KatalogSecurity{CertManager: &orktypes.CertManagerConfig{ValidFor: "6mo"}}}
		assert.Equal(t, "6mo", k.CertValidForStr())
	})

	t.Run("block absent — falls back to hard default (1y)", func(t *testing.T) {
		k := &Katalog{}
		assert.Equal(t, "1y", k.CertValidForStr())
	})
}
