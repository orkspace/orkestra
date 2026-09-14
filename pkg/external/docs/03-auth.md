# 03 — Auth Resolution

`auth:` is optional on any external call. When declared, it resolves a credential and threads it to the protocol client. The client decides what to do with it — HTTP injects it as a header, Redis uses it as the password, Kafka uses it for SASL, Postgres and Mongo use it as a URI override.

## Sources

Exactly one source must be set. Both set together is a validation error.

### `secretRef`

Reads a single key from a Kubernetes Secret:

```yaml
auth:
  secretRef:
    name: my-secret
    namespace: my-namespace
    key: token
```

All three fields are required. `namespace` is never defaulted — the operator service account needs `get` on Secrets in every namespace where credentials are stored. `ork validate` rejects a missing `namespace`.

The Secret is read via the Kubernetes API on every call. It is not cached — if the Secret is rotated, the new credential is picked up on the next reconcile.

The runtime service account needs `secrets get` in the target namespace. This RBAC rule is generated automatically when any CRD in the Katalog declares an `external:` block with `auth.secretRef`. See `pkg/katalog/generate_rbac.go:HasExternalSecretRefs`.

### `env`

Reads a credential from an environment variable at call time:

```yaml
auth:
  env: "$MY_API_TOKEN"
```

`ExpandEnv` is called — `$VAR` and `${VAR}` syntax both work. An empty expansion (variable not set or Empty() is treated as an error, not a missing credential.

Use `secretRef` in production. `env` is intended for local development and CI where injecting secrets as environment variables is simpler than managing Kubernetes Secrets.

## HTTP header injection

For HTTP calls, the credential is injected as an HTTP header. The default header is `Authorization` and the value is `Bearer <credential>`.

Override the header name with `header:`:

```yaml
auth:
  env: "$API_KEY"
  header: "X-Api-Key"
```

When `header: "X-Api-Key"` is set, the value is injected as-is without the `Bearer ` prefix — `X-Api-Key: <credential>`.

`header:` is ignored for non-HTTP protocols. The credential string is passed directly to the client.

## Credential threading

`resolveAuth()` in `auth.go` returns `(credential, header string, err error)`. The resolved credential is passed to `ProtocolClient.Fetch()` as a plain string. The client is responsible for using it correctly:

| Protocol | How credential is used |
|----------|----------------------|
| HTTP | `Authorization: Bearer <credential>` (or `<header>: <credential>`) |
| Redis | Set as `opts.Password` via `go-redis` |
| Postgres | Replaces the URL entirely when set (full DSN in Secret) |
| Mongo | Replaces the URL entirely when set (full URI in Secret) |
| Kafka | Split on first `:` → SASL/PLAIN `{Username, Password}` |

---

**Next →** [04 — The Result Cache](04-cache.md)
