#!/usr/bin/env bash
# Generates documentation/reference/schema/04-e2e/08-complete-example.md
# from pkg/registry/e2e/fixture/e2e.yaml.
#
# Run via: make generate-e2e-example
# Wired into: make ork
#
# The fixture is the authoritative source — it exercises every kubectl:
# subcommand and is validated on every build. The doc page is generated
# from it so the two can never drift.

set -euo pipefail

FIXTURE="pkg/registry/e2e/fixture/e2e.yaml"
OUT="documentation/reference/schema/04-e2e/08-complete-example.md"

if [ ! -f "$FIXTURE" ]; then
  echo "generate-e2e-example: $FIXTURE not found" >&2
  exit 1
fi

cat > "$OUT" <<'HEADER'
# Complete E2E example

One file that exercises every `expect:` subcommand — `resources`, `commands`,
and every `kubectl:` subcommand. Use it as the canonical reference for what a
fully-featured E2E looks like.

Source: [`pkg/registry/e2e/fixture/e2e.yaml`](https://github.com/orkspace/orkestra/blob/main/pkg/registry/e2e/fixture/e2e.yaml)

---

HEADER

# Append the fixture content as a fenced YAML block, stripping leading comments.
echo '```yaml' >> "$OUT"
sed '/^#/d' "$FIXTURE" >> "$OUT"
echo '```' >> "$OUT"

cat >> "$OUT" <<'FOOTER'

---

## What each checkpoint covers

| Checkpoint | Subcommand(s) |
|---|---|
| All resources created and ready | `resources:` |
| Both probes reach Ready status | `kubectl.get` — jsonpath field extraction |
| Field assertions on Deployments and Services | `kubectl.get` — multiple entries per block |
| Pod logs show expected startup output | `kubectl.logs` — `labelSelector`, `outputContains` |
| Describe shows expected resource state | `kubectl.describe` — kind + name |
| Exec into nginx pod (has sh) | `kubectl.exec` — `labelSelector`, inline command |
| Port-forward to devserver health endpoints | `kubectl.port-forward` — multiple paths per block |
| Apply a ConfigMap inline and assert it landed | `kubectl.apply` — inline manifest + follow-up `kubectl.get` |
| Patch server probe and assert the CR field updated | `kubectl.patch` — merge patch on spec field |
| Arbitrary commands still work alongside the DSL | `commands:` — raw shell alongside `kubectl:` |
| Cleanup verified | `resources:` with `count: 0` |

Two CRs run in parallel from `cr.yaml` (multi-document):

| CR | Image | Port | Purpose |
|---|---|---|---|
| `my-probe-server` | `ghcr.io/orkspace/orkestra-dev-server:latest` | 9999 | Port-forward and JSON endpoint assertions |
| `my-probe-exec` | `nginx:alpine` | 80 | Exec assertions — nginx has `sh`, the devserver is distroless |

See [`pkg/registry/e2e/fixture/README.md`](https://github.com/orkspace/orkestra/blob/main/pkg/registry/e2e/fixture/README.md) for
instructions on running this fixture and the rule for adding new subcommands.

---

→ Back: [07-kubectl.md](07-kubectl.md) | [Schema index](index.md)
FOOTER

echo "generate-e2e-example: wrote $OUT"