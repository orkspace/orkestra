// Package intake implements gateway.webhooks — inbound intent-delivery
// sources (GitHub, GitLab, Slack, generic HTTP) that resolve through the
// exact same target-mode pipeline a direct POST /api/v1/apply call does.
//
// Each source verifies and parses its own payload into a flat field map,
// then hands off to api.ApplyTargetFields — the same BuildCRFromTarget,
// serve.tokens check, SSA, and serve.config.response evaluation a direct
// caller gets. The source is irrelevant past that point.
package intake

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/pkg/gateway/api"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ResolvedGitSource is one GitHub or GitLab intake entry with its
// credentials already resolved — ready to register a route.
type ResolvedGitSource struct {
	Config       orktypes.GitWebhookConfig
	Secret       string // verifies the incoming webhook request
	ContentToken string // reads file content via the source's own API
}

// ResolvedSlackSource is one Slack intake entry with its signing secret resolved.
type ResolvedSlackSource struct {
	Config        orktypes.SlackWebhookConfig
	SigningSecret string
}

// ResolvedGenericSource is one generic intake entry with its secret resolved.
type ResolvedGenericSource struct {
	Config orktypes.GenericWebhookConfig
	Secret string
}

// Set holds every resolved, enabled intake source, grouped by source type —
// GitHub and GitLab share a resolution path (both are GitWebhookConfig);
// Slack and generic each have their own shape.
type Set struct {
	GitHub  []ResolvedGitSource
	GitLab  []ResolvedGitSource
	Slack   []ResolvedSlackSource
	Generic []ResolvedGenericSource
}

// Load resolves every enabled webhook entry's credentials — bootstrapping
// or rotating Secrets exactly as gateway.applyAPI.auth.tokens does, via the
// same APISecretRef shape. Disabled entries are skipped entirely: no
// Secret read, no self-bootstrap, nothing registered for them later.
func Load(
	ctx context.Context,
	cfg *orktypes.GatewayWebhookConfig,
	kube kubeclient.Interface,
	ownNamespace string,
) (*Set, error) {
	if cfg.Empty() {
		return &Set{}, nil
	}

	var (
		set Set
		err error
	)

	if set.GitHub, err = loadGitSources(ctx, "github", cfg.GitHub, kube, ownNamespace); err != nil {
		return nil, err
	}
	if set.GitLab, err = loadGitSources(ctx, "gitlab", cfg.GitLab, kube, ownNamespace); err != nil {
		return nil, err
	}
	if set.Slack, err = loadSlackSources(ctx, cfg.Slack, kube, ownNamespace); err != nil {
		return nil, err
	}
	if set.Generic, err = loadGenericSources(ctx, cfg.Generic, kube, ownNamespace); err != nil {
		return nil, err
	}

	return &set, nil
}

func loadGitSources(
	ctx context.Context,
	source string,
	entries []orktypes.GitWebhookConfig,
	kube kubeclient.Interface,
	ownNamespace string,
) ([]ResolvedGitSource, error) {
	resolved := make([]ResolvedGitSource, 0, len(entries))
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		secret, err := api.ResolveSecretRef(ctx, e.SecretRef, kube, ownNamespace)
		if err != nil {
			return nil, fmt.Errorf("webhooks.%s[%q].secretRef: %w", source, e.Name, err)
		}
		contentToken, err := api.ResolveSecretRef(ctx, e.ContentTokenRef, kube, ownNamespace)
		if err != nil {
			return nil, fmt.Errorf("webhooks.%s[%q].contentTokenRef: %w", source, e.Name, err)
		}
		resolved = append(resolved, ResolvedGitSource{Config: e, Secret: secret, ContentToken: contentToken})
	}
	return resolved, nil
}

func loadSlackSources(
	ctx context.Context,
	entries []orktypes.SlackWebhookConfig,
	kube kubeclient.Interface,
	ownNamespace string,
) ([]ResolvedSlackSource, error) {
	resolved := make([]ResolvedSlackSource, 0, len(entries))
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		secret, err := api.ResolveSecretRef(ctx, e.SigningSecretRef, kube, ownNamespace)
		if err != nil {
			return nil, fmt.Errorf("webhooks.slack[%q].signingSecretRef: %w", e.Name, err)
		}
		resolved = append(resolved, ResolvedSlackSource{Config: e, SigningSecret: secret})
	}
	return resolved, nil
}

func loadGenericSources(
	ctx context.Context,
	entries []orktypes.GenericWebhookConfig,
	kube kubeclient.Interface,
	ownNamespace string,
) ([]ResolvedGenericSource, error) {
	resolved := make([]ResolvedGenericSource, 0, len(entries))
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		secret, err := api.ResolveSecretRef(ctx, e.SecretRef, kube, ownNamespace)
		if err != nil {
			return nil, fmt.Errorf("webhooks.generic[%q].secretRef: %w", e.Name, err)
		}
		resolved = append(resolved, ResolvedGenericSource{Config: e, Secret: secret})
	}
	return resolved, nil
}
