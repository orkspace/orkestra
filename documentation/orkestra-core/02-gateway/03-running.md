# Running the Gateway

The Gateway has two modes: local and in-cluster. Both execute the same intent chain logic. Neither requires the other to be running.

---

## Local mode

Local mode runs the full Gateway chain in process — no cluster connection, no TLS, no webhook server. It is designed for the development loop: write a Katalog, test intents and admission rules immediately, before deploying anything.

### ork serve play

`ork serve play` runs the complete six-stage intent chain from a flat intent file and prints each stage:

```bash
ork serve play -f katalog.yaml --token platform-team --intent intent.yaml
```

```text
→  stage 1 · Target resolution
   ✓ kind=Platform  target=platform  alias=(none)

→  stage 2 · Token check
   ✓ token platform-team can create on Platform

→  stage 3 · CR construction
   ✓ name=my-platform  namespace=default

→  stage 4 · Provenance annotations
      orkestra.orkspace.io/serve-alias: (none)
      orkestra.orkspace.io/serve-target: platform
   ✓ provenance stamped — full CR:

→  stage 5 · Admission validation
   ✓ passed — no violations

→  stage 6 · Response payload
   ✓ payload evaluated
```

No CR is applied. No cluster is touched. The output shows exactly what the Gateway would do.

To hand the built CR directly into `ork simulate` after play completes:

```bash
ork serve play -f katalog.yaml --token platform-team --intent intent.yaml --simulate
```

To use a simulate spec for the handoff — with full assert mode:

```bash
ork serve play -f katalog.yaml --token platform-team --intent intent.yaml \
  --simulate simulate.yaml
```

`--simulate` is only valid for `create` and `update` operations. The built CR substitutes the `cr:` field in the simulate spec; the `expect:` block is evaluated normally.

### ork gate run

`ork gate run` starts the Gateway locally in HTTP-only mode. The full Serve API is live — `POST /api/v1/apply`, `GET /api/v1/resources/` — but TLS and admission webhooks are disabled (those require a live cluster). Use it to test serve routing, apply flows, and intake payloads without a Helm deployment.

```bash
ork gate run -f katalog.yaml
```

The gateway listens on `:8080` by default. Use `ORK_PORT` to change it:

```bash
ORK_PORT=8085 ork gate run -f katalog.yaml
```

Admission and conversion webhooks are not registered — they require TLS and a running cluster.

### ork gate

`ork gate` evaluates admission rules only — validation and mutation — against a CR file. It does not run the full intent chain. Use it when you have a CR already and want to check what the admission webhook would do:

```bash
ork gate -f katalog.yaml --cr cr.yaml
```

```text
◆  Platform  (platform)
  ✓ 4/4 validation rules passed
  note: no mutations apply
```

With a CR that violates a rule:

```bash
ork gate -f katalog.yaml --cr cr-bad.yaml
```

```text
◆  Platform  (platform)
  ✗ spec.image  images must come from the internal registry (registry.internal/)

  ✗ 3/4 validation rules passed · ✗ 1 denial

admission denied
```

Limitations in local mode (both commands):

| Rule type | Local behaviour |
|-----------|----------------|
| `operator: unique` | Skipped — no live informer cache |
| `external: <endpoint>` | Skipped — no real endpoint to call |

Both limitations are noted in the output. Rules that cannot be evaluated locally pass silently, and the notes tell you which ones to verify against a real cluster.

---

## Live delivery

`ork serve apply` sends the same intent file to a real gateway. It is the live counterpart to `ork serve play` — same input format, same detection logic, different execution path.

```bash
ork serve apply -f intent.yaml --api https://gateway.myorg.io --token "$ORK_TOKEN"
```

The gateway runs the full pipeline (target resolution → token check → CR construction → provenance stamping → admission → SSA) and returns a structured response:

```text
  serve apply  intent.yaml

  ✓ PlatformResource  team-payments/my-service
  poll: https://gateway.myorg.io/api/v1/resources/platformresource/team-payments/my-service

accepted
```

`--dry-run` runs the full admission pipeline without writing the CR to etcd — useful for validating a token's permissions or an intent's shape against a live gateway before committing.

Use `--dry-run` to validate against the live gateway without applying — token check, field validation, admission rules run, but nothing is written to etcd:

```bash
ork serve apply -f intent.yaml --api https://gateway.myorg.io --token "$ORK_TOKEN" --dry-run
```

→ [Live Delivery concept](../../concepts/self-service/07-live-delivery.md) — intent file format, full CR mode, GitOps pattern, rollback

---

## In-cluster mode

In-cluster, the Gateway runs as a standard Kubernetes Deployment. It serves:

- The Serve API (`/api/v1/apply`, `/api/v1/exclude`, `/api/v1/get`, `/api/v1/list`, `/api/v1/delete`) over HTTP
- TLS webhook endpoints (`/validate`, `/mutate`, `/convert`) registered with `ValidatingWebhookConfiguration` and `MutatingWebhookConfiguration`
- The notification endpoint (`/notify`) called by the Runtime on event dispatch

### High availability

Run two replicas for HA. The Gateway is stateless — no leader election, no shared state. Kubernetes load-balances webhook calls and serve requests across all replicas automatically.

```yaml
spec:
  replicas: 2
```

### Standalone deployment

The Gateway can run without any Runtime. A standalone Gateway gives you deletion protection, admission webhooks, namespace protection, and auto-managed TLS on any cluster — even one with no Orkestra-managed CRDs.

This is useful for platform teams that want the security layer deployed before any operators are running.

### Configuration

The Gateway is configured from the same Katalog as the Runtime. `ork generate bundle --for gateway` produces all the Kubernetes manifests: Deployment, Service, ServiceAccount, ClusterRole, ClusterRoleBinding, webhook configurations, and certificate Secret.

```bash
ork generate bundle --for gateway -f katalog.yaml
```

The certificate rotation interval, webhook failure policy, and endpoint configuration are all declared in the Katalog under `security.webhooks`.
