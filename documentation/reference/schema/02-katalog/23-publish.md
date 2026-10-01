# publish:

The `publish:` block declares the supply chain policy for a pattern. It controls
who is trusted to sign it and which quality gates must pass before the artifact
is considered complete. It is distinct from `security:`, which governs runtime
admission and namespace protection.

```yaml
publish:
  signing:
    verify: true
    expectedIdentities:
      - github.com/myorg/postgres/.github/workflows/release.yaml@refs/heads/main
      - github.com/myorg/postgres/.github/workflows/hotfix.yaml@refs/heads/main
  tests:
    e2e: true       # default: true
    simulate: true  # default: true
    intent: false   # default: false
```

---

## `publish.signing`

Controls signature requirements for this pattern.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `verify` | `bool` | `false` | When `true`, `ork pull` refuses the artifact unless a valid Cosign keyless signature is present. |
| `expectedIdentities` | `[]string` | `[]` | OIDC subject claims trusted to sign this pattern. Any one match passes. Empty means any valid signature is accepted. |

### `expectedIdentities` format

Each entry is an OIDC **subject claim** — the identity that signed the artifact,
not a public key. The format depends on which CI platform signed it:

| Platform | Subject format |
|----------|---------------|
| GitHub Actions | `github.com/<org>/<repo>/.github/workflows/<file>.yaml@refs/heads/<branch>` |
| GitLab CI | `gitlab.com/<namespace>/<project>//<job>@refs/heads/<branch>` |

The OIDC issuer is inferred automatically from the subject prefix.

```yaml
publish:
  signing:
    verify: true
    expectedIdentities:
      # only the release workflow on main is trusted
      - github.com/myorg/postgres/.github/workflows/release.yaml@refs/heads/main
```

---

## `publish.tests`

Controls which quality gates must pass at push time and are reported on the
artifact. All three default to the same behaviour as today — `intent` is opt-in.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `e2e` | `bool` | `true` | Require `e2e.yaml` to pass before push succeeds. Setting `false` is equivalent to always passing `--no-e2e`. |
| `simulate` | `bool` | `true` | Require `simulate.yaml` to pass before push succeeds. Setting `false` is equivalent to always passing `--no-simulate`. |
| `intent` | `bool` | `false` | Run `ork serve play` against `intent.yaml` and bake the result as an attestation. Requires `gateway.api.enabled: true` and `intent.yaml` present in the pattern directory. |

The CLI flags (`--no-e2e`, `--no-simulate`, `--add-intent`) are per-push overrides.
The `publish.tests` block is the standing policy declared in the katalog.

### `intent: true` requirements

Enabling `publish.tests.intent: true` requires:

- `gateway.api.enabled: true` in the same katalog
- `intent.yaml` or `intent.json` present in the pattern directory

`ork validate` will error if either condition is missing.

---

## Validation rules

`ork validate` checks the `publish:` block as part of the standard validate pass.

| Rule | Error |
|------|-------|
| `expectedIdentities` contains an empty string | `publish.signing.expectedIdentities[N]: identity must not be empty` |
| `tests.intent: true` without `gateway.api.enabled: true` | `publish.tests.intent: true requires gateway.api.enabled: true` |
| `tests.intent: true` without `intent.yaml` or `intent.json` | `publish.tests.intent: true requires intent.yaml or intent.json in the pattern directory` |

---

## Full example

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: Katalog

metadata:
  name: postgres
  description: Declarative PostgreSQL operator

publish:
  signing:
    verify: true
    expectedIdentities:
      - github.com/myorg/postgres/.github/workflows/release.yaml@refs/heads/main
  tests:
    e2e: true
    simulate: true
    intent: true

gateway:
  api:
    enabled: true

spec:
  crds:
    database:
      ...
```

---

## Where to go next

- [Artifact Signing](../../../security/10-artifact-signing.md) — how keyless signing works end to end
- [`ork pattern sign`](../../cli/12-patterns.md) — sign a pushed artifact
- [`ork pattern verify`](../../cli/12-patterns.md) — verify a signature
- [`ork push --sign`](../../cli/09-push.md) — sign at push time
