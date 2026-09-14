package types

// GatewayWebhookConfig declares inbound intent-delivery sources for the Apply
// API — GitHub, GitLab, Slack, and generic HTTP callers that deliver
// target-mode intent (a flat field map) instead of calling
// POST /api/v1/apply directly. Every source resolves through the same
// BuildCRFromTarget path a direct API call would.
type GatewayWebhookConfig struct {
	// Include is a path (relative to the katalog file) to a YAML file with
	// its own "webhooks:" block. Expanded at load time: included entries
	// load first, inline entries override by name within each source list.
	Include string `yaml:"include,omitempty" json:"include,omitempty"`

	GitHub  []GitWebhookConfig     `yaml:"github,omitempty" json:"github,omitempty"`
	GitLab  []GitWebhookConfig     `yaml:"gitlab,omitempty" json:"gitlab,omitempty"`
	Slack   []SlackWebhookConfig   `yaml:"slack,omitempty" json:"slack,omitempty"`
	Generic []GenericWebhookConfig `yaml:"generic,omitempty" json:"generic,omitempty"`
}

// GitWebhookConfig is one push-event intake source — shared shape for
// GitHub and GitLab entries. What differs between the two is how the
// gateway verifies the request (HMAC signature vs. static token
// comparison) and which content API it calls, not the declared config.
type GitWebhookConfig struct {
	// Include, when set, replaces this entry in-place with the source's
	// own list (e.g. "github:") from the referenced file. All other fields
	// on this entry are ignored when Include is set.
	Include string `yaml:"include,omitempty" json:"include,omitempty"`

	// Name identifies this entry — for token scoping (serve.tokens),
	// logs, and include-merge-by-name. Required unless Include is set.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Enabled activates this entry's route.
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// Path is the route this entry registers on the gateway, e.g.
	// "/webhooks/github/payments". Must be unique across every webhook
	// entry (any source), the same way every Gateway API route is.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// SecretRef verifies the request came from the source — X-Hub-Signature-256
	// for GitHub, X-Gitlab-Token for GitLab. Same shape as
	// gateway.api.auth.tokens[].secretRef.
	SecretRef *APISecretRef `yaml:"secretRef,omitempty" json:"secretRef,omitempty"`

	// ContentTokenRef is a separate credential for reading file content —
	// a GitHub App installation token or a GitLab API token. SecretRef only
	// proves the webhook is genuine; it doesn't grant read access to fetch
	// what changed, since push/webhook events never carry file content.
	// Cannot be self-bootstrapped — minted externally, must already exist.
	ContentTokenRef *APISecretRef `yaml:"contentTokenRef,omitempty" json:"contentTokenRef,omitempty"`

	// Watch lists glob patterns for the file paths this entry reacts to —
	// same shape as GitHub Actions' own on.push.paths filter. A push that
	// touches none of these is ignored.
	Watch []string `yaml:"watch,omitempty" json:"watch,omitempty"`

	// Branch restricts processing to pushes on this branch. Required.
	Branch string `yaml:"branch,omitempty" json:"branch,omitempty"`

	// ReportStatus, when true, posts the apply outcome back to the source as
	// a commit status (GitHub) or pipeline status (GitLab), using
	// ContentTokenRef — the same credential that reads file content is
	// expected to carry write access for this too. Off by default: it's an
	// extra API call and a stricter credential requirement neither source
	// needs to function.
	ReportStatus bool `yaml:"reportStatus,omitempty" json:"reportStatus,omitempty"`
}

// SlackWebhookConfig is one Slack slash-command intake source — one entry
// per Slack workspace/app.
type SlackWebhookConfig struct {
	// Include, when set, replaces this entry in-place with the "slack:"
	// list from the referenced file. All other fields are ignored when set.
	Include string `yaml:"include,omitempty" json:"include,omitempty"`

	Name    string `yaml:"name,omitempty" json:"name,omitempty"`
	Enabled bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`

	// SigningSecretRef verifies the request signature Slack sends on every
	// slash command. Same shape as gateway.api.auth.tokens[].secretRef.
	SigningSecretRef *APISecretRef `yaml:"signingSecretRef,omitempty" json:"signingSecretRef,omitempty"`

	// Commands lists the slash commands this entry responds to, e.g.
	// "/deploy". A command not in this list gets an "unknown command" reply.
	Commands []string `yaml:"commands,omitempty" json:"commands,omitempty"`
}

// GenericWebhookConfig is one generic JSON-webhook intake source — for
// callers that aren't GitHub, GitLab, or Slack (PagerDuty, Datadog, an
// internal system). One entry per external system.
type GenericWebhookConfig struct {
	// Include, when set, replaces this entry in-place with the "generic:"
	// list from the referenced file. All other fields are ignored when set.
	Include string `yaml:"include,omitempty" json:"include,omitempty"`

	Name    string `yaml:"name,omitempty" json:"name,omitempty"`
	Enabled bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`

	// SecretRef verifies the request via Orkestra's own HMAC scheme.
	SecretRef *APISecretRef `yaml:"secretRef,omitempty" json:"secretRef,omitempty"`
}

// ── GatewayWebhookConfig methods ──────────────────────────────────────────

// Empty reports whether no webhook sources are declared at all.
func (w *GatewayWebhookConfig) Empty() bool {
	if w == nil {
		return true
	}
	return len(w.GitHub) == 0 && len(w.GitLab) == 0 && len(w.Slack) == 0 && len(w.Generic) == 0
}

// ── GitWebhookConfig methods ──────────────────────────────────────────

// HasSecretRef reports whether this entry has a secret configured for
// verifying the incoming request.
func (c *GitWebhookConfig) HasSecretRef() bool {
	if c == nil {
		return false
	}
	return c.SecretRef != nil
}

// HasContentTokenRef reports whether this entry has a separate credential
// configured for fetching file content.
func (c *GitWebhookConfig) HasContentTokenRef() bool {
	if c == nil {
		return false
	}
	return c.ContentTokenRef != nil
}

// ── SlackWebhookConfig methods ──────────────────────────────────────────

// HasSigningSecretRef reports whether this entry has a signing secret
// configured for verifying incoming slash commands.
func (c *SlackWebhookConfig) HasSigningSecretRef() bool {
	if c == nil {
		return false
	}
	return c.SigningSecretRef != nil
}

// ── GenericWebhookConfig methods ──────────────────────────────────────────

// HasSecretRef reports whether this entry has a secret configured for
// verifying the incoming request.
func (c *GenericWebhookConfig) HasSecretRef() bool {
	if c == nil {
		return false
	}
	return c.SecretRef != nil
}
