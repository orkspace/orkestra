package validate

import (
	"fmt"
	"strings"
)

// validateGatewayStaticTokens checks token and secretRef entries:
//  1. token values must be ${ENV_VAR} references — literals are caught here
//     before gateway startup rejects them at runtime.
//  2. secretRef entries must supply both name and key.
func (e *executor) validateGatewayStaticTokens() error {
	var errs []string

	for _, t := range e.gatewayAPIAuthTokens() {
		if t.IsOIDC() {
			continue
		}

		if v := strings.TrimSpace(t.Token); v != "" {
			if !strings.HasPrefix(v, "${") || !strings.HasSuffix(v, "}") {
				errs = append(errs, fmt.Sprintf(
					"  • gateway.api.auth.tokens[%q]: token must be an ${ENV_VAR} reference, got literal — "+
						"set the value via extraEnv in Helm and reference it as ${MY_VAR}",
					t.Name))
			}
		}

		if t.SecretRef != nil {
			if strings.TrimSpace(t.SecretRef.Name) == "" {
				errs = append(errs, fmt.Sprintf(
					"  • gateway.api.auth.tokens[%q]: secretRef.name is required", t.Name))
			}
			if strings.TrimSpace(t.SecretRef.Key) == "" {
				errs = append(errs, fmt.Sprintf(
					"  • gateway.api.auth.tokens[%q]: secretRef.key is required", t.Name))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s gateway.api.auth.tokens validation failed:\n%s", failureMark(), strings.Join(errs, "\n"))
	}
	return nil
}

// validateGatewayOIDCTokens checks OIDC-specific rules for every OIDC token
// entry in gateway.api.auth.tokens:
//
//  1. Generic oidc entries must supply issuer.
//  2. githubOIDC and gitlabOIDC entries must declare at least one allow field —
//     an empty allow block accepts any valid token from that issuer.
func (e *executor) validateGatewayOIDCTokens() error {
	var errs []string

	for _, t := range e.gatewayAPIAuthTokens() {
		if !t.IsOIDC() {
			continue
		}

		if t.OIDC != nil && strings.TrimSpace(t.OIDC.Issuer) == "" {
			errs = append(errs, fmt.Sprintf(
				"  • gateway.api.auth.tokens[%q]: oidc.issuer is required — "+
					"use githubOIDC or gitlabOIDC for preset providers with hardcoded issuers",
				t.Name))
		}

		if t.GitHubAllowEmpty() {
			errs = append(errs, fmt.Sprintf(
				"  • gateway.api.auth.tokens[%q]: githubOIDC.allow is empty — "+
					"declare at least one field (repository, repositoryOwner, ref, workflow, environment, jobWorkflowRef) "+
					"to restrict which GitHub Actions jobs may authenticate",
				t.Name))
		}

		if t.GitLabAllowEmpty() {
			errs = append(errs, fmt.Sprintf(
				"  • gateway.api.auth.tokens[%q]: gitlabOIDC.allow is empty — "+
					"declare at least one field (namespacePath, refProtected, environment) "+
					"to restrict which GitLab CI jobs may authenticate",
				t.Name))
		}

		if t.VaultURLMissing() {
			errs = append(errs, fmt.Sprintf(
				"  • gateway.api.auth.tokens[%q]: vaultOIDC.url is required — "+
					"set it to the Vault server URL (e.g. https://vault.myorg.io)",
				t.Name))
		}

		if t.VaultAllowEmpty() {
			errs = append(errs, fmt.Sprintf(
				"  • gateway.api.auth.tokens[%q]: vaultOIDC.allow is empty — "+
					"declare at least one field (entityName, entityID, namespace) or a custom allow entry "+
					"to restrict which Vault identities may authenticate",
				t.Name))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s gateway OIDC token validation failed:\n%s", failureMark(), strings.Join(errs, "\n"))
	}
	return nil
}
