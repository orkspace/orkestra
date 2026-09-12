## v0.7.17 — Queue behaviour, pre-reconcile gating, admission runtime query

### Breaking: default queue is unlimited

`queue.maxDepth` defaults to `0` (unlimited). Previously an internal default cap was applied. Operators that relied on implicit back-pressure must now declare `queue.maxDepth:` explicitly and add `queue.behaviour:` if needed.

### Breaking: `cross.labels` renamed to `cross.labelSelector`

The YAML field for label-based cross-CRD matching is now `labelSelector`. The old name is rejected at load time.

### Breaking: `cross.source.type` renamed to `cross.source.protocol`

Values: `cr`, `health`, `metrics`, `info`, `events`. The old name is rejected at load time.

### Queue back-pressure with conditional behaviour

Declare what happens when the queue approaches or reaches `maxDepth`:

```yaml
queue:
  maxDepth: 500
  behaviour:
    onThreshold:
      value: 80
      when:
        - field: "{{ inBusinessHours }}"
          equals: "false"
    onLimit:
      drop: true
```

`onThreshold` fires at N% of `maxDepth`; `onLimit` fires at 100%. Both support `when:`/`or:` conditions evaluated with the full resolver context — time functions, notes, gate fields. Items are dropped only when conditions pass.

### Pre-reconcile gating

`preReconcile.enqueueGate` and `preReconcile.reconcileGate` conditions now delegate evaluation to `domain.Katalog` at informer time — the konstruktor registers configuration only, no closures. This breaks the import cycle between informer and pkg/katalog and makes gating consistent with queue behaviour evaluation.

### Operational state on the CR

The runtime stamps `.health` and `.metrics` onto each CR after every reconcile. These fields are readable in preReconcile conditions, validation rules, and cross-CRD references — no HTTP call needed:

```yaml
preReconcile:
  reconcileGate:
    when:
      - field: "{{ .health.status }}"
        equals: "healthy"
```

### Event-aware reconcile gating

`eventAware: true` on reconcileGate preserves individual event identity through the workqueue, preventing updates for the same object from being coalesced before reconcile-gate evaluation. Each event retains its own sentinel context. Defaults to `false`; use when event-specific gate evaluation is required.

```yaml
preReconcile:
  reconcileGate:
    eventAware: true
```

### Resource-level forceConflict

All 20 supported resources can now declaratively set `forceConflict` at the resource level. Resource-level settings override the CRD-level setting, which defaults to the system default of `true`.

### Admission — conditional runtime query

The admission webhook fetches live runtime data (health, metrics, uniqueness) only when a validation or mutation rule actually references it. CRDs with no `.health.*` or `.metrics.*` rules pay zero HTTP cost at admission time.

### Cross-CRD reads (ONCOP path 2b fix)

`readCross` rewritten with a clean two-step model: find informer (CRD-based or label-based), find CR (matchLabels → label → name), HTTP fallback. Fixes ONCOP path 2b where the URL was built but not passed to the HTTP fetch.

### Resolver moved to `pkg/template`

`pkg/resources/template` → `pkg/template`. Any operator code importing the resolver directly must update the import path.

---

## v0.7.16 — Per-target operatorBox, lifecycle:, controller-runtime compatibility

### Breaking: `anyOf:` renamed to `or:`

Global rename across the entire schema — gates, autoscale conditions, validation rules, simulation specs, serve fields. The old name is rejected at load time.

```yaml
# before
anyOf:
  - field: spec.env
    equals: production

# after
or:
  - field: spec.env
    equals: production
```

### Breaking: `resources:` renamed to `managedResources:` in the constructor block

`constructor.resources:` and `hooks.resources:` are now `managedResources:`, which distinguishes managed resource kinds from the `watch:` block. Old name rejected at load time.

### Breaking: `domain.Reconciler` interface signature

Typed operators implementing `domain.Reconciler` must update their `Reconcile` method:

```go
// before
Reconcile(ctx context.Context, key string) error

// after
Reconcile(ctx context.Context, req domain.Request) (domain.Result, error)
```

`req.Key` is `namespace/name`. `req.NamespacedName` is available directly. `domain.Result.RequeueAfter` propagates to the work queue — `ctrl.Result{RequeueAfter: X}` returned from a `domain.ReconcilerFrom`-wrapped reconciler now works without any changes inside `Reconcile`.

`ork migrate` handles this automatically. Native mode operators must update manually.

### Per-target operatorBox

Each serve target can now declare its own `operatorBox` — resources, hooks, and pre-reconcile gates scoped to that surface:

```yaml
serve:
  target:
    standard:
      operatorBox:
        reconciler:
          hooks: true
    enterprise:
      operatorBox:
        preReconcile:
          enqueueGate:
            when:
              - field: "{{ isBusinessHours }}"
                equals: "true"
```

`reconciler.default: false` wires the target's constructor from `ReconcilerRegistry`. A missing entry is a load-time error.

### `lifecycle:` block

```yaml
lifecycle:
  maturity: beta          # alpha | beta | stable | deprecated

  deprecation:
    message: "Replaced by task-runner"
    migratedTo: task-runner:v1.0.0
    timeline:
      from: "2026-01-01"
      to:   "2027-01-01"

  compatibility:
    orkestra: ">= 0.7.0"
    kubernetes: ">= 1.28"
```

A Komposer must declare `lifecycle.accept.patterns` to consume a deprecated Katalog. Without it, the deprecated operator blocks startup.

### `requeue:` — per-object requeue scheduling

```yaml
operatorBox:
  requeue:
    - when:
        - field: .status.phase
          equals: Pending
      after: "{{ timeUntil .status.certExpiresAt }}"
```

Different objects get different requeue intervals based on their own state. Complements `resync:` (uniform, whole-CRD period) — `requeue:` is conditional and per-object.

### `timeUntil` note

Returns the duration until a timestamp as a Go duration string (`"72h0m0s"`), or `"0s"` if past or invalid. Primary use: `requeue.after:` for cert-expiry-style scheduling.

### `failPolicy:` on pre-reconcile gates

```yaml
operatorBox:
  preReconcile:
    reconcileGate:
      external:
        - name: featureCheck
          url: "{{ .spec.flagUrl }}"
      failPolicy: closed   # deny reconcile when external call fails
```

`open` (default) passes through when the external call fails. `closed` denies.

### `observe.events` — Kubernetes Events as reconciliation triggers [EXPERIMENTAL]

`observe.events` extends the `observe:` block with a second observation mechanism alongside `observe.watch`. Kubernetes `Event` objects can now trigger primary CR reconciliation, with the event's own properties available as resolver context at the `enqueueGate`.

```yaml
operatorBox:
  observe:
    events:
      dbReady:
        reason: DatabaseReady
        type: Normal
        regarding:
          kind: Database
          name: my-database
        enqueueGate:
          when:
            - field: "{{ .events.dbReady.reportingController }}"
              equals: "database.myorg.io/controller"
```

The event entry matches on `reason`, `action`, `type`, `reportingController`, `reportingInstance`, `regarding`, and `related`.
Matched events resolve the primary CR key via `regarding` (default) or broadcast to all managed CRs when `regarding` is absent.

The `enqueueGate` has access to the full event context as `.events.<name>.*` — `reason`, `action`, `type`, `reportingController`,
`reportingInstance`, `regarding`, `related`. The gate can reason about what the event says, not just that it happened. The same `when:`/`or:` condition machinery applies — no new syntax.

`observe.watch` and `observe.events` are siblings under `pkg/runtime/informer/observe`. Both share the same enqueue path, the
same gate evaluation, and the same resolver construction. The only difference is what they observe and what context they inject.

This is marked experimental. `ork validate` emits a warning when event entries are declared. The `regarding` / `keyFrom` relationship and
edge case handling are being settled in follow-up PRs before the experimental flag is removed.

Use when a secondary controller already emits Events and you want to react without polling, without watching the secondary CRD, and without writing a bridge controller.

---

## v0.7.15 — Artifact signing, gateway webhooks, multi-cluster, simulate --envtest, pre-reconcile gates

### Artifact signing

Sign and verify patterns via Cosign keyless signing — the OIDC token CI already issues is the credential:

```bash
ork push --sign                          # push and sign
ork pattern sign ghcr.io/org/my-op:v1   # sign after push
ork pattern verify ghcr.io/org/my-op:v1
ork push --sign-local                    # push to ttl.sh and sign for local testing
```

Declare signing policy in the Katalog:

```yaml
publish:
  signing:
    verify: true
    expectedIdentities:
      - github.com/myorg/payments/.github/workflows/release.yaml@refs/heads/main
  tests:
    e2e: true
    simulate: true
```

`ork pull --verify` refuses unsigned artifacts when `verify: true` is set. Cosign resolves from `$PATH`, then `~/.orkestra/tools/cosign`, then downloads automatically on first use.

### Gateway webhook intake

Inbound push-based delivery from GitHub, GitLab, Slack, and generic webhooks — routes through the same target-mode pipeline as `POST /api/v1/apply`:

```yaml
gateway:
  webhooks:
    github:
      - name: payments-repo
        path: /webhooks/github/payments
        branch: main
        watch: ["services/*/intent.yaml"]
        secretRef: { name: ork-payments-github-secret, key: secret }
        contentTokenRef: { name: ork-payments-github-app-token, key: token }
    slack:
      - name: platform-workspace
        path: /webhooks/slack
        signingSecretRef: { name: ork-slack-signing-secret, key: secret }
        commands: ["/deploy"]
```

Webhook names resolve as `serve.tokens` identities and are stamped as provenance annotations on every CR applied.

`ork webhook list` and `ork webhook play` let you inspect and locally test webhook entries without a running cluster or real GitHub/GitLab/Slack account.

### Multi-cluster routing

```yaml
gateway:
  clusters:
    prod:
      endpoint: https://prod.internal:6443
      tokenRef: { name: orkestra-prod, namespace: default, key: token }
      caRef:    { name: orkestra-prod, namespace: default, key: ca.crt }
```

Route CRDs or targets to named clusters via `serve.cluster` and `serve.target.<name>.cluster`. Template expressions resolve at apply time from the intent payload.

- `ork clusters validate` — offline validation of cluster config
- `ork clusters check` — live connectivity and CRD presence check
- `ork clusters bootstrap` — provision least-privilege access on a target cluster

### `ork simulate --envtest`

Run simulate specs against a real `kube-apiserver` + `etcd` locally — no cluster, no deployed operator:

```bash
ork simulate -f simulate.yaml --envtest
```

Requires `crd:` or `crdFiles:` in the spec. Envtest binaries download automatically to `~/.ork/envtest-bins` on first use.

### Pre-reconcile gates

Drop objects before they reach the reconciler, at two points in the pipeline:

```yaml
operatorBox:
  preReconcile:
    enqueueGate:      # informer layer — no health state change
      when:
        - field: "{{ .spec.active }}"
          equals: "true"
    reconcileGate:    # kordinator layer — sets health to gated
      or:
        - field: "{{ .spec.environment }}"
          equals: "production"
        - field: "{{ .spec.environment }}"
          equals: "staging"
```

Both gates support `external:` calls and `failPolicy:`.

### `ork gate run` — local gateway for Serve development

`ork gate run -f katalog.yaml` starts the gateway in HTTP-only mode for testing serve routing and intent apply flows locally without a cluster.

### Breaking: `kubeclient.KubeClient` renamed to `kubeclient.Interface`

Update any typed operator code referencing `kubeclient.KubeClient` to `kubeclient.Interface`.

---

## v0.7.14 — Serve aliases, provenance, OIDC tokens, and local intent testing

### Serve aliases

A CRD can expose multiple named entry points with independent token scopes and response configs:

```yaml
serve:
  target:
    primary:
      primary: true
    preview:
      tokens:
        preview-team:
          permissions:
            global: ["apply"]
```

Each alias accepts the same apply API. Once a CR is created via an alias, only that alias can update it — pass `?override=true` to switch surfaces.

### Intent provenance annotations

Every gateway-applied CR is stamped with:
- `orkestra.orkspace.io/serve-target` — primary target name
- `orkestra.orkspace.io/serve-alias` — alias name, or `""` for primary
- `orkestra.orkspace.io/serve-source` — verified OIDC `sub` claim, or `""` for static token

Seven built-in notes expose these in template expressions and `when:` conditions: `getServeTarget`, `getServeAlias`, `getServeSource`, `hasServeTarget`, `hasServeAlias`, `hasServeSource`, `isDirectApply`.

### OIDC token authentication

Short-lived tokens from GitHub Actions, GitLab CI, HashiCorp Vault, or any OIDC provider:

```yaml
gateway:
  api:
    auth:
      tokens:
        - name: gh-ci
          githubOIDC:
            allow:
              repository: myorg/payments
              ref: refs/heads/main
```

`ork token list` / `ork token verify` / `ork token probe` inspect and test token entries locally without a running gateway.

### `ork serve play` / `ork gate` / `ork serve apply`

- `ork serve play` — runs the full apply chain locally from an intent file (target resolution, token check, CR construction, admission, response). Add `--simulate` to test the result end-to-end.
- `ork gate -f katalog.yaml --cr cr.yaml` — evaluates admission rules locally against a CR.
- `ork serve apply -f intent.yaml --api https://gateway.myorg.io` — sends intent to a live gateway.

### Serve field translation

Transform caller input before it reaches the CR:

```yaml
serve:
  fields:
    schedule:
      values:
        schedule.minute:     '{{ cronMinute .value }}'
        schedule.hour:       '{{ cronHour   .value }}'
        schedule.dayOfMonth: '{{ cronDom    .value }}'
```

`.value` is the raw submitted value. `.request` is the full intent payload and available in all expression contexts.

### `fires.reconcile: false`

Marks a validation or mutation rule as admission-only — skipped on every reconcile. Use for rules that reference `.request.*`, which is only present at admission time.

### Deprecation timelines and accept gates

```yaml
lifecycle:
  deprecation:
    timeline:
      from: "2026-01-01"
      to:   "2027-01-01"
    accept:
      beforeEol: true
      eol: true
```

`ork run` blocks on deprecated operators until the consuming Komposer explicitly acknowledges them.

---

## v0.7.13 — External protocols, serve layer, new condition operators, two breaking changes

### `validation.external` and `mutation.external`

External calls can now fire at admission time, not only reconcile time:

```yaml
validation:
  external:
    - name: healthCheck
      url: "{{ .spec.serviceUrl }}/health"
      fires:
        reconcile: false   # admission-only
  rules:
    - field: "{{ .external.healthCheck.status }}"
      equals: "200"
      action: deny
      message: "health check failed"
```

Results propagate into all template contexts, including status fields and resource templates.

### External protocol clients

`external:` blocks support native protocol clients alongside HTTP:

| Protocol | `protocol:` value |
|---|---|
| Prometheus | `prometheus` |
| Redis | `redis` |
| PostgreSQL | `postgres` |
| MongoDB | `mongo` |
| Kafka | `kafka` |

Results available at `external.<name>.*` in all template contexts.

### New condition operators

`gte`, `lte`, `between`, `notBetween`, `notIn`, `notContains`, `regex`, `notPrefix`, `notSuffix` — available in `when:`, `or:`, and `validation.rules`.

Fixes: `gt`/`lt` in validation rules were accidentally inclusive; `operator: in` was never evaluated in validation rules (silently always passed).

### `operator: unique`

Check that a field value is unique across all CRs of this CRD. Authoritative at reconcile time; best-effort early rejection at admission time.

### Breaking: `labels`/`annotations` move to native map syntax

```yaml
# before
labels:
  - key: app
    value: "{{ .metadata.name }}"

# after
labels:
  app: "{{ .metadata.name }}"
```

Applies to `selector:`, `labelSelector:`, and `fieldSelector:` too.

### Breaking: `envFrom.secretRef`/`configMapRef` move to struct

```yaml
# before
envFrom:
  secretRef: [myapp-creds]

# after
envFrom:
  secretRef:
    - name: myapp-creds
      prefix: "DB_"
```

### `serve labels/annotations` — expose metadata as serve fields

```yaml
serve:
  labels:
    team:
      label: "Team"
      required: true
```

Written to `metadata.labels`/`metadata.annotations` on apply. `required: true` synthesizes a server-side `exists` validation rule; `type: enum` synthesizes an `in` rule.

### `serve.fields.path` — nested spec paths

`path: app.resources.cpu` maps a flat serve field to a nested spec location.

### `validation.rules` `link:` field

Names the serve field a validation rule concerns, so clients can highlight the offending form field by name:

```yaml
validation:
  rules:
    - field: '{{ getLabel . "team" }}'
      link: team
      operator: exists
```

### `kubectl.apply` — admission rejection tests

```yaml
kubectl:
  apply:
    - file: ./cr-invalid.yaml
      exitCode: 1
      outputContains: "spec.domain must be unique"
```

Assert that an apply should be rejected, and what the message contains.

---

## v0.7.12 — Gateway API, serve, hook args, SSA, and codebase restructure

### Gateway API

Three endpoints when `gateway.api.enabled: true`:

- `POST /api/v1/apply` — apply a CR or flat intent; returns `accepted`, `pollUrl`
- `GET /api/v1/resources/{kind}/{ns}[/{name}]` — read or list CRs without kubeconfig
- `DELETE /api/v1/resources/{kind}/{ns}/{name}` — delete a CR

All routes require a bearer token declared in `gateway.api.auth.tokens`.

### Serve layer — developer self-service

`serve.enabled: true` surfaces a `[+ Create]` form in the Control Center. Fields, labels, and hints come from `serve.fields`. `serve.name` and `serve.namespace` resolve server-side from the intent — callers never supply them directly.

Target mode: callers can submit `{"target": "myoperator", ...fields}` instead of a full CR. The gateway builds the CR.

### Hook and constructor args

Per-CRD configuration passed to hooks and constructors at reconcile time, with full template support:

```yaml
operatorBox:
  reconciler:
    hooks:
      args:
        region: '{{ default "us-east-1" .spec.region }}'
```

Read via `kube.Args()`.

### Notes in validation and mutation rules

User-defined notes resolve as template expressions inside `validation.rules` and `mutation.rules`, at reconcile time and at admission time.

### Conditional validation and mutation rules

`when:` and `or:` on `validation.rules` and `mutation.rules` entries. Non-matching rules are skipped entirely — at reconcile time and admission time.

### Server-Side Apply

All reconcilable resource types now use SSA (`fieldManager: orkestra-runtime`). Eliminates false-positive drift from Kubernetes-injected defaults.

---

## v0.7.11 — Workload autoscaler, user-defined notes, and new example packs

### `autoscale:` block

Declarative replica control for Deployments, StatefulSets, and ReplicaSets:

```yaml
deployments:
  - name: "{{ .metadata.name }}"
    replicas: 2
    autoscale:
      min: 2
      max: 10
      cooldown: 3m
      scaleUp:
        conditions:
          when:
            - field: external.queue.pendingJobs
              greaterThan: "100"
        increment: 2
      scaleDown:
        conditions:
          when:
            - field: external.queue.pendingJobs
              lessThan: "20"
        decrement: 1
```

Scale signals: time/day conditions, `external:` HTTP metrics, `cross:` sibling CRD status fields. Step (`increment`/`decrement`) or jump (`target`) scaling. Drift correction is suppressed for autoscaled workloads.

### User-defined notes

Named template expressions callable anywhere in a Katalog:

```yaml
notes:
  - name: serviceHost
    expression: "{{ .metadata.name }}.{{ .metadata.namespace }}.svc.cluster.local"
```

Package notes in a Motif, distribute via `spec.imports`, override at the Komposer level.

### `spec.imports` — Katalog-wide Motif scope

```yaml
spec:
  imports:
    - motif: ./org-standards.yaml
```

Profiles and notes declared in the Motif are available to every CRD without per-CRD imports.

### `ork proxy`

Port-forward for Helm-deployed Orkestra. Always targets the leader pod. Reconnects on pod replacement.

```bash
ork proxy               # Runtime, Control Center, and Gateway
ork proxy --for cc      # Control Center only
```

---

## v0.7.10 — E2E DSL, endpoint control, OCI run, and cluster improvements

### `kubectl:` DSL block in e2e

Eleven subcommands (`get`, `logs`, `describe`, `exec`, `port-forward`, `apply`, `patch`, `delete`, `events`, `auth`, `top`) inside `expect:` entries. All share `equals`, `notEquals`, `outputContains`, `outputNotContains`, `greaterThan`, `lessThan` assertion fields.

`leaderElection:` on `port-forward` and `logs` resolves the target pod from a Kubernetes Lease — essential for multi-replica deployments.

### `ork run <name>:<version>` — OCI run

```bash
ork run postgres:v1.0.0 --dev
ork run postgres:v1.0.0 --dev --apply-cr
```

Pull and start a pattern from the registry in one command.

### `ork delete cluster`

```bash
ork delete cluster --name ork-e2e
```

### Endpoint control in the Control Center

CRDs with `endpoints: enabled: false` render a clean disabled state instead of zeros or errors.

### `ork e2e --report-file`

```bash
ork e2e --report-file "$GITHUB_STEP_SUMMARY"
```

GFM markdown table — renders directly in GitHub Actions.

### `spec.onFailure` and per-expectation `onFailure`

Capture logs, describe output, and raw commands at failure — globally after all expectations, or immediately when a specific checkpoint fails.

---

## v0.7.9 — Reconciler config, user-defined profiles, and resilience examples

### Breaking: reconciler config moves inside `operatorBox`

```yaml
# before
spec:
  crds:
    service:
      workers: 4
      resync: 30s

# after
spec:
  crds:
    service:
      operatorBox:
        reconciler:
          workers: 4
          resync: 30s
```

### `reconciler` profile class

```yaml
operatorBox:
  reconciler:
    profile: high-throughput   # workers: 10, resync: 5m, queue.maxDepth: 1000
```

Built-in: `high-throughput`, `conservative`, `development`. User-defined profiles override built-ins.

### User-defined profiles for resources, probes, container security, and pod security

All profile classes now support named presets in `profiles:`. Declare in a Motif; reference by name across every Katalog that imports it.

### `include:` in `expect:` — test composition

```yaml
expect:
  - include: ./infra-ready.yaml
  - name: My check
    ...
  - include: ./cleanup.yaml
```

---

## v0.7.8 — NetworkPolicy, ResourceQuota, LimitRange, ClusterRole, ClusterRoleBinding

Five resource types promoted to first-class Orkestra resources with full profile support, GC, and e2e assertion coverage.

Built-in profiles: NetworkPolicy (`deny-all`, `allow-same-namespace`, `allow-dns-egress`), ResourceQuota (`small`, `medium`, `large`, `xlarge`). All six profile classes support user-defined presets.

New sub-pack: `use-cases/namespace-provisioner` — a single CRD that provisions a namespace with quota, RBAC, and network policy in one apply.

New command: `ork create pattern` scaffolds a complete operator suite from a prompt.

---

## v0.7.7 — New packs, typed group overrides, universal e2e

### New packs

```bash
ork init --pack from-controller-runtime    # baseline → declarative → hooks → constructor
ork init --pack ecosystem-composition      # ArgoCD, cert-manager, Prometheus, Crossplane
ork init --pack resilience                 # panic isolation, CRD recovery, leader failover
```

### Typed apiTypes group override

Publish a typed operator under one API group, consume it under another — no source fork required. Set `apiTypes.group` in your Komposer override.

### Patch API simplified

`PatchStatus`, `PatchFinalizers`, `PatchLabels`, `PatchAnnotations`, and `PatchSpec` no longer take an explicit `gvr`:

```go
// before
r.kube.PatchStatus(ctx, obj, apiv1.GroupVersionResource, fields)

// after
r.kube.PatchStatus(ctx, obj, fields)
```

### Breaking: `operatorBox.reconciler` sub-block

`hooks:` and `constructor:` move under `operatorBox.reconciler:`. Declarative-only Katalogs: remove `default: true` entirely.

```yaml
# before
operatorBox:
  default: true
  hooks:
    location: ...

# after
operatorBox:
  reconciler:
    hooks:
      location: ...
```

### Breaking: universal e2e target

```yaml
# before
spec:
  customOperator: true

# after
spec:
  custom:
    target: kubernetes
```

---

## v0.7.6 — Registry Guide pack, CLI UX polish, bug fixes

### Registry Guide pack

A structured, zero-to-production walkthrough of the Orkestra registry — 13 self-contained steps from consuming a published pattern on day one to automated CI publishing with GitHub Actions. Covers declarative operators, typed Go operators with hooks, multi-katalog Komposers, CRD API evolution, deprecation workflows, and the full `ork push` gate pipeline.

```bash
ork init --pack registry-guide
```

### CLI UX polish

- **`ork push`** — `--use-current` and `--cluster` flags let you reuse an existing cluster for the e2e gate, skipping cluster provisioning for faster local iteration.
- **`ork push`** on typed operators — `RuntimeVersion` is read from `go.mod` and recorded as an OCI annotation. Visible in `ork inspect` as `Runtime: vX.Y.Z`.
- **`ork pull`** — warns when the pulled typed operator was built against a different Orkestra version than the current binary.
- **`ork inspect`** — shows `Typed: ✓ hooks` and `Runtime: vX.Y.Z` for typed patterns.
- **`ork validate` / `ork simulate`** — surface a clear `TypedOperatorError` with an actionable `ork inspect <ref>` hint when a Komposer sources a registry-based typed operator.

### orkestra-action

- `template: "true"` now auto-detects `katalog.yaml` in the working directory, consistent with `validate: "true"` and `simulate: "true"`.
- Install step downloads directly from GitHub releases — no CDN redirect, resilient to network issues.
- `/ready` and `/started` health endpoints added to the dev server image.

### Bug fixes

- Probe path templates (`{{ .spec.probePath }}`) now resolve correctly in Deployment, ReplicaSet, StatefulSet, and Pod specs.
- Deployment no longer triggers a no-op update every reconcile when an HTTP probe is configured (missing `Scheme` field on `HTTPGetAction`).
- Helm bundle deploy correctly routes CRDs to their declared `metadata.namespace` — each team sees their own namespace panel in the Control Center.
- `ork inspect --versions` table columns align correctly when version strings are colorized.
- `ork inspect --versions` works without a version tag in the argument.
- Deprecated pattern row aligns correctly in `ork patterns` — `⚠` marker moved to its own column.
- Deprecation message is shown even when no migration target is set.
- Control Center version labels no longer double the `v` prefix.
- Control Center katalog panel shows the namespace-level description when a namespace filter is active.

---

## v0.7.4 — pkg/resources rename

`pkg/orkestra-registry` is renamed to `pkg/resources`.

**Who is affected:** typed-mode operators only — Go code that writes custom hooks or constructors and imports from `pkg/orkestra-registry`. Dynamic-mode operators (pure YAML) are unaffected.

**What to change:** update import paths from `pkg/orkestra-registry/<kind>` to `pkg/resources/<kind>`:

```go
// before
"github.com/orkspace/orkestra/pkg/orkestra-registry/deployments"

// after
"github.com/orkspace/orkestra/pkg/resources/deployments"
```

**Why:** `pkg/orkestra-registry` and `pkg/registry` (the OCI distribution layer) both carried the word "registry" with no relation to each other. The resource library — idempotent Create/Update/Delete/Resolve implementations for every Kubernetes resource type — is not a registry. `pkg/resources` is accurate and removes the collision. No logic changes, no API changes, no behavioral differences.

---

## v0.7.3 Simulate: standalone operation

`ork simulate` is now a self-contained command. Key changes:

**Flat imports syntax** — simulate aggregators now use a plain list, matching e2e:

```yaml
imports:
  - ./09-hooks/simulate.yaml
  - ./10-constructor/simulate.yaml
```

**`--dev-server` flag** — start the mock server before running simulate, no cluster needed:

```bash
ork simulate --dev-server
ork simulate -f simulate.yaml --dev-server
```

**Decoupled from e2e** — passing an `e2e.yaml` to `ork simulate` now returns a helpful error instead of silently falling back to op-print mode.

**Example suites** — root `simulate.yaml` aggregators added to the beginner, intermediate, and external example packs. The three external examples with `serviceUrl`/`healthCheckUrl` now have full simulate.yaml files with `expect:` assertions, all runnable with `--dev-server`.

**health-gate CRs** — `cr-healthy.yaml` and `cr-degraded.yaml` now point to `localhost:9999` instead of httpbin.org, so they work with `--dev-server` without any external network dependency.

**`ork simulate init` improvements** — the generated `simulate.yaml` now includes a commented `absent:` block (seeded with the first observed resource type) as a hint for failure-path coverage. A new `--dry-run` flag prints to stdout instead of writing the file — useful for previewing before committing.

## v0.7.1 — Simulate + E2E fixes

### `ork simulate`

- **`e2e.yaml` input** — `ork simulate -f e2e.yaml` reads `spec.katalog` and `spec.cr`; skips `customOperator: true`; expands aggregator imports recursively.
- **`./...` discovery** — discovers all `*e2e.yaml` files, prints per-file results and aggregate summary; exit code 1 on cycle errors; `--skip` patterns follow the same convention as `ork e2e ./... --skip`.
- **`--skip-external`** — stubs `external:` HTTP calls with empty 200 responses. Default: calls hit the real network.
- **`--debug-ops`** — prints every recorded op with cycle number (diagnostic).
- **GVK-aware CR matching** — multi-document CR files (separated by `---`) supported; each CRD matched to the CR whose `kind` matches `apiTypes.kind`; CRDs with no matching CR skipped with a note.
- **`cross:` observation** — when sibling CRDs' CRs are present in the CR file, they are seeded into per-CRD fake informers and wired as `katalogRegistry`; `cross.*` fields populate correctly.
- **Hook wiring** — `HookRegistry[gvk]` looked up at runtime; custom binary runs real hooks against the fake cluster.
- **Constructor wiring** — `ReconcilerRegistry[gvk]` looked up; constructor called with `fakeKube`, `informer`, and `event.Discard()`.
- **Typed indexer** — typed CRDs have their CR converted via JSON round-trip to the registered Go type before being seeded; fixes constructor type-assertions and hook `BindToObjectHooks` panics.
- **`event.Discard()`** — constructors receive a silent no-op recorder; `noop.go` removed; `Recorder` interface moved to `event.go`.
- **Motif path fix** — `isFileMotif()` implemented; motif import file paths absolutized relative to the katalog dir (same fix as `crdFile`).

### E2E

- **`crdFile` path in bundle** — `CRDFile` is now cleared from every `CRDEntry` after `populateAPITypesFromCRDFile` runs; `apiTypes` are fully populated before the field is erased. Prevents the runtime pod from trying to read a local filesystem path that does not exist inside the container.
- **`10-motif-composition` e2e** — `crd-worker.yaml` added to `setup.apply`; both CRDs are installed before the CR is applied.

---

## v0.7.0 — Early Access

This release ships the first public early-access build of Orkestra. It focuses on the E2E framework, the typed operator toolchain, and making every example runnable end-to-end without manual pre-steps.

### E2E Framework

- **`wait:` on import declarations** — sleep a duration before an import starts; validated at load time, shown in `ork validate`.
- **`spec.customOperator: true`** — skip the Orkestra bundle and Helm install; use `ork e2e` as a pure test harness for any operator (cert-manager, custom controllers, etc.).
- **Shared Orkestra across imports** — one Helm install and one uninstall per suite; `sharedOrkestra` prevents namespace deletion from cascading across imports. All sync output suppressed.
- **`ork e2e ./...` discovery** — recursive discovery of e2e files, skips pure aggregators, supports `--wait` and `--skip`.
- **`--dry-run` flag** — single file calls `ork validate`; `./...` lists files with count and first ten.
- **Helm output suppression** — install/uninstall/setup output captured silently; included in error message on failure.
- **Bug fixes** — `isPureAggregator` inline check, `WaitForResource` for Deployments, `checkAll` command-before-resource ordering.

### CLI — `ork generate registry`

- **Registry generation fixed** — `generateKatalog` was setting `kat.Spec` but never calling `m.Enabled()`, so `kat.Enabled()` always returned nil. `generate.TypeRegistry` iterated over an empty map and silently exited on every invocation — no registry file was ever written.
- **`TypeRegistry` returns `(bool, error)`** — `ensureMainGo` is now only written when the registry actually produced content.
- **`CustomHooksEnabled` and `ConstructorEnabled` corrected** — both methods returned `== nil` instead of `!= nil`. Safe for declarative CRDs (all call sites are guarded by `DefaultReconcile()`) but would have caused panics and wrong behaviour for typed operators.
- **Output style** — `→ generating registry for <module>`, `✓ pkg/typeregistry/zz_generated_typeregistry.go`, `○ nothing to generate` — matches the rest of the CLI.

### CLI — `ork init --pack advanced`

- **Typed examples embedded** — `go:embed` silently skips subdirectories containing a `go.mod` (nested module rule). `09-hooks`, `10-constructor`, and `11-mixed-operator-pattern` were missing from every `ork init --pack advanced` invocation. Fixed by renaming `go.mod`/`go.sum` → `.txt` at embed time.
- **Transparent extraction** — `ork init` now restores `go.mod.txt` → `go.mod`, `go.sum.txt` → `go.sum`, and strips `//go:build ignore` from all extracted `.go` files. Users get a fully compilable project with no manual steps.
- **Makefile safety net** — `make registry` and `make build` in typed examples perform the same restoration for users who clone the repo directly.

### CLI — path-relative resolution

- `crdFile`, `crFiles`, `setup.apply`, and Komposer `imports.files` now resolve relative to the declaring file. `ork run -f /any/path/katalog.yaml` works from any directory.
- CLI-provided `-f` paths are converted to absolute on intake so downstream resolution is always anchored correctly.

### Examples

- **`examples/use-cases/full-stack-app`** — all six sub-katalogs switched from inline `apiTypes:` to `crdFile:`; manual `kubectl apply -f crd.yaml` pre-step eliminated from every walkthrough. `03-cross-crd/crd.yaml` split into `crd-managed-database.yaml` + `crd-database-backed-app.yaml`. `06-full-stack` uses a `setup:` block to pull in the managed-database dependency.
- **Advanced typed examples** — `09-hooks` (typed Go hooks), `10-constructor` (custom reconciler), and `11-mixed-operator-pattern` (dynamic + hooks + constructor together) fully implemented, documented, and e2e-ready.
- **Typed e2e documented** — READMEs for typed examples now explain `--set runtime.image.repository` / `--set runtime.image.tag` so `ork e2e` deploys the user's custom image (which contains the generated type registry) instead of the default Orkestra image.
