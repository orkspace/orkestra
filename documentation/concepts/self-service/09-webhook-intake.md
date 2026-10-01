# Webhook Intake

!!! warning "Stability"
    Webhook intake is implemented but not yet fully tested. Treat this feature as experimental until further notice.

`ork serve apply` is pull-based: someone runs a command with a token and sends one intent at a moment they choose. Webhook intake is push-based: the gateway sits listening, and an external event — a commit landing on a watched branch, a Slack slash command, an incident firing in PagerDuty — triggers the apply without anyone invoking the CLI.

Every source, no matter how different its wire format, ends at the exact same place the CLI does: the target-mode apply chain. The source's only job is to turn its own payload into a flat field map and prove it's genuine. Past that point there is no "webhook" concept left at all — it's the same target resolution, token check, CR construction, provenance stamping, and admission validation every other caller goes through.

---

## Four sources, one pipeline

```yaml
gateway:
  admission:
    webhooks:
      github:
        - name: payments-repo
          enabled: true
          path: /webhooks/github/payments
          branch: main
          watch:
            - "services/*/intent.yaml"
          secretRef: { name: ork-payments-github-secret, key: secret }
          contentTokenRef: { name: ork-payments-github-app-token, key: token }
      gitlab: [ ... same shape ... ]
      slack:
        - name: platform-workspace
          enabled: true
          path: /webhooks/slack
          signingSecretRef: { name: ork-slack-signing-secret, key: secret }
          commands: ["/deploy"]
      generic:
        - name: pagerduty
          enabled: true
          path: /webhooks/generic/pagerduty
          secretRef: { name: ork-pagerduty-webhook-secret, key: secret }
```

- **GitHub / GitLab** — a push to `branch` that touches a file matching `watch` fetches that file's content and applies it as a target-mode intent.
- **Slack** — a slash command's text (`"<target> key=value ..."`) becomes the intent. Slack requires an ack within 3 seconds, so the apply itself runs on a bounded background worker pool and posts the outcome back to `response_url`.
- **Generic** — any caller that can POST JSON and sign it with HMAC. The body *is* the intent, no parsing needed.

Each entry's own `name` is what authorizes it under `serve.tokens` — the same identity model a bearer token uses — and what gets stamped as the `serve-source` provenance annotation on every CR it applies. See [Serve token permissions](../../security/08-serve-permissions.md) and [Webhook credential verification](../../security/09-webhook-verification.md) for the authentication/authorization side.

---

## Why a fourth delivery path, when `ork serve apply` already exists

`ork serve apply` requires something to run the command — a CI job, a person at a terminal. That's the right shape when a pipeline is already in the loop: a GitHub Actions workflow that already checked out the repo, already has the fields it needs, authenticating with its own `githubOIDC` identity, no webhook infrastructure required at all.

Webhook intake is for the case where you don't want to own a CI workflow file just to notify the gateway — you want the platform itself to react the instant a push lands, with no pipeline dependency. Not every team wants a `.github/workflows/notify-gateway.yml` file; some want the reaction to be a property of the platform, not something every repo has to opt into and maintain.

---

## Why fetch, not clone

ArgoCD and Flux both treat a push webhook purely as a "resync now" signal — the actual content still comes from a full git clone regardless of what changed. That's the right model when reconciling an arbitrary manifest tree, since you don't know in advance how many files matter.

A target-mode intent file is different: a handful of flat key-value lines, matched by explicit `watch` glob patterns the platform team declared, rarely more than one or two touched per push. A per-file read through the Contents API (GitHub) or Repository Files API (GitLab) is lighter than shipping git-clone infrastructure into the gateway for that. The tradeoff is real — a very broad `watch` pattern matching many files in one push means many API calls, not one clone.

!!! note "Deletion has no story yet"
    A deleted intent file is not currently treated as "tear this down" — only `added`/`modified` paths are processed. There's no content left to fetch for a removed path, and delete semantics for a push-triggered surface haven't been designed yet.

---

## Testing without a live account

`ork webhook play` runs the same chain locally — target resolution, token check, CR construction, provenance stamping, admission validation — against a real `gateway.webhooks` entry's declared `branch`/`watch`/`commands`, with no cluster, no HTTP server, and no real GitHub/GitLab/Slack account:

```bash
ork webhook list -f katalog.yaml

ork webhook play --source github -f katalog.yaml --webhook payments-repo \
  --event push-event.json \
  --fetch services/payments/intent.yaml=local-intent.yaml
```

Signature/token verification is deliberately skipped in play mode — there's no real secret exchange to test outside a live deployment — but everything downstream of that (which is most of what actually breaks: a wrong `watch` pattern, a branch mismatch, a `serve.tokens` typo) runs for real. `--simulate` extends the chain into `ork simulate`, the same way it does for `ork serve play` — the full path from a webhook payload to a reconciled resource, still with no cluster.

→ [CLI reference — ork webhook](../../reference/cli/16-webhook.md)

---

## Where to go next

- **[Webhook credential verification](../../security/09-webhook-verification.md)** — how each of the four sources proves a request is genuine
- **[Serve token permissions](../../security/08-serve-permissions.md)** — the authorization model webhook entries share with bearer tokens
- **[Local Intent Testing](06-local-intent-testing.md)** — `ork serve play`, the CLI-driven counterpart `ork webhook play` mirrors
- **[Live Delivery](07-live-delivery.md)** — what happens once a webhook entry is deployed against a real cluster
- **[Gateway as a Delivery Layer](05-gateway-as-delivery-layer.md)** — why the gateway's job ends the moment the CR is applied, regardless of which surface delivered it
