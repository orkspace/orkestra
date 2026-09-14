package types

import (
	"fmt"
	"path/filepath"
)

// ExpandGatewayWebhookIncludes resolves gateway.webhooks includes at both levels:
//
// Per-entry includes within each source list — an entry with include: set
// is replaced in-place by that source's own list from the referenced file
// (a "github:"-keyed file for a GitHub entry, and so on). The same
// mechanism ExpandExternalCalls already uses for onReconcile.external and
// friends.
//
// The block-level include (cfg.Include) — a "webhooks:"-keyed file merged
// with the inline lists. Included entries load first, inline entries
// override by name — the same rule idp.allowedTokens.include already uses.
//
// The include path is resolved relative to baseDir. Cleared after expansion.
func ExpandGatewayWebhookIncludes(gw *GatewayConfig, baseDir string) error {
	if gw == nil || gw.Webhooks == nil {
		return nil
	}
	cfg := gw.Webhooks

	var err error
	if cfg.GitHub, err = expandGitHubIncludes(cfg.GitHub, baseDir); err != nil {
		return fmt.Errorf("webhooks.github: %w", err)
	}
	if cfg.GitLab, err = expandGitLabIncludes(cfg.GitLab, baseDir); err != nil {
		return fmt.Errorf("webhooks.gitlab: %w", err)
	}
	if cfg.Slack, err = expandSlackWebhookIncludes(cfg.Slack, baseDir); err != nil {
		return fmt.Errorf("webhooks.slack: %w", err)
	}
	if cfg.Generic, err = expandGenericWebhookIncludes(cfg.Generic, baseDir); err != nil {
		return fmt.Errorf("webhooks.generic: %w", err)
	}

	if cfg.Include == "" {
		return nil
	}

	path := cfg.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading webhooks.include %q: %w", cfg.Include, err)
	}

	var f struct {
		Webhooks GatewayWebhookConfig `yaml:"webhooks"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing webhooks.include %q: %w", cfg.Include, err)
	}

	// The included document's own entries may declare per-entry includes too.
	includedDir := filepath.Dir(path)
	if f.Webhooks.GitHub, err = expandGitHubIncludes(f.Webhooks.GitHub, includedDir); err != nil {
		return fmt.Errorf("webhooks.include %q: github: %w", cfg.Include, err)
	}
	if f.Webhooks.GitLab, err = expandGitLabIncludes(f.Webhooks.GitLab, includedDir); err != nil {
		return fmt.Errorf("webhooks.include %q: gitlab: %w", cfg.Include, err)
	}
	if f.Webhooks.Slack, err = expandSlackWebhookIncludes(f.Webhooks.Slack, includedDir); err != nil {
		return fmt.Errorf("webhooks.include %q: slack: %w", cfg.Include, err)
	}
	if f.Webhooks.Generic, err = expandGenericWebhookIncludes(f.Webhooks.Generic, includedDir); err != nil {
		return fmt.Errorf("webhooks.include %q: generic: %w", cfg.Include, err)
	}

	cfg.GitHub = mergeGitWebhooksByName(f.Webhooks.GitHub, cfg.GitHub)
	cfg.GitLab = mergeGitWebhooksByName(f.Webhooks.GitLab, cfg.GitLab)
	cfg.Slack = mergeSlackWebhooksByName(f.Webhooks.Slack, cfg.Slack)
	cfg.Generic = mergeGenericWebhooksByName(f.Webhooks.Generic, cfg.Generic)
	cfg.Include = ""

	return nil
}

func expandGitHubIncludes(entries []GitWebhookConfig, baseDir string) ([]GitWebhookConfig, error) {
	var expanded []GitWebhookConfig
	for _, entry := range entries {
		if entry.Include == "" {
			expanded = append(expanded, entry)
			continue
		}
		path := entry.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("reading github include %q: %w", entry.Include, err)
		}
		var f struct {
			GitHub []GitWebhookConfig `yaml:"github"`
		}
		if err := strictUnmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing github include %q: %w", entry.Include, err)
		}
		expanded = append(expanded, f.GitHub...)
	}
	return expanded, nil
}

func expandGitLabIncludes(entries []GitWebhookConfig, baseDir string) ([]GitWebhookConfig, error) {
	var expanded []GitWebhookConfig
	for _, entry := range entries {
		if entry.Include == "" {
			expanded = append(expanded, entry)
			continue
		}
		path := entry.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("reading gitlab include %q: %w", entry.Include, err)
		}
		var f struct {
			GitLab []GitWebhookConfig `yaml:"gitlab"`
		}
		if err := strictUnmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing gitlab include %q: %w", entry.Include, err)
		}
		expanded = append(expanded, f.GitLab...)
	}
	return expanded, nil
}

func expandSlackWebhookIncludes(entries []SlackWebhookConfig, baseDir string) ([]SlackWebhookConfig, error) {
	var expanded []SlackWebhookConfig
	for _, entry := range entries {
		if entry.Include == "" {
			expanded = append(expanded, entry)
			continue
		}
		path := entry.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("reading slack include %q: %w", entry.Include, err)
		}
		var f struct {
			Slack []SlackWebhookConfig `yaml:"slack"`
		}
		if err := strictUnmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing slack include %q: %w", entry.Include, err)
		}
		expanded = append(expanded, f.Slack...)
	}
	return expanded, nil
}

func expandGenericWebhookIncludes(entries []GenericWebhookConfig, baseDir string) ([]GenericWebhookConfig, error) {
	var expanded []GenericWebhookConfig
	for _, entry := range entries {
		if entry.Include == "" {
			expanded = append(expanded, entry)
			continue
		}
		path := entry.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("reading generic include %q: %w", entry.Include, err)
		}
		var f struct {
			Generic []GenericWebhookConfig `yaml:"generic"`
		}
		if err := strictUnmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing generic include %q: %w", entry.Include, err)
		}
		expanded = append(expanded, f.Generic...)
	}
	return expanded, nil
}

// mergeGitWebhooksByName merges an included list with an inline list —
// included entries load first, inline entries override by Name.
func mergeGitWebhooksByName(included, inline []GitWebhookConfig) []GitWebhookConfig {
	order := make([]string, 0, len(included)+len(inline))
	merged := make(map[string]GitWebhookConfig, len(included)+len(inline))
	for _, e := range included {
		if _, exists := merged[e.Name]; !exists {
			order = append(order, e.Name)
		}
		merged[e.Name] = e
	}
	for _, e := range inline {
		if _, exists := merged[e.Name]; !exists {
			order = append(order, e.Name)
		}
		merged[e.Name] = e
	}
	out := make([]GitWebhookConfig, 0, len(order))
	for _, name := range order {
		out = append(out, merged[name])
	}
	return out
}

func mergeSlackWebhooksByName(included, inline []SlackWebhookConfig) []SlackWebhookConfig {
	order := make([]string, 0, len(included)+len(inline))
	merged := make(map[string]SlackWebhookConfig, len(included)+len(inline))
	for _, e := range included {
		if _, exists := merged[e.Name]; !exists {
			order = append(order, e.Name)
		}
		merged[e.Name] = e
	}
	for _, e := range inline {
		if _, exists := merged[e.Name]; !exists {
			order = append(order, e.Name)
		}
		merged[e.Name] = e
	}
	out := make([]SlackWebhookConfig, 0, len(order))
	for _, name := range order {
		out = append(out, merged[name])
	}
	return out
}

func mergeGenericWebhooksByName(included, inline []GenericWebhookConfig) []GenericWebhookConfig {
	order := make([]string, 0, len(included)+len(inline))
	merged := make(map[string]GenericWebhookConfig, len(included)+len(inline))
	for _, e := range included {
		if _, exists := merged[e.Name]; !exists {
			order = append(order, e.Name)
		}
		merged[e.Name] = e
	}
	for _, e := range inline {
		if _, exists := merged[e.Name]; !exists {
			order = append(order, e.Name)
		}
		merged[e.Name] = e
	}
	out := make([]GenericWebhookConfig, 0, len(order))
	for _, name := range order {
		out = append(out, merged[name])
	}
	return out
}
