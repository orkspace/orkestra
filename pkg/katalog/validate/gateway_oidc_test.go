package validate

import (
	"strings"
	"testing"

	"github.com/orkspace/orkestra/pkg/katalog"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

func katalogWithOIDCToken(t orktypes.APIToken) *executor {
	return newExec(&katalog.Katalog{
		Gateway: &orktypes.GatewayConfig{
			Enabled: true,
			API: &orktypes.GatewayAPIConfig{
				Enabled: true,
				Auth:    orktypes.APIAuth{Tokens: []orktypes.APIToken{t}},
			},
		},
	})
}

func TestValidateGatewayOIDC_GenericRequiresIssuer(t *testing.T) {
	k := katalogWithOIDCToken(orktypes.APIToken{
		Name: "ci",
		OIDC: &orktypes.OIDCToken{
			// Issuer intentionally omitted
			Allow: map[string]string{"sub": "repo:myorg/app"},
		},
	})
	if err := k.validateGatewayOIDCTokens(); err == nil {
		t.Error("expected error: oidc.issuer required")
	} else if !strings.Contains(err.Error(), "oidc.issuer") {
		t.Errorf("error should mention oidc.issuer, got: %v", err)
	}
}

func TestValidateGatewayOIDC_GenericWithIssuer_OK(t *testing.T) {
	k := katalogWithOIDCToken(orktypes.APIToken{
		Name: "ci",
		OIDC: &orktypes.OIDCToken{
			Issuer: "https://token.example.com",
			Allow:  map[string]string{"sub": "repo:myorg/app"},
		},
	})
	if err := k.validateGatewayOIDCTokens(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateGatewayOIDC_GitHub_OK(t *testing.T) {
	k := katalogWithOIDCToken(orktypes.APIToken{
		Name:       "gh-ci",
		GitHubOIDC: &orktypes.GitHubOIDC{Allow: orktypes.GitHubOIDCClaims{Repository: "myorg/app"}},
	})
	if err := k.validateGatewayOIDCTokens(); err != nil {
		t.Errorf("unexpected error for githubOIDC: %v", err)
	}
}

func TestValidateGatewayOIDC_GitLab_OK(t *testing.T) {
	k := katalogWithOIDCToken(orktypes.APIToken{
		Name:       "gl-ci",
		GitLabOIDC: &orktypes.GitLabOIDC{Allow: orktypes.GitLabOIDCClaims{NamespacePath: "mygroup/app"}},
	})
	if err := k.validateGatewayOIDCTokens(); err != nil {
		t.Errorf("unexpected error for gitlabOIDC: %v", err)
	}
}

func TestValidateGatewayOIDC_GitHub_EmptyAllow(t *testing.T) {
	k := katalogWithOIDCToken(orktypes.APIToken{
		Name:       "gh-ci",
		GitHubOIDC: &orktypes.GitHubOIDC{}, // no allow fields
	})
	if err := k.validateGatewayOIDCTokens(); err == nil {
		t.Error("expected error: githubOIDC.allow is empty")
	} else if !strings.Contains(err.Error(), "githubOIDC.allow is empty") {
		t.Errorf("error should mention githubOIDC.allow is empty, got: %v", err)
	}
}

func TestValidateGatewayOIDC_GitLab_EmptyAllow(t *testing.T) {
	k := katalogWithOIDCToken(orktypes.APIToken{
		Name:       "gl-ci",
		GitLabOIDC: &orktypes.GitLabOIDC{}, // no allow fields
	})
	if err := k.validateGatewayOIDCTokens(); err == nil {
		t.Error("expected error: gitlabOIDC.allow is empty")
	} else if !strings.Contains(err.Error(), "gitlabOIDC.allow is empty") {
		t.Errorf("error should mention gitlabOIDC.allow is empty, got: %v", err)
	}
}

func TestValidateGatewayOIDC_Vault_OK(t *testing.T) {
	k := katalogWithOIDCToken(orktypes.APIToken{
		Name: "vault-ci",
		VaultOIDC: &orktypes.VaultOIDC{
			URL:   "https://vault.myorg.io",
			Allow: orktypes.VaultOIDCClaims{EntityName: "ci-agent"},
		},
	})
	if err := k.validateGatewayOIDCTokens(); err != nil {
		t.Errorf("unexpected error for vaultOIDC: %v", err)
	}
}

func TestValidateGatewayOIDC_Vault_MissingURL(t *testing.T) {
	k := katalogWithOIDCToken(orktypes.APIToken{
		Name: "vault-ci",
		VaultOIDC: &orktypes.VaultOIDC{
			// URL intentionally omitted
			Allow: orktypes.VaultOIDCClaims{EntityName: "ci-agent"},
		},
	})
	if err := k.validateGatewayOIDCTokens(); err == nil {
		t.Error("expected error: vaultOIDC.url is required")
	} else if !strings.Contains(err.Error(), "vaultOIDC.url") {
		t.Errorf("error should mention vaultOIDC.url, got: %v", err)
	}
}

func TestValidateGatewayOIDC_Vault_EmptyAllow(t *testing.T) {
	k := katalogWithOIDCToken(orktypes.APIToken{
		Name:      "vault-ci",
		VaultOIDC: &orktypes.VaultOIDC{URL: "https://vault.myorg.io"}, // no allow fields
	})
	if err := k.validateGatewayOIDCTokens(); err == nil {
		t.Error("expected error: vaultOIDC.allow is empty")
	} else if !strings.Contains(err.Error(), "vaultOIDC.allow is empty") {
		t.Errorf("error should mention vaultOIDC.allow is empty, got: %v", err)
	}
}

func TestValidateGatewayTokens_SourceExclusivity(t *testing.T) {
	// Both token and secretRef set — should fail.
	k := katalogWithOIDCToken(orktypes.APIToken{
		Name:      "bad",
		Token:     "${MY_TOKEN}",
		SecretRef: &orktypes.APISecretRef{Name: "s", Key: "k"},
	})
	if err := k.validateGatewayTokens(); err == nil {
		t.Error("expected error: only one source may be set")
	}
}

func TestValidateGatewayTokens_NoSource(t *testing.T) {
	k := katalogWithOIDCToken(orktypes.APIToken{Name: "empty"})
	if err := k.validateGatewayTokens(); err == nil {
		t.Error("expected error: must set exactly one source")
	}
}
