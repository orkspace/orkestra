package types

import "strings"

const (
	githubOIDCIssuer = "https://token.actions.githubusercontent.com"
	gitlabOIDCIssuer = "https://gitlab.com"
)

// GitHubOIDC configures authentication via GitHub Actions OIDC tokens.
// The issuer and JWKS URL are fixed — only the allow block is configurable.
type GitHubOIDC struct {
	Allow GitHubOIDCClaims `yaml:"allow" json:"allow"`
}

// GitHubOIDCClaims declares which GitHub-issued claims must match.
// All declared (non-Empty() fields must match — unset fields are not checked.
type GitHubOIDCClaims struct {
	Repository      string `yaml:"repository,omitempty" json:"repository,omitempty"`
	RepositoryOwner string `yaml:"repositoryOwner,omitempty" json:"repositoryOwner,omitempty"`
	Ref             string `yaml:"ref,omitempty" json:"ref,omitempty"`
	Workflow        string `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	Environment     string `yaml:"environment,omitempty" json:"environment,omitempty"`
	JobWorkflowRef  string `yaml:"jobWorkflowRef,omitempty" json:"jobWorkflowRef,omitempty"`
}

func (g *GitHubOIDC) asMap() map[string]string {
	c := g.Allow
	m := make(map[string]string, 6)
	if c.Repository != "" {
		m["repository"] = c.Repository
	}
	if c.RepositoryOwner != "" {
		m["repository_owner"] = c.RepositoryOwner
	}
	if c.Ref != "" {
		m["ref"] = c.Ref
	}
	if c.Workflow != "" {
		m["workflow"] = c.Workflow
	}
	if c.Environment != "" {
		m["environment"] = c.Environment
	}
	if c.JobWorkflowRef != "" {
		m["job_workflow_ref"] = c.JobWorkflowRef
	}
	return m
}

// GitLabOIDC configures authentication via GitLab CI OIDC tokens.
type GitLabOIDC struct {
	Allow GitLabOIDCClaims `yaml:"allow" json:"allow"`
}

// GitLabOIDCClaims declares which GitLab-issued claims must match.
type GitLabOIDCClaims struct {
	NamespacePath string `yaml:"namespacePath,omitempty" json:"namespacePath,omitempty"`
	RefProtected  string `yaml:"refProtected,omitempty" json:"refProtected,omitempty"`
	Environment   string `yaml:"environment,omitempty" json:"environment,omitempty"`
}

func (g *GitLabOIDC) asMap() map[string]string {
	c := g.Allow
	m := make(map[string]string, 3)
	if c.NamespacePath != "" {
		m["namespace_path"] = c.NamespacePath
	}
	if c.RefProtected != "" {
		m["ref_protected"] = c.RefProtected
	}
	if c.Environment != "" {
		m["environment"] = c.Environment
	}
	return m
}

// VaultOIDC configures authentication via HashiCorp Vault OIDC tokens.
// The caller authenticates to Vault first (via any Vault auth method), then
// presents a short-lived OIDC token issued by Vault's identity provider.
// Vault's OIDC issuer is {url}/v1/identity/oidc — both the iss claim and the
// discovery document (.well-known/openid-configuration) live under that path.
type VaultOIDC struct {
	// URL is the Vault server URL (e.g. https://vault.myorg.io). Required.
	URL string `yaml:"url" json:"url"`

	// Namespace is the Vault namespace to scope the issuer to (Vault Enterprise only).
	// When set, the effective issuer is {url}/v1/{namespace}.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Audience is the expected aud claim value in the Vault-issued token.
	// When empty, the audience check is skipped.
	Audience string `yaml:"audience,omitempty" json:"audience,omitempty"`

	// Allow declares which Vault identity claims must match.
	// All declared (non-Empty() fields must match — unset fields are not checked.
	Allow VaultOIDCClaims `yaml:"allow" json:"allow"`
}

// VaultOIDCClaims declares which Vault-issued claims must match.
// Vault's OIDC token includes entity and group identity from the Vault identity store.
type VaultOIDCClaims struct {
	// EntityName is the Vault entity name (identity/entity name).
	EntityName string `yaml:"entityName,omitempty" json:"entityName,omitempty"`

	// EntityID is the Vault entity ID (stable UUID for the entity).
	EntityID string `yaml:"entityID,omitempty" json:"entityID,omitempty"`

	// Namespace is the Vault namespace the entity belongs to (Enterprise only).
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Allow is a free-form map of additional claim name → required value.
	// Use for any claims configured via Vault's OIDC provider template.
	Allow map[string]string `yaml:"allow,omitempty" json:"allow,omitempty"`
}

func (v *VaultOIDC) asMap() map[string]string {
	c := v.Allow
	m := make(map[string]string, 3+len(c.Allow))
	if c.EntityName != "" {
		m["entity_name"] = c.EntityName
	}
	if c.EntityID != "" {
		m["entity_id"] = c.EntityID
	}
	if c.Namespace != "" {
		m["namespace"] = c.Namespace
	}
	for k, val := range c.Allow {
		m[k] = val
	}
	return m
}

// OIDCToken configures authentication via any OIDC-compliant identity provider.
// The gateway discovers the JWKS URI via {issuer}/.well-known/openid-configuration.
type OIDCToken struct {
	// Issuer is the OIDC provider's issuer URL. Required.
	Issuer string `yaml:"issuer" json:"issuer"`

	// Audience is the expected `aud` claim value.
	// When empty, the audience check is skipped.
	Audience string `yaml:"audience,omitempty" json:"audience,omitempty"`

	// Allow is a free-form map of claim name → required value.
	// All declared entries must match — unset keys are not checked.
	Allow map[string]string `yaml:"allow,omitempty" json:"allow,omitempty"`
}

// ── APIToken methods ───────────────────────────────────────────────────────────

// IsStatic reports whether this token entry uses a static bearer value
// (env var reference or Kubernetes Secret).
func (t *APIToken) IsStatic() bool {
	return t.Token != "" || t.SecretRef != nil
}

// IsOIDC reports whether this token entry uses OIDC authentication.
func (t *APIToken) IsOIDC() bool {
	return t.GitHubOIDC != nil || t.GitLabOIDC != nil || t.VaultOIDC != nil || t.OIDC != nil
}

// OIDCIssuer returns the issuer URL for this token entry.
// Returns empty string for static tokens.
func (t *APIToken) OIDCIssuer() string {
	switch {
	case t.GitHubOIDC != nil:
		return githubOIDCIssuer
	case t.GitLabOIDC != nil:
		return gitlabOIDCIssuer
	case t.VaultOIDC != nil:
		return t.VaultOIDC.URL + "/v1/identity/oidc"
	case t.OIDC != nil:
		return t.OIDC.Issuer
	default:
		return ""
	}
}

// OIDCDiscoveryBase returns the base URL used for OIDC discovery.
// For all providers this equals OIDCIssuer() — Vault's issuer already
// includes the /v1/identity/oidc path, so discovery base == issuer.
func (t *APIToken) OIDCDiscoveryBase() string {
	return t.OIDCIssuer()
}

// OIDCKind returns a short label identifying the OIDC provider kind.
// Used in logs and error messages.
func (t *APIToken) OIDCKind() string {
	switch {
	case t.GitHubOIDC != nil:
		return "github"
	case t.GitLabOIDC != nil:
		return "gitlab"
	case t.VaultOIDC != nil:
		return "vault"
	case t.OIDC != nil:
		return "generic"
	default:
		return ""
	}
}

// OIDCAudience returns the expected audience for this token entry.
// GitHub and GitLab tokens do not enforce a specific audience by default.
func (t *APIToken) OIDCAudience() string {
	switch {
	case t.VaultOIDC != nil:
		return t.VaultOIDC.Audience
	case t.OIDC != nil:
		return t.OIDC.Audience
	default:
		return ""
	}
}

// VaultURLMissing reports whether this entry has a vaultOIDC source with no URL set.
func (t *APIToken) VaultURLMissing() bool {
	return t.VaultOIDC != nil && strings.TrimSpace(t.VaultOIDC.URL) == ""
}

// VaultAllowEmpty reports whether this entry has a vaultOIDC source with no
// allow constraints declared — an empty allow block accepts any Vault entity token.
func (t *APIToken) VaultAllowEmpty() bool {
	if t.VaultOIDC == nil {
		return false
	}
	a := t.VaultOIDC.Allow
	return a.EntityName == "" && a.EntityID == "" && a.Namespace == "" && len(a.Allow) == 0
}

// GitHubAllowEmpty reports whether this entry has a githubOIDC source with no
// allow constraints declared — an empty allow block accepts any GitHub Actions token.
func (t *APIToken) GitHubAllowEmpty() bool {
	if t.GitHubOIDC == nil {
		return false
	}
	a := t.GitHubOIDC.Allow
	return a.Repository == "" && a.RepositoryOwner == "" && a.Ref == "" &&
		a.Workflow == "" && a.Environment == "" && a.JobWorkflowRef == ""
}

// GitLabAllowEmpty reports whether this entry has a gitlabOIDC source with no
// allow constraints declared — an empty allow block accepts any GitLab CI token.
func (t *APIToken) GitLabAllowEmpty() bool {
	if t.GitLabOIDC == nil {
		return false
	}
	a := t.GitLabOIDC.Allow
	return a.NamespacePath == "" && a.RefProtected == "" && a.Environment == ""
}

// MatchesOIDCClaims reports whether all declared allow fields match the given
// claims map (extracted from a verified JWT). Claims not declared in the allow
// block are not checked — only declared fields must match.
func (t *APIToken) MatchesOIDCClaims(claims map[string]string) bool {
	var allow map[string]string
	switch {
	case t.GitHubOIDC != nil:
		allow = t.GitHubOIDC.asMap()
	case t.GitLabOIDC != nil:
		allow = t.GitLabOIDC.asMap()
	case t.VaultOIDC != nil:
		allow = t.VaultOIDC.asMap()
	case t.OIDC != nil:
		allow = t.OIDC.Allow
	default:
		return false
	}
	for key, want := range allow {
		if got, ok := claims[key]; !ok || got != want {
			return false
		}
	}
	return true
}
