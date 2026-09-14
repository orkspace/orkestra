package validate

import (
	"fmt"
	"strings"
)

// validateGateway fails fast when gateway-dependent features are configured but
// no gatewayEndpoint is set. It collects every active feature that needs a
// gateway so the error tells the user everything at once.
// validateGateway fails fast when both standalone mode and an explicit endpoint are set.
// A simple "enabled: true" works
func (e *executor) validateGateway() error {
	if !e.k.NeedsGateway() {
		return nil
	}

	// If enabled, return early
	if e.k.IsGatewayEnabled() {
		return nil
	}

	// Conflict: only one of standalone or explicit endpoint can be used
	if e.k.IsStandaloneGateway() && e.k.GatewayEndpoint() != "" {
		return fmt.Errorf(
			"%s orkestra: cannot enable both gateway.standalone and gateway.endpoint\n\n"+
				"Conflicting configuration:\n"+
				"  • gateway.standalone = true\n"+
				"  • gateway.endpoint = %q\n\n"+
				"Fix - set only one:\n"+
				"  • Standalone mode: set gateway.standalone = true and unset gateway.endpoint\n"+
				"  • Runtime Companion mode: set gateway.endpoint and set gateway.standalone = false (or omit it)",
			failureMark(), e.k.GatewayEndpoint(),
		)
	}

	// Standalone gateway mode – no endpoint needed
	if e.k.IsStandaloneGateway() {
		return nil
	}

	// External gateway endpoint provided – ok
	if e.k.GatewayEndpoint() != "" {
		return nil
	}

	// If we reach here, gateway is needed but no standalone or endpoint was set.
	var reasons []string
	if e.k.IsDeletionProtectionEnabled() {
		reasons = append(reasons, "  • security.deletionProtection")
	}
	if e.k.IsAdmissionEnabled() {
		reasons = append(reasons, "  • security.webhooks.admission")
	}
	if e.k.IsConversionEnabled() {
		reasons = append(reasons, "  • security.conversion")
	}
	if e.k.IsNamespaceProtectionEnabled() {
		reasons = append(reasons, "  • security.namespaceProtection")
	}
	if e.k.HasNotification() && !e.k.IsNotificationStandalone() {
		reasons = append(reasons, "  • notification")
	}

	return fmt.Errorf(
		"%s orkestra: gateway endpoint required but not configured\n\n"+
			"Enabled features that need a gateway:\n%s\n\n"+
			"Fix - set gateway.endpoint, enable gateway.standalone, or set ORK_GATEWAY_ENDPOINT.",
		failureMark(), strings.Join(reasons, "\n"),
	)
}
