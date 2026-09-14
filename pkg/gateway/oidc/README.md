# pkg/gateway/oidc

`oidc` provides JWKS caching and JWT verification for the Gateway API's OIDC token auth mode. It is the only place in the codebase that fetches external signing keys and verifies bearer tokens against them.

```go
cache := oidc.NewCache(oidc.DefaultTTL)
claims, err := cache.Verify(issuerURL, bearerToken, audience)
```

`claims` is a flat `map[string]string` of every scalar claim in the verified JWT. The caller matches these against the `allow` block declared in the Katalog token entry — `APIToken.MatchesOIDCClaims(claims)`.

## Three verification paths

The gateway supports three OIDC configurations. All three produce the same `map[string]string` from `Verify` — the only difference is how the JWKS URL is resolved.

| Katalog field | JWKS resolution | Configurable |
|--------------|----------------|-------------|
| `githubOIDC` | Fixed: `https://token.actions.githubusercontent.com/.well-known/jwks` | No — GitHub's JWKS is stable |
| `gitlabOIDC` | Discovery: `https://gitlab.com/.well-known/openid-configuration` → `jwks_uri` | No |
| `oidc` | Discovery: `{issuer}/.well-known/openid-configuration` → `jwks_uri` | Yes — caller sets `issuer` |

GitHub skips discovery entirely. Every other issuer goes through the standard OIDC discovery flow.

## What `Verify` checks

In order:

1. Fetches (or returns cached) JWKS for the issuer
2. Parses the JWT — extracts `kid` from the header
3. Looks up the signing key by `kid` in the JWKS
4. Verifies the cryptographic signature
5. Checks `exp` — rejects expired tokens
6. Checks `iss` — must equal `issuerURL` exactly
7. Checks `aud` — must contain `audience` (skipped when `audience` is Empty()
8. Returns all scalar claims as `map[string]string`

A failure at any step returns a non-nil error. The gateway maps OIDC errors to the same 401 response as a static token mismatch.

## JWKS caching

Keys are cached per issuer with a configurable TTL (default: 1 hour). GitHub rotates its keys infrequently; most providers rotate on a similar cadence. Cached keys are reused across requests until the TTL expires, at which point the next request triggers a background re-fetch.

The cache is safe for concurrent use — reads use a shared lock, fetches promote to an exclusive lock only after the network call completes.

## Developer documentation

| I want to… | Go to |
|-----------|-------|
| Understand JWKS fetching, caching, and discovery | [docs/01-cache.md](docs/01-cache.md) |
| Understand JWT verification step by step | [docs/02-verify.md](docs/02-verify.md) |
| Understand how this wires into stage 2 of the gateway | [../api/docs/05-auth.md](../api/docs/05-auth.md) |
| Understand the Katalog token entry types | [`pkg/types/types_oidc_token.go`](../../types/types_oidc_token.go) |
