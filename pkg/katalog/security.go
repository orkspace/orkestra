// pkg/katalog/security.go
//
// Security accessors on *Katalog.
//
// Precedence (highest → lowest):
//  1. Katalog YAML (k.Security)
//  2. ENV vars via SecurityConfig (k.konfig.Security())
//  3. Hard defaults coded below
//
// KatalogSecurity uses *bool fields which allows detecting
// "not declared" (nil) vs "explicitly false" (*false).
//
// Deletion protection is ENABLED BY DEFAULT when the security.deletionProtection block
// is present but deletionProtection.enabled is not declared.
package katalog

import (
	"time"
)

// securityEnvDefaults returns the SecurityConfig from konfig, or a zero
// value when konfig is not wired (e.g. NewEmptyKatalog in tests).
func (k *Katalog) securityEnvDefaults() interface {
	RuntimeServiceName() string
	GatewayServiceName() string
	DeletionProtectionEnabled() bool
	DeletionProtectionSvcName() string
	DeletionProtectionPolicy() string
	DeletionProtectionCleanup() bool
	AdmissionEnabled() bool
	ConversionEnabled() bool
	WebhooksSvcName() string
	WebhooksPolicy() string
	WebhookCleanup() bool
	ConversionWindowVal() int
	HousekeeperEnabled() bool
	HousekeeperSyncInterval() time.Duration
	NamespaceProtectionEnabled() bool
	NamespaceProtectionSvcName() string
	NamespaceProtectionPolicy() string
	NamespaceProtectionCleanup() bool
	CertAutoRotate() bool
	CertValidForStr() string
	CertRotationThresholdStr() string
} {
	return &envSecurityReader{k: k}
}

const (
	orkRun  = "orkestra-runtime"
	orkGate = "orkestra-gateway"
)

// envSecurityReader adapts *konfig.SecurityConfig through a small interface
// so that security.go does not import konfig directly.
type envSecurityReader struct{ k *Katalog }

func (r *envSecurityReader) RuntimeServiceName() string {
	if r.k.konfig == nil {
		return orkRun
	}
	return r.k.konfig.Security().ServiceName.Runtime
}

func (r *envSecurityReader) GatewayServiceName() string {
	if r.k.konfig == nil {
		return orkGate
	}
	return r.k.konfig.Security().ServiceName.Gateway
}

func (r *envSecurityReader) DeletionProtectionEnabled() bool {
	if r.k.konfig == nil {
		return false
	}
	return r.k.konfig.Security().DeletionProtection.Enabled
}
func (r *envSecurityReader) DeletionProtectionSvcName() string {
	if r.k.konfig == nil {
		return orkGate
	}
	return r.k.konfig.Security().DeletionProtection.ServiceName
}
func (r *envSecurityReader) DeletionProtectionPolicy() string {
	if r.k.konfig == nil {
		return "Fail"
	}
	return r.k.konfig.Security().DeletionProtection.FailurePolicy
}
func (r *envSecurityReader) AdmissionEnabled() bool {
	if r.k.konfig == nil {
		return false
	}
	return r.k.konfig.Security().Webhooks.Admission.Enabled
}
func (r *envSecurityReader) ConversionEnabled() bool {
	if r.k.konfig == nil {
		return false
	}
	return r.k.konfig.Security().Conversion.Enabled
}
func (r *envSecurityReader) WebhooksSvcName() string {
	if r.k.konfig == nil {
		return orkGate
	}
	return r.k.konfig.Security().Webhooks.ServiceName
}
func (r *envSecurityReader) WebhooksPolicy() string {
	if r.k.konfig == nil {
		return "Ignore"
	}
	return r.k.konfig.Security().Webhooks.FailurePolicy
}

func (r *envSecurityReader) WebhookCleanup() bool {
	if r.k.konfig == nil {
		return false
	}
	return r.k.konfig.Security().Webhooks.CleanupOnShutdown
}
func (r *envSecurityReader) DeletionProtectionCleanup() bool {
	if r.k.konfig == nil {
		return false
	}
	return r.k.konfig.Security().DeletionProtection.CleanupOnShutdown
}
func (r *envSecurityReader) NamespaceProtectionCleanup() bool {
	if r.k.konfig == nil {
		return false
	}
	return r.k.konfig.Security().NamespaceProtection.CleanupOnShutdown
}

func (r *envSecurityReader) ConversionWindowVal() int {
	if r.k.konfig == nil {
		return 100
	}
	return r.k.konfig.Security().Conversion.ConversionWindow
}
func (r *envSecurityReader) HousekeeperEnabled() bool {
	if r.k.konfig == nil {
		return false
	}
	return r.k.konfig.Security().Webhooks.Housekeeper.Enabled
}
func (r *envSecurityReader) HousekeeperSyncInterval() time.Duration {
	if r.k.konfig == nil {
		return 30 * time.Second
	}
	return r.k.konfig.Security().Webhooks.Housekeeper.SyncInterval
}
func (r *envSecurityReader) NamespaceProtectionEnabled() bool {
	if r.k.konfig == nil {
		return false
	}
	return r.k.konfig.Security().NamespaceProtection.Enabled
}
func (r *envSecurityReader) NamespaceProtectionPolicy() string {
	if r.k.konfig == nil {
		return "Fail"
	}
	return r.k.konfig.Security().NamespaceProtection.FailurePolicy
}
func (r *envSecurityReader) NamespaceProtectionSvcName() string {
	if r.k.konfig == nil {
		return orkGate
	}
	return r.k.konfig.Security().NamespaceProtection.ServiceName
}

func (r *envSecurityReader) CertAutoRotate() bool {
	if r.k.konfig == nil {
		return true
	}
	return r.k.konfig.Security().CertManager.AutoRotate
}

func (r *envSecurityReader) CertRotationThresholdStr() string {
	if r.k.konfig == nil {
		return "30d"
	}
	return r.k.konfig.Security().CertManager.RotationThreshold
}

func (r *envSecurityReader) CertValidForStr() string {
	if r.k.konfig == nil {
		return "1y"
	}
	return r.k.konfig.Security().CertManager.ValidFor
}

//

// ── Deletion protection ───────────────────────────────────────────────────────

// IsDeletionProtectionEnabled reports whether deletion protection is active.
//
// Precedence:
//
//	YAML security.deletionProtection block present → use YAML value (default-on when block declared)
//	YAML block absent                              → fall back to ENABLE_DELETION_PROTECTION env
func (k *Katalog) IsDeletionProtectionEnabled() bool {
	if k.Security.DeletionProtection != nil {
		// YAML declared the block — apply its enabled semantics.
		return k.Security.IsDeletionProtectionEnabled()
	}
	return k.securityEnvDefaults().DeletionProtectionEnabled()
}

// DeletionProtectionServiceName returns the effective service name for deletion protection.
// YAML value takes precedence over ENV.
func (k *Katalog) DeletionProtectionServiceName() string {
	env := k.securityEnvDefaults()
	return k.Security.DeletionProtectionServiceName(env.DeletionProtectionSvcName())
}

// Runtime Service Name returns the effective service name for orkestra runtime.
// YAML value takes precedence over ENV.
func (k *Katalog) RuntimeServiceName() string {
	env := k.securityEnvDefaults()
	return k.Security.RuntimeServiceName(env.RuntimeServiceName())
}

// Gateway Service Name returns the effective service name for orkestra gateway.
// YAML value takes precedence over ENV.
func (k *Katalog) GatewayServiceName() string {
	env := k.securityEnvDefaults()
	return k.Security.GatewayServiceName(env.GatewayServiceName())
}

// DeletionProtectionFailurePolicy returns the effective failure policy string.
// YAML value takes precedence over ENV.
func (k *Katalog) DeletionProtectionFailurePolicy() string {
	if k.Security.DeletionProtection != nil && k.Security.DeletionProtection.FailurePolicy != "" {
		return k.Security.DeletionProtection.FailurePolicy
	}
	return k.securityEnvDefaults().DeletionProtectionPolicy()
}

// IsStrictModeEnabled reports whether strict mode is active for deletion protection.
// Strict mode treats removal of the deletion-protection label as a deletion attempt.
// Only valid when deletion protection is also enabled.
func (k *Katalog) IsStrictModeEnabled() bool {
	if k.Security.DeletionProtection == nil {
		return false
	}
	return k.Security.DeletionProtection.StrictMode
}

// ── Namespace protection ───────────────────────────────────────────────────────

// IsNamespaceProtectionEnabled reports whether namespace protection is active.
//
// Precedence:
//
//	YAML security.namespaceProtection block present → use YAML value (default-on when block declared)
//	YAML block absent                               → fall back to ENABLE_NAMESPACE_PROTECTION env
func (k *Katalog) IsNamespaceProtectionEnabled() bool {
	if k.Security.NamespaceProtection != nil {
		// YAML declared the block — apply its enabled semantics.
		return k.Security.IsNamespaceProtectionEnabled()
	}
	return k.securityEnvDefaults().NamespaceProtectionEnabled()
}

// NamespaceProtectionServiceName returns the effective service name for namespace protection.
// YAML value takes precedence over ENV.
func (k *Katalog) NamespaceProtectionServiceName() string {
	env := k.securityEnvDefaults()
	return k.Security.NamespaceProtectionServiceName(env.NamespaceProtectionSvcName())
}

// NamespaceProtectionFailurePolicy returns the effective failure policy string.
// YAML value takes precedence over ENV.
func (k *Katalog) NamespaceProtectionFailurePolicy() string {
	if k.Security.NamespaceProtection != nil && k.Security.NamespaceProtection.FailurePolicy != "" {
		return k.Security.NamespaceProtection.FailurePolicy
	}
	return k.securityEnvDefaults().NamespaceProtectionPolicy()
}

// NamespaceProtectionCleanupOnShutdown reports whether Deletion Protection should be deleted on shutdown.
//
// Precedence:
//
//	YAML security.namespaceProtection.cleanupOnShutdown present → use YAML value
//	YAML block absent                                      → fall back to NAMESPACE_PROTECTION_CLEANUP_ON_SHUTDOWN env
func (k *Katalog) NamespaceProtectionCleanupOnShutdown() bool {
	if k.Security.NamespaceProtection != nil {
		return k.Security.NamespaceProtection.CleanupOnShutdown
	}
	return k.securityEnvDefaults().NamespaceProtectionCleanup()
}

// ── Admission webhooks ────────────────────────────────────────────────────────

// IsAdmissionEnabled reports whether admission webhooks are globally enabled.
//
// Precedence:
//
//	YAML security.webhooks.admission block present → use YAML value
//	YAML block absent                              → fall back to ENABLE_ADMISSION_WEBHOOK env
func (k *Katalog) IsAdmissionEnabled() bool {
	if k.Security.Webhooks != nil && k.Security.Webhooks.Admission != nil {
		return k.Security.IsAdmissionEnabled()
	}
	return k.securityEnvDefaults().AdmissionEnabled()
}

// ── Conversion webhook ────────────────────────────────────────────────────────

// IsConversionEnabled reports whether the conversion webhook is globally enabled.
//
// Precedence:
//
//	YAML security.conversion block present → use YAML value
//	YAML block absent                      → fall back to ENABLE_CONVERSION env
func (k *Katalog) IsConversionEnabled() bool {
	if k.Security.Conversion != nil {
		return k.Security.IsConversionEnabled()
	}
	return k.securityEnvDefaults().ConversionEnabled()
}

// WebhooksServiceName returns the effective service name for admission/conversion webhooks.
// YAML value takes precedence over ENV.
func (k *Katalog) WebhooksServiceName() string {
	env := k.securityEnvDefaults()
	return k.Security.WebhooksServiceName(env.WebhooksSvcName())
}

// WebhooksFailurePolicy returns the effective failure policy for admission webhooks.
// YAML value takes precedence over ENV.
func (k *Katalog) WebhooksFailurePolicy() string {
	env := k.securityEnvDefaults()
	return k.Security.WebhooksFailurePolicy(env.WebhooksPolicy())
}

// DeletionProtectionCleanupOnShutdown reports whether Deletion Protection should be deleted on shutdown.
//
// Precedence:
//
//	YAML security.deletionProtection.cleanupOnShutdown present → use YAML value
//	YAML block absent                                      → fall back to DELETION_PROTECTION_CLEANUP_ON_SHUTDOWN env
func (k *Katalog) DeletionProtectionCleanupOnShutdown() bool {
	if k.Security.DeletionProtection != nil {
		return k.Security.DeletionProtection.CleanupOnShutdown
	}
	return k.securityEnvDefaults().DeletionProtectionCleanup()
}

// WebhookCleanupOnShutdown reports whether admission webhooks should be deleted on shutdown.
//
// Precedence:
//
//	YAML security.webhooks.cleanupOnShutdown present → use YAML value
//	YAML block absent                                → fall back to WEBHOOK_CLEANUP_ON_SHUTDOWN env
func (k *Katalog) WebhookCleanupOnShutdown() bool {
	if k.Security.Webhooks != nil {
		return k.Security.Webhooks.CleanupOnShutdown
	}
	return k.securityEnvDefaults().WebhookCleanup()
}

// ── Conversion stats window ───────────────────────────────────────────────────

// ConversionWindow returns the effective rolling window size for conversion/admission stats.
//
// Precedence:
//
//	YAML security.conversion.conversionWindow > 0 → use YAML value
//	YAML absent or zero                           → fall back to CONVERSION_WINDOW env
func (k *Katalog) ConversionWindow() int {
	if k.Security.Conversion != nil && k.Security.Conversion.ConversionWindow > 0 {
		return k.Security.Conversion.ConversionWindow
	}
	return k.securityEnvDefaults().ConversionWindowVal()
}

// ── Gateway config ────────────────────────────────────────────────────────────

// IsGatewayEnabled reports whether this Katalog declares a gateway: block at
// all — deliberately just a nil check, not a check of individual fields
// within it (enabled, endpoint, api, …). Any declared gateway: block
// means the katalog expects a gateway to exist; checking specific fields
// undercounts as the block grows new standalone-meaningful sections (e.g.
// api: with no enabled: or endpoint: set, as in a CI-only Gateway API
// client setup).
func (k *Katalog) IsGatewayEnabled() bool {
	if k == nil {
		return false
	}
	return k.Gateway != nil
}

// IsGatewayAPIEnabled reports whether the Gateway Gateway API flag is set.
func (k *Katalog) IsGatewayAPIEnabled() bool {
	if k == nil {
		return false
	}
	return k.Gateway != nil && k.Gateway.API != nil && k.Gateway.API.Enabled
}

// HasGatewayAPISecretRefs reports whether any Gateway API token entry uses secretRef.
// Used by GenerateGatewayRBACRules to add secrets get/create permissions.
func (k *Katalog) HasGatewayAPISecretRefs() bool {
	if !k.IsGatewayAPIEnabled() {
		return false
	}
	for _, t := range k.Gateway.API.Auth.Tokens {
		if t.SecretRef != nil {
			return true
		}
	}
	return false
}

// HasServeEnabled reports whether the Gateway API is enabled and at least one CRD
// has serve.enabled: true. This is the activation gate — callers should use this,
// not IsGatewayAPIEnabled, to decide whether to register serve routes or RBAC.
func (k *Katalog) HasServeEnabled() bool {
	if k == nil {
		return false
	}
	if !k.IsGatewayAPIEnabled() {
		return false
	}
	for _, crd := range k.Enabled() {
		if crd.ServeEnabled() {
			return true
		}
	}
	return false
}

// IsStandaloneGateway reports whether this Katalog is deployed as a standalone
// gateway with no companion runtime operator.
//
// When true:
//   - gatewayEndpoint validation is skipped
//   - spec: may be empty (no CRDs required)
func (k *Katalog) IsStandaloneGateway() bool {
	return k.Gateway != nil && k.Gateway.Standalone
}

// GatewayEndpoint returns the effective gateway endpoint URL.
//
// Precedence:
//
//	YAML gateway.endpoint non-empty          → use gateway block value
//	If not set          	                 → fall back to ORK_GATEWAY_ENDPOINT env
func (k *Katalog) GatewayEndpoint() string {
	if k.Gateway != nil && k.Gateway.Endpoint != "" {
		return k.Gateway.Endpoint
	}
	if k.konfig != nil {
		return k.konfig.GatewayEndpoint()
	}
	return ""
}

// ClusterName returns the effective cluster name for this Katalog.
//
// Precedence:
//
//	metadata.clusterName non-empty → use Katalog value
//	CLUSTER_NAME env var set       → use konfig value
//	Neither set                    → orkestra-runtime
func (k *Katalog) ClusterName() string {
	if k.metadata.ClusterName != "" {
		return k.metadata.ClusterName
	}
	if k.konfig != nil {
		return k.konfig.Cluster().Name()
	}
	return orkRun
}

// ── Gateway requirement ───────────────────────────────────────────────────────

// NeedsGateway reports whether this Katalog requires a companion gateway process.
//
// A gateway is required when any of the following are configured:
//   - Security features that run on the gateway's HTTPS server
//     (deletion protection, admission webhooks, conversion, namespace protection)
//   - Notifications — unless standalone: true is declared (or implied by local dev)
//
// Used by Validate to fail fast when gatewayEndpoint is not set.
func (k *Katalog) NeedsGateway() bool {
	return k.NeedsCertificates() || (k.HasNotification() && !k.IsNotificationStandalone())
}

// ── Certificates ──────────────────────────────────────────────────────────────

// NeedsCertificates reports whether Orkestra must generate TLS certificates.
//
// Certificates are required when deletion protection, admission webhooks,
// or conversion webhooks are enabled with valid usecases
// configured in at least 1 CRD— all three use the same TLS cert.
func (k *Katalog) NeedsCertificates() bool {
	return k.IsDeletionProtectionEnabled() ||
		k.IsNamespaceProtectionEnabled() ||
		k.HasValidationOrMutationRules() || // Valid admission rule
		k.HasConversionPaths()
}

// IsHousekeeperEnabled reports whether the webhook housekeeper is enabled.
func (k *Katalog) IsHousekeeperEnabled() bool {
	return k.securityEnvDefaults().HousekeeperEnabled()
}

// HousekeeperSyncInterval returns the housekeeper sync interval.
func (k *Katalog) HousekeeperSyncInterval() time.Duration {
	return k.securityEnvDefaults().HousekeeperSyncInterval()
}

// ── Certificate manager ───────────────────────────────────────────────────────

// CertAutoRotate reports whether pre-emptive TLS certificate rotation is enabled.
//
// Precedence:
//
//	YAML security.certManager.autoRotate declared → use YAML value
//	YAML absent                                   → fall back to TLS_AUTO_ROTATE env (default: true)
func (k *Katalog) CertAutoRotate() bool {
	if k.Security.CertManager != nil {
		return k.Security.IsCertAutoRotateEnabled()
	}
	return k.securityEnvDefaults().CertAutoRotate()
}

// CertRotationThreshold returns the pre-rotation window as a parsed duration.
// Returns 30 days when the configured value cannot be parsed.
//
// Precedence:
//
//	YAML security.certManager.rotationThreshold non-empty → use YAML value
//	YAML absent or empty                                  → fall back to TLS_ROTATION_THRESHOLD env (default: "30d")
func (k *Katalog) CertRotationThreshold() time.Duration {
	raw := k.Security.CertRotationThresholdVal(k.securityEnvDefaults().CertRotationThresholdStr())
	if d, err := parseTimeDuration(raw); err == nil {
		return d
	}
	return 30 * 24 * time.Hour
}

// CertValidFor returns the certificate validity duration as a parsed duration.
// Returns 1 year when the configured value cannot be parsed.
//
// Precedence:
//
//	YAML security.certManager.rotateAfter non-empty → use YAML value
//	YAML absent or empty                                  → fall back to TLS_ROTATE_AFTER env (default: "1y")
func (k *Katalog) CertValidFor() time.Duration {
	raw := k.Security.ValidForVal(k.securityEnvDefaults().CertValidForStr())
	if d, err := parseTimeDuration(raw); err == nil {
		return d
	}
	return 365 * 24 * time.Hour
}

// CertValidForStr returns the raw validity string for use in CertificateSpec.ValidFor.
// Falls back to "1y" when not configured.
func (k *Katalog) CertValidForStr() string {
	return k.Security.ValidForVal(k.securityEnvDefaults().CertValidForStr())
}

// GatewayTokenNames returns the names of all tokens declared in
// gateway.api.auth.tokens. Returns nil when the gateway is not enabled.
func (k *Katalog) GatewayTokenNames() []string {
	if !k.IsGatewayEnabled() {
		return nil
	}
	if !k.Gateway.HasAPI() || k.Gateway.API.Auth.Empty() {
		return nil
	}
	names := make([]string, 0, len(k.Gateway.API.Auth.Tokens))
	for _, t := range k.Gateway.API.Auth.Tokens {
		names = append(names, t.Name)
	}
	return names
}
