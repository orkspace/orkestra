package validate

import (
	"fmt"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// GatewayWebhookEntryNames returns the names of every configured
// gateway.webhooks entry across all four sources — the same identity each
// entry authorizes with in serve.tokens, alongside (not instead of) names
// declared in gateway.api.auth.tokens. Returns nil when no webhooks are
// configured.
func (e *executor) GatewayWebhookEntryNames() []string {
	if !e.k.IsGatewayEnabled() || e.k.Gateway.Webhooks.Empty() {
		return nil
	}
	w := e.k.Gateway.Webhooks
	names := make([]string, 0, len(w.GitHub)+len(w.GitLab)+len(w.Slack)+len(w.Generic))
	for _, e := range w.GitHub {
		names = append(names, e.Name)
	}
	for _, e := range w.GitLab {
		names = append(names, e.Name)
	}
	for _, e := range w.Slack {
		names = append(names, e.Name)
	}
	for _, e := range w.Generic {
		names = append(names, e.Name)
	}
	return names
}

// validateGatewayWebhooks confirms:
//
//  1. gateway.webhooks requires gateway.api.enabled: true — webhook
//     entries resolve through the same BuildCRFromTarget path a direct
//     POST /api/v1/apply call does.
//  2. Every entry has a name. Include-only placeholders are already
//     expanded by this point (ExpandGatewayWebhookIncludes runs at parse
//     time) — a lingering empty name means a hand-written entry omitted it.
//  3. Entry names are unique across all four sources, not just within one —
//     a name is the lookup key LookupWebhookSource resolves and the
//     serve.tokens identity, both source-agnostic, so two entries sharing
//     a name (even across sources) would be ambiguous.
//  4. Every enabled entry has a path, and paths are unique across every
//     webhook entry, any source — they all register on the same mux.
//  5. Every enabled entry has a credential to verify the request.
//  6. GitHub/GitLab entries additionally require contentTokenRef (to fetch
//     file content) and branch.
//  7. Slack entries require at least one command.
func (e *executor) validateGatewayWebhooks() error {
	if !e.k.IsGatewayEnabled() || e.k.Gateway.Webhooks.Empty() {
		return nil
	}

	if !e.k.Gateway.HasAPI() {
		return errWebhooksRequireAPI()
	}

	seenPaths := make(map[string]string)
	seenNames := make(map[string]string)

	claimPath := func(source, name, path string) error {
		if path == "" {
			return errWebhookMissingPath(source, name)
		}
		if owner, exists := seenPaths[path]; exists {
			return errWebhookDuplicatePath(source, name, path, owner)
		}
		seenPaths[path] = fmt.Sprintf("%s[%q]", source, name)
		return nil
	}

	claimName := func(source, name string) error {
		if owner, exists := seenNames[name]; exists {
			return errWebhookDuplicateName(source, name, owner)
		}
		seenNames[name] = source
		return nil
	}

	for _, src := range []struct {
		key     string
		entries []orktypes.GitWebhookConfig
	}{
		{"github", e.k.Gateway.Webhooks.GitHub},
		{"gitlab", e.k.Gateway.Webhooks.GitLab},
	} {
		for _, e := range src.entries {
			if e.Name == "" {
				return errWebhookMissingName(src.key, e.Path)
			}
			if err := claimName(src.key, e.Name); err != nil {
				return err
			}

			if !e.Enabled {
				continue
			}
			if err := claimPath(src.key, e.Name, e.Path); err != nil {
				return err
			}
			if !e.HasSecretRef() {
				return errWebhookMissingSecretRef(src.key, e.Name)
			}
			if !e.HasContentTokenRef() {
				return errWebhookMissingContentTokenRef(src.key, e.Name)
			}
			if e.Branch == "" {
				return errWebhookMissingBranch(src.key, e.Name)
			}
		}
	}

	for _, e := range e.k.Gateway.Webhooks.Slack {
		if e.Name == "" {
			return errWebhookMissingName("slack", e.Path)
		}
		if err := claimName("slack", e.Name); err != nil {
			return err
		}

		if !e.Enabled {
			continue
		}
		if err := claimPath("slack", e.Name, e.Path); err != nil {
			return err
		}
		if !e.HasSigningSecretRef() {
			return errWebhookMissingSigningSecretRef(e.Name)
		}
		if len(e.Commands) == 0 {
			return errWebhookMissingCommands(e.Name)
		}
	}

	for _, e := range e.k.Gateway.Webhooks.Generic {
		if e.Name == "" {
			return errWebhookMissingName("generic", e.Path)
		}
		if err := claimName("generic", e.Name); err != nil {
			return err
		}

		if !e.Enabled {
			continue
		}
		if err := claimPath("generic", e.Name, e.Path); err != nil {
			return err
		}
		if !e.HasSecretRef() {
			return errWebhookMissingSecretRef("generic", e.Name)
		}
	}

	return nil
}

// ── error helpers ────────────────────────────────────────────────────────────

func errWebhooksRequireAPI() error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s gateway.webhooks declared without gateway.api.enabled: true

Webhook entries deliver intent through the same path a direct
POST /api/v1/apply call does — there is no Gateway API without it.

Enable it:
  gateway:
    api:
      enabled: true
──────────────────────────────────────────────`, failureMark())
}

func errWebhookMissingName(source, path string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s gateway.webhooks.%s: entry with no name
   path: %s

Every entry needs a name — for token scoping (serve.tokens), logs,
and include-merge-by-name.
──────────────────────────────────────────────`, failureMark(), source, path)
}

func errWebhookDuplicateName(source, name, owner string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s gateway.webhooks.%s: name %q is already used by %s

Every webhook entry needs a unique name across all four sources, not just
within its own list — the name doubles as the serve.tokens identity and
the lookup key "ork webhook play" resolves --webhook against when --source
is omitted.
──────────────────────────────────────────────`, failureMark(), source, red(name), owner)
}

func errWebhookMissingPath(source, name string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s gateway.webhooks.%s[%q]: path is required

An enabled entry needs a route to register on the gateway, e.g.
"/webhooks/%s/%s".
──────────────────────────────────────────────`, failureMark(), source, name, source, name)
}

func errWebhookDuplicatePath(source, name, path, owner string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s gateway.webhooks.%s[%q]: path %q is already registered
   registered by: %s

Every webhook entry needs a unique path — they all register on the same
gateway mux.
──────────────────────────────────────────────`, failureMark(), source, name, red(path), owner)
}

func errWebhookMissingSecretRef(source, name string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s gateway.webhooks.%s[%q]: secretRef is required

An enabled entry needs a way to verify the request came from the source
it claims to. Declare it the same way gateway.api.auth.tokens does:
  secretRef:
    name: ork-%s-%s-secret
    key: secret
──────────────────────────────────────────────`, failureMark(), source, name, source, name)
}

func errWebhookMissingContentTokenRef(source, name string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s gateway.webhooks.%s[%q]: contentTokenRef is required

Push events carry only the changed file paths, never their content.
Fetching what changed needs a separate credential — a GitHub App
installation token or a GitLab API token — distinct from secretRef,
which only proves the webhook itself is genuine.
──────────────────────────────────────────────`, failureMark(), source, name)
}

func errWebhookMissingBranch(source, name string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s gateway.webhooks.%s[%q]: branch is required

Without it, every push to every branch would be processed.
──────────────────────────────────────────────`, failureMark(), source, name)
}

func errWebhookMissingSigningSecretRef(name string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s gateway.webhooks.slack[%q]: signingSecretRef is required

An enabled entry needs a way to verify incoming slash commands actually
came from Slack.
──────────────────────────────────────────────`, failureMark(), name)
}

func errWebhookMissingCommands(name string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s gateway.webhooks.slack[%q]: at least one command is required

Without a declared command list, every slash command would be treated
as unknown.
──────────────────────────────────────────────`, failureMark(), name)
}
