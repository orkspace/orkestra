package validate

import (
	"fmt"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateGatewayTokens is the dispatcher for gateway.api.auth.tokens validation.
func (e *executor) validateGatewayTokens() error {
	if !e.k.IsGatewayEnabled() || !e.k.Gateway.HasAPI() || e.k.Gateway.API.Auth.Empty() {
		return nil
	}
	if err := e.validateGatewayTokensCommon(); err != nil {
		return err
	}
	if err := e.validateGatewayStaticTokens(); err != nil {
		return err
	}
	return e.validateGatewayOIDCTokens()
}

// gatewayAPIAuthTokens returns the token entries from gateway.api.auth.tokens.
// Callers must have already confirmed the gateway and API are enabled.
func (e *executor) gatewayAPIAuthTokens() []orktypes.APIToken {
	return e.k.Gateway.API.Auth.Tokens
}

// validateGatewayTokensCommon checks for duplicate names and that each entry
// declares exactly one credential source.
func (e *executor) validateGatewayTokensCommon() error {
	seen := make(map[string]bool)
	var duplicates []string
	var errs []string

	for _, t := range e.gatewayAPIAuthTokens() {
		if seen[t.Name] {
			duplicates = append(duplicates, t.Name)
		}
		seen[t.Name] = true

		sourceCount := 0
		if strings.TrimSpace(t.Token) != "" {
			sourceCount++
		}
		if t.SecretRef != nil {
			sourceCount++
		}
		if t.IsOIDC() {
			sourceCount++
		}

		switch sourceCount {
		case 0:
			errs = append(errs, fmt.Sprintf(
				"  • gateway.api.auth.tokens[%q]: must set exactly one of: token, secretRef, githubOIDC, gitlabOIDC, vaultOIDC, oidc",
				t.Name))
		case 1:
			// ok
		default:
			errs = append(errs, fmt.Sprintf(
				"  • gateway.api.auth.tokens[%q]: only one of token, secretRef, githubOIDC, gitlabOIDC, vaultOIDC, oidc may be set",
				t.Name))
		}
	}

	if len(duplicates) > 0 {
		errs = append([]string{fmt.Sprintf(
			"  • gateway.api.auth.tokens: duplicate names: %s — each token name must be unique",
			red(strings.Join(duplicates, ", ")))}, errs...)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s gateway.api.auth.tokens validation failed:\n%s", failureMark(), strings.Join(errs, "\n"))
	}
	return nil
}
