// pkg/provider/redis/provider.go
//
// Redis provider for Orkestra.
//
// Handles the "cache" block in Katalog declarations.
// Uses go-redis/v9 — the standard Redis client for Go.
//
// Supported resource kinds:
//
//	acluser — Redis ACL user (create, update password and rules, delete)
//	config  — Redis server configuration (SET and persist across restarts via CONFIG SET)
//
// Installation:
//
//	go get github.com/redis/go-redis/v9
//
// Registration:
//
//	p, err := cacheprovider.NewFromAuth(ctx, auth)
//	registry.Register(p)
//
// Auth keys (providers[].auth block):
//
//	addr     — server address as host:port (default: localhost:6379)
//	password — server password / default user password (default: Empty()
//	db       — logical database index (default: 0)
//	tls      — "true" to enable TLS (default: false)
//
// Katalog:
//
//	providers:
//	  - name: cache
//	    required: true
//	    auth:
//	      addr: "$REDIS_ADDR"
//	      password: "$REDIS_PASSWORD"
//
//	operatorBox:
//	  providers:
//	    cache:
//	      - acluser:
//	          name: "{{ .spec.cacheUser }}"
//	          password: "{{ .spec.cachePassword }}"
//	          rules: "~* &* +@all"
//	          credentials:
//	            secretName: "{{ .metadata.name }}-cache-creds"   # REDIS_PASSWORD key
//
//	      - config:
//	          key: maxmemory-policy
//	          value: allkeys-lru
package redisprovider

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/redis/go-redis/v9"
)

// ─────────────────────────────────────────────────────────────────────────────
// Provider
// ─────────────────────────────────────────────────────────────────────────────

// Provider implements orktypes.Provider for the "cache" block.
type Provider struct {
	client *redis.Client
}

// New creates a Redis cache provider from an existing client.
func New(client *redis.Client) *Provider {
	return &Provider{client: client}
}

// NewFromAddr creates a Redis provider from an address and optional password.
func NewFromAddr(ctx context.Context, addr, password string, db int) (*Provider, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("cache: ping failed: %w", err)
	}
	return New(client), nil
}

// NewFromAuth creates a Redis provider from a Katalog auth map.
// Keys: addr, password, db, tls.
func NewFromAuth(ctx context.Context, auth map[string]string) (*Provider, error) {
	addr := auth["addr"]
	if addr == "" {
		addr = "localhost:6379"
	}
	password := auth["password"]

	db := 0
	if s := auth["db"]; s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			db = n
		}
	}

	opts := &redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	}
	if auth["tls"] == "true" {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("cache: ping to %q failed: %w", addr, err)
	}
	return New(client), nil
}

func (p *Provider) Name() string { return "redis" }

// ─────────────────────────────────────────────────────────────────────────────
// Reconcile
// ─────────────────────────────────────────────────────────────────────────────

func (p *Provider) Reconcile(ctx context.Context, req orktypes.ReconcileRequest) error {
	for _, decl := range req.Declarations {
		var err error
		switch decl.Kind {
		case "acluser":
			err = p.reconcileACLUser(ctx, req, decl)
		case "config":
			err = p.reconcileConfig(ctx, req, decl)
		default:
			req.Logger.Warn().
				Str("kind", decl.Kind).
				Msg("cache: unknown resource kind — skipped")
			continue
		}
		if err != nil {
			return fmt.Errorf("cache.%s: %w", decl.Kind, err)
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Delete
// ─────────────────────────────────────────────────────────────────────────────

func (p *Provider) Delete(ctx context.Context, req orktypes.DeleteRequest) error {
	// Delete in reverse order: config → acluser
	for i := len(req.Declarations) - 1; i >= 0; i-- {
		decl := req.Declarations[i]
		var err error
		switch decl.Kind {
		case "acluser":
			err = p.deleteACLUser(ctx, req, decl)
		case "config":
			// Config keys are not deleted — server configuration is not
			// owned by a single CR. Log and skip.
			req.Logger.Debug().
				Str("key", decl.Field("key", "")).
				Msg("cache: config keys are not deleted on CR deletion — skipping")
			continue
		}
		if err != nil {
			return fmt.Errorf("cache.%s delete: %w", decl.Kind, err)
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ACL User
// ─────────────────────────────────────────────────────────────────────────────

func (p *Provider) reconcileACLUser(ctx context.Context, req orktypes.ReconcileRequest, decl orktypes.ProviderDeclaration) error {
	name, err := decl.Require("name")
	if err != nil {
		return err
	}

	// Resolve password from Secret if specified
	password := decl.Field("password", "")
	if secretName := decl.Field("credentials.secretName", ""); secretName != "" {
		data, err := req.Kube.GetSecret(ctx, req.OwnerNamespace, secretName)
		if err != nil {
			return fmt.Errorf("reading credentials secret %q: %w", secretName, err)
		}
		if pw := string(data["REDIS_PASSWORD"]); pw != "" {
			password = pw
		}
	}

	// ACL rules — default allows everything
	rules := decl.Field("rules", "~* &* +@all")

	// ACL SETUSER is idempotent: creates or updates the user.
	// Build the rule set: on/off, password, key patterns, commands.
	args := []interface{}{"SETUSER", name, "on"}
	if password != "" {
		args = append(args, ">"+password)
	}
	// Append each space-separated rule as a separate argument
	for _, r := range splitRules(rules) {
		args = append(args, r)
	}

	if err := p.client.Do(ctx, args...).Err(); err != nil {
		return fmt.Errorf("setting ACL user %q: %w", name, err)
	}

	req.Logger.Info().Str("user", name).Msg("cache: ACL user reconciled")
	return nil
}

func (p *Provider) deleteACLUser(ctx context.Context, req orktypes.DeleteRequest, decl orktypes.ProviderDeclaration) error {
	name := decl.Field("name", "")
	if name == "" {
		return nil
	}

	if err := p.client.Do(ctx, "ACL", "DELUSER", name).Err(); err != nil {
		// User may not exist — treat as success
		req.Logger.Debug().Str("user", name).Err(err).Msg("cache: ACL DELUSER — treating as success")
	}

	req.Logger.Info().Str("user", name).Msg("cache: ACL user deleted")
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────────────────────────────────────

func (p *Provider) reconcileConfig(ctx context.Context, req orktypes.ReconcileRequest, decl orktypes.ProviderDeclaration) error {
	key, err := decl.Require("key")
	if err != nil {
		return err
	}
	value, err := decl.Require("value")
	if err != nil {
		return err
	}

	// CONFIG GET to check current value
	current, err := p.client.ConfigGet(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("getting config %q: %w", key, err)
	}

	if v, ok := current[key]; ok && v == value {
		req.Logger.Debug().
			Str("key", key).
			Str("value", value).
			Msg("cache: config already at desired value — no-op")
		return nil
	}

	if err := p.client.ConfigSet(ctx, key, value).Err(); err != nil {
		return fmt.Errorf("setting config %q=%q: %w", key, value, err)
	}

	req.Logger.Info().
		Str("key", key).
		Str("value", value).
		Msg("cache: config set")

	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// splitRules splits a space-separated ACL rule string into individual tokens.
func splitRules(rules string) []string {
	var out []string
	start := -1
	for i := 0; i < len(rules); i++ {
		if rules[i] == ' ' {
			if start >= 0 {
				out = append(out, rules[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, rules[start:])
	}
	return out
}
