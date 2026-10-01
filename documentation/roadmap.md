# Roadmap

*Last updated: July 2026*

---

## Where we are

Orkestra is the Kubernetes operator runtime. The core is shipped and running in production. Here is what exists today:

**Runtime**

- Dynamic mode — zero-code operators, no generated types, no compilation step
- Typed mode — Go types, Go hooks, custom constructors when you need them
- Generic Reconciler with three-path dispatch: templates, hooks, constructor
- Per-CRD isolation — dedicated informer, workqueue, and worker pool per CRD
- Dependency graph — topological startup order (`dependsOn`), cycle detection
- safeReconcile — panic recovery per CRD, other CRDs unaffected
- Konductor election — leader election with warm-cache follower failover
- Autoscale — dynamic worker and resync scaling based on metrics

**Declarations**

- Katalog — CRDs, reconcile templates, workers, resync, dependencies, conversion rules
- Komposer — compose Katalogs from files, Helm charts, and OCI/Git registries
- Motifs — reusable resource primitives shared across Katalogs via the motif registry
- Conditions (`when:`) — conditional resource creation based on CR field values
- Declarative version conversion — conversion rules in YAML, no Go code
- Declarative validation — deny/warn rules at reconcile and admission time
- Declarative mutation — defaults and overrides at reconcile and admission time

**Platform**

- Gateway (`ork gate`) — admission webhooks, TLS, conversion webhooks, notifications
- Control Center (`ork control`) — live operator dashboard, multi-runtime support
- Registry (`ork push`, `ork pull`, `ork inspect`, `ork patterns`) — publish, pull, and inspect OCI patterns
- Simulate (`ork simulate`) — declarative reconciler assertions, zero-cluster, `simulate.yaml` kind with assert mode and registry gate
- E2E (`ork e2e`) — declarative end-to-end testing that gates registry publication

**Security**

- Namespace protection — admission and runtime enforcement, two independent layers
- Deletion protection — CR and CRD deletion guarded by labeled finalizers
- Admission control — deny/warn rules at admission time without a webhook server
- RBAC generation — `ork generate bundle --for runtime` produces scoped ClusterRoles

**CLI**

`ork init`, `ork run`, `ork gate`, `ork validate`, `ork template`, `ork simulate`, `ork plan`, `ork diff`, `ork generate`, `ork push`, `ork pull`, `ork inspect`, `ork patterns`, `ork control`, `ork notes`, `ork e2e`, `ork version`

**Distribution**

- Homebrew tap — `brew install orkspace/tap/ork`
- curl installer — `curl -sSL get.orkestra.sh | bash` with GPG signing
- Docker image — GHCR, distroless, two-stage build
- Helm chart — production-ready deployment chart

---

## Where we are going

### Pod security profiles ✓ shipped

Declarative pod security per workload resource. Set a named profile in one line:

```yaml
securityContext:
  profile: hardened
podSecurity:
  profile: hardened
```

`hardened` sets `runAsNonRoot: true`, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, drops all capabilities. Profiles: `baseline`, `restricted`, `hardened`. Individual fields can be declared instead of a profile. → See [Pod security](./security/07-pod-security.md).

### Operator as library ✓ shipped

Orkestra is a Go library. Teams can import it (`go.mod` version pin) and write their own entrypoint — full control, no fork needed:

```go
func main() {
    kfg, err := konfig.Init()
    if err != nil {
        logger.Fatal().AnErr("failed to load configurations", err)
        utils.Exit(err)
    }
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    cli.Execute(kfg, ctx)
}
```

They get the full runtime, gateway, CLI, and webhook system. If they need a custom webhook, they know exactly where to plug it in. Two things needed: a version-pinned `go.mod` import and this entrypoint.

### ork simulate init ✓ shipped

`ork simulate` currently requires you to run `--debug-ops`, read the output, and manually write `expect:` rules. `ork simulate init` closes the loop:

```bash
ork simulate init              # reads katalog.yaml + cr.yaml in current dir
ork simulate init -f katalog.yaml --cr cr.yaml
```

It runs the reconciler once with `--debug-ops` internally and generates a `simulate.yaml` pre-filled with the observed cycle-1 ops as `expect:` rules. The user gets a working assertion file without writing anything — edit and refine from there. Time-to-first-assertion: zero.

### simulate.yaml expect.absent ✓ shipped

`expect:` currently asserts that ops happened. `expect.absent` asserts they did not:

```yaml
expect:
  ops:
    - cycle: 1
      verb: create
      resource: ingresses   # assert ingress WAS created
  absent:
    - cycle: 1
      verb: create
      resource: ingresses
      name: my-app-ingress  # assert this specific ingress was NOT created
```

This covers conditional resources — created only when a spec field is present, absent when it is not. Without absent assertions, the gap between "the Ingress is conditional on spec.host" and "the simulation verified the conditional works" cannot be closed in `simulate.yaml`.

### Simulate steady-state diagnostics

Today, when a simulation does not reach steady state, the output shows which ops changed in each cycle but does not identify the root cause. The enhancement adds a "why not steady" block:

```text
  ~ Max cycles reached (10) in 381ms

  Unstable ops (still changing at cycle 10):
    ~ status/my-app  ← status.phase transitions on every cycle (spec.replicas note re-evaluates)
    ~ secrets/my-app-creds  ← once: check fires every cycle (secret exists but update still runs)
```

This makes it immediately clear which resource is preventing convergence and why — without reading ten cycles of output.

### Policy

`Policy` is a first-class Orkestra pattern kind — authored as `policy.yaml`, versioned, and published to an OCI registry like any other artifact. It defines the rules that `ork lint` enforces and travels with the patterns it governs.

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: Policy
metadata:
  name: myorg-standards
  version: v1.0.0

linting:
  rules:
    - id: no-missing-resource-requests
      severity: error
    - id: secret-rotation-required
      severity: warning

registry:
  allowedRegistries:
    - ghcr.io/myorg/prod
    - name: ghcr.io/myorg/staging
      actions:
        push: false
        pull: true
```

A policy can import other published policies. If two imported policies conflict on the same rule, the aggregation fails — no silent merge, no last-one-wins. Policies attached to a Komposer are authoritative: built-in rules can be overridden by the policy author, but org-wide policy rules cannot be overridden downstream.

If `policy.yaml` is present when a pattern is pushed, it is embedded in the OCI artifact and enforced automatically when consumers run `ork lint` against that pattern — no `--policy` flag required.

### Concurrent E2E imports

`ork e2e` files with `imports:` are aggregators — they run independent E2E specs sequentially today. The improvement makes concurrency the default: imports run in parallel unless they declare `dependsOn:` to another import.

For Orkestra operator imports, Orkestra is installed once per namespace rather than once per cluster. Each import gets its own namespace-scoped Orkestra instance, isolated state, and independent assertion loop. The coordinator manages the shared cluster and tears down each namespace after its import completes.

For `custom.target: kubernetes` imports there is no Orkestra installation — each import applies and asserts against its own set of resources. Concurrency there is straightforward and requires no cross-import coordination.

The practical effect: a suite of 10 independent E2E specs that today takes 10 × test-time runs in approximately 1 × test-time. CI feedback for full suite runs drops from tens of minutes to the time of the slowest single import.

---

### ork plan — deeper intent analysis

`ork plan` exists today and shows a diff between the current katalog and the running state. It is useful but limited — it does not yet explain *why* resources will change, surface drift from outside reconcile, or show which CRs will be affected by a template change.

Planned improvements:

- **Intent diff** — distinguish a template change that affects 3 of 12 CRs from one that affects all of them. Show which CRs are affected and how.
- **Drift detection** — detect resources that exist in the cluster but are no longer declared in the Katalog. Surface them as orphans before apply, not after.
- **Dependency impact** — when a Katalog change affects a dependency, surface the downstream CRDs that will re-reconcile as a result.
- **Structured output** — `--output json` for CI integration; diff summaries as GitHub step comments via `ork-action`.

Until these land, `ork plan` is a basic diff — helpful for catching renames and field changes, not yet suitable as a full pre-apply review gate.

### ork lint

`ork validate` checks schema correctness — the document is well-formed. `ork lint` checks semantic correctness — the document is safe and sound for your deployment context.

```bash
ork lint -f katalog.yaml
ork lint -f katalog.yaml --policy org-policy.yaml
```

Examples of what lint catches that validate cannot:

- A Deployment with no resource requests (will be evicted under pressure)
- A ServiceAccount bound to cluster-wide verbs (over-privileged)
- A Secret with no rotation policy declared
- A CRD with `condition: healthy` on a dependency that has a history of degradation

Lint runs at CI time, not author time. It is a different gate — closer to `golangci-lint` than to `go vet`.

Lint rules are defined in a `Policy` pattern — the same publishable, versioned artifact as a Katalog or Motif. Run `ork lint --policy oci://ghcr.io/myorg/policies/myorg-standards:v1.0.0` to apply your organisation's published standards, or attach `policy:` in a Komposer to enforce them automatically at validate time without a separate lint step in CI.

### Namespaced katalogs

Today, the merger merges all Katalog sources into one flat runtime Katalog. A Katalog with `namespace: platform-team` would stay scoped — the merger produces `map[namespace]*Katalog` instead of one merged output. Each namespaced Katalog runs in its own reconciler scope with independent health tracking, independent workers, and real isolation from other namespaces.

The Control Center shows each namespace as a separate panel — from its perspective, namespaced Katalogs look like separate runtimes.

This makes Orkestra usable as a shared platform primitive: one Orkestra instance, multiple teams with real isolation, no cross-contamination when one team's CRD degrades.

### Performance benchmarks

Published numbers for reconcile throughput, queue latency, and informer memory usage at 50+ and 100+ CRDs. Stress test results with quality gates.

### CNCF Sandbox

Target 2027. Prerequisite is production adoption at multiple organisations, with metrics. CNCF Sandbox gives Orkestra vendor neutrality, community governance, and the credibility that enterprise platform teams require before adopting an open-source runtime.

---

## The longer horizon

### Declarative canary rollouts

A `rollout:` block in the Katalog gates how a template change propagates:

```yaml
operatorBox:
  rollout:
    strategy: canary
    initialWeight: 10
    increment: 20
    interval: 5m
    gate:
      metric: error_rate
      threshold: "< 1"
```

Orkestra manages the weight split, polls the gate condition (using the same expression engine as `when:`), and advances or rolls back automatically. The substrate already has all the pieces — template engine, health model, conditional evaluation. Canary is applying them to a new lifecycle concern.

### Katalog and Komposer as native Kubernetes kinds

Katalog and Komposer as native Kubernetes kinds — registered by the cluster itself, understood by `kube-controller-manager`, auditable through the standard Kubernetes audit log.

```bash
kubectl get katalogs          # not yet, but this is where we are going
kubectl describe katalog website-operator
```

The path: production adoption → CNCF Sandbox → Kubernetes Enhancement Proposal → alpha behind a feature gate → beta → general availability. A realistic timeline is five years. The work is not primarily technical — the design is largely correct. The work is community trust.

See [Declarative Operators: A New Model for Kubernetes Extensibility](/publications/declarative-operators-whitepaper/)
for the full argument.

---

## What we are not building

**Replacing controller-runtime.** Orkestra is a higher-level abstraction. Custom constructors bridge to controller-runtime for use cases that need it. They are complementary, not competitive.

**A general-purpose policy engine.** Orkestra's validation and mutation are scoped to the CRDs it manages. Global cluster-wide policy belongs in OPA, Kyverno, or VAP.

---

## Contributing

The highest-value contributions right now:

| Area | What helps most |
|---|---|
| **Production deployments** | Run Orkestra on real workloads, report what breaks |
| **Registry patterns** | Five-file patterns for common CRDs — postgres, redis, cert-manager |
| **Testing at scale** | 50+ CRD deployments, stress test results |
| **Documentation** | Edge cases, gotchas, things that weren't obvious |
| **Hooks** | Real-world hook implementations for complex operators |

Open a [GitHub issue](https://github.com/orkspace/orkestra/issues) or
[Discussion](https://github.com/orkspace/orkestra/discussions) for anything not covered above.

---

[Start here →](./getting-started/index.md)
