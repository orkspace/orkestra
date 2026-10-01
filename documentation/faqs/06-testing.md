# Testing

## How do I test an Orkestra operator?

Two tools. One for fast iteration, one for production confidence:

**`ork simulate`** — runs the reconciler against a fake in-memory cluster. No Kubernetes. No Docker. Sub-second. Declare what your operator should produce per cycle, get a pass or fail. The same reconciler code that runs in production runs here.

**`ork e2e`** — runs the operator against a real kind cluster. Creates the cluster, applies CRDs and the operator, applies your CR, asserts Kubernetes resource state, tears down. Full integration truth.

The pattern: simulate in your inner loop, e2e as your pre-deploy gate.

---

## What is the difference between `ork simulate` and `ork e2e`?

| | `ork simulate` | `ork e2e` |
|---|---|---|
| **Cluster** | No — in-memory | Yes — kind |
| **Speed** | Sub-second | 2–5 min |
| **What it asserts** | Reconciler ops by cycle | Kubernetes resource state (ready, count, fields) |
| **When to use** | Every change, CI, development loop | Pre-deploy verification, integration truth |

They are not alternatives. Simulate is your inner loop. E2E is your pre-deploy gate. Use both.

---

## How do I generate a `simulate.yaml`?

`ork simulate init` runs the reconciler once, captures the cycle-1 create operations, and writes a `simulate.yaml` pre-filled with `expect:` rules:

```bash
ork simulate init           # writes simulate.yaml in the current directory
ork simulate init --dry-run # preview: prints to stdout, nothing written
ork simulate init --force   # overwrite existing simulate.yaml
```

The generated file includes a commented `absent:` block seeded with the first observed resource type — a starting point for failure-path assertions you can fill in.

→ [Scaffolding tests with simulate init](../concepts/simulate/03-init.md)

---

## What does `skipExternal: true` do?

It stubs all `external:` HTTP calls with an empty 200 response instead of hitting the real network. Useful when the external service is not available in your simulate environment and you only care about the structural output — which resources are created — not the content shaped by the external response.

```yaml
spec:
  katalog: ./katalog.yaml
  cr: ./cr.yaml
  skipExternal: true
```

If you need the external calls to return meaningful responses — for example, a feature flag that controls whether a resource is created — use `--dev-server` instead.

---

## What is `ork simulate --dev-server`?

`ork simulate --dev-server` starts the Orkestra mock HTTP server at `localhost:9999` before running simulate. The dev server responds to the same endpoints used in the learning examples:

- `GET /health` — 200 healthy
- `GET /config/:name` — static JSON config blob
- `GET /flags/:name/:flag` — feature flag value
- `GET /status/200`, `GET /status/503` — explicit status codes

This lets examples that call external services (config injection, feature flags, health gates) get realistic responses during simulate — no real network, no cluster required.

`--skip-external` stubs calls with empty 200s. `--dev-server` gives them real responses. Use `--dev-server` when the response content affects what resources your operator creates.

→ [simulate CLI reference](../reference/cli/05-simulate.md)

---

## What does `--debug-ops` do?

`--debug-ops` prints every operation the reconciler performed across every cycle before the assertion output — the full picture of what your operator actually did:

```text
  [debug-ops] 7 total ops recorded across all cycles:
  [debug-ops]   cycle=1   verb=create   resource=deployments         name=my-website
  [debug-ops]   cycle=1   verb=create   resource=services            name=my-website
  [debug-ops]   cycle=2   verb=update   resource=deployments         name=my-website
  [debug-ops]   cycle=3   verb=get      resource=deployments         name=my-website
```

Use it to understand what your operator does before writing assertions. The intended authoring workflow:

```bash
ork simulate --debug-ops     # see every op across all cycles
ork simulate init            # capture cycle-1 creates as assertions automatically
# edit simulate.yaml         # add absent: blocks, later cycles, edge cases
ork push            # simulate gate runs these assertions before publish
```

`--debug-ops` works in all three invocation modes: direct flags, a `simulate.yaml` file, and `./...` discovery.

---

## Can I run simulate for multiple operators at once?

Yes — with an aggregator. A `simulate.yaml` with `imports:` and no `spec:` runs each imported file independently:

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: Simulate
metadata:
  name: platform-sim

imports:
  - ./database/simulate.yaml
  - ./cache/simulate.yaml
  - ./api/simulate.yaml
```

Each file runs independently. The suite fails if any file fails.

To run all simulate files in a directory tree:

```bash
ork simulate ./...
```

→ [Aggregator mode](../concepts/simulate/index.md)

---

## Does simulate run the real reconciler?

Yes. `ork simulate` uses the same reconciler pipeline that runs against a live cluster — the same template evaluation, the same `when:` conditions, the same `external:` calls, the same hooks. The only difference is the backing store: instead of etcd and the Kubernetes API, simulate uses an in-memory fake client.

If a test passes in simulate, the reconciler logic is correct. What you cannot verify in simulate: admission webhook behavior end-to-end, network policies, actual pod scheduling, and anything that depends on Kubernetes controllers running (e.g. ReplicaSet → Pod creation). That is what `ork e2e` is for.


## Can I use `ork e2e` to test any Kubernetes workload — not just Orkestra operators?

Yes. `ork e2e` does not care what manages the resources it tests. Set `spec.custom.target: kubernetes` and Orkestra skips its own bundle generation and Helm install — the cluster lifecycle, `setup:` blocks, CR apply, assertions, and cleanup all run unchanged. The workload under test can be a Helm chart, a third-party operator, a raw manifest, or anything else that runs in Kubernetes.

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: E2E
metadata:
  name: my-helm-chart-test
spec:
  custom:
    target: kubernetes
  cr: ./cr.yaml
  setup:
    helm:
      - release: my-app
        chart: ./charts/my-app
  expect:
    - resource: deployments
      name: my-app
      namespace: default
      present: true
      ready: true
```

The cluster is created, the Helm chart is installed, the assertions run, and the cluster is torn down — without Orkestra involved in managing the workload itself. You get the full e2e harness against anything.

→ [Testing anything in Kubernetes](../guides/e2e-universal/index.md)

---

## Can I use `ork e2e` to test an operator I didn't write?

Yes — this is the same `custom.target: kubernetes` mode. cert-manager, FluxCD, Crossplane, ArgoCD, your own legacy controller-runtime operator — if it runs in Kubernetes, `ork e2e` can test it. Install the operator in `setup.helm`, apply your CR, assert the resources it should produce.

→ [Testing anything in Kubernetes](../guides/e2e-universal/index.md) · [custom.target reference](../reference/schema/04-e2e/05-custom-target.md)

---

## Further reading

- **[simulate concept docs](../concepts/simulate/index.md)** — how simulate works in depth
- **[E2E concept docs](../concepts/e2e/index.md)** — end-to-end testing
- **[Simulate schema reference](../reference/schema/05-simulate/index.md)** — full field reference
- **[Universal e2e guide](../guides/e2e-universal/index.md)** — test anything that runs in Kubernetes
