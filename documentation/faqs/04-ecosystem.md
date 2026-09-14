# Ecosystem

## How does Orkestra compare to ArgoCD?

ArgoCD does manage drift. That's a fair starting point.

The real question is what you do when you need an operator — when drift correction of static manifests is not enough, when you need conditional resources, live state reactions, external API calls, cross-operator dependencies.

At that point you drop into Go. And every team that does this builds the same machinery, from scratch, for every operator: informers, workqueues, leader election, worker pools, health endpoints, Prometheus metrics, retry logic, finalizers, status management, safe panic recovery, multi-version CRD handling, admission and deletion protection.

**That machinery is not the reason the operator exists. It is the cost of entry.**

Orkestra says: the machinery is the runtime's job. You focus on the reason the operator exists — your custom logic.

| | Helm + ArgoCD | Orkestra |
|---|---|---|
| **Drift correction** | Yes — reconciles declared state | Yes — `reconcile: true` on any resource |
| **Custom CR logic** | No | Yes — `when:`, `external:`, `status:`, hooks |
| **Live state reactions** | No | Yes — reconciler re-runs on every change event |
| **External API calls** | No | Yes — `external:` block, no client code |
| **Admission control** | No | Yes — validation and mutation webhooks |
| **Multi-version CRDs** | No | Yes — declarative conversion, no webhook code |
| **Per-operator autoscaling** | No | Yes — workers, queue depth, resync period |
| **Operator distribution** | Helm chart | OCI Katalog |

Helm + ArgoCD is the right tool for managing static platform resources. Orkestra is the runtime for teams writing operators — so they write the part that matters and stop rebuilding the machinery every time.

---

## How does Orkestra compare to Kubebuilder and Operator SDK?

Both Kubebuilder and Operator SDK are scaffolding frameworks. They generate the controller skeleton — informer setup, event handlers, reconcile loop stub — and reduce the boilerplate you write. You fill in the logic.

You still own the machinery. Queue depth, worker count, leader election lease, health endpoints, Prometheus metrics, retry logic, panic recovery — all of it is yours to configure, test, and maintain. Kubebuilder scaffolds it. You own it.

**Orkestra is a runtime. You do not write controllers. The machinery is the runtime's job.**

| | Kubebuilder / Operator SDK | Orkestra |
|---|---|---|
| **Language** | Go | YAML (Go hooks optional) |
| **Controller code** | Required — you fill in the reconciler | Not required — runtime executes your Katalog |
| **Informer / queue / workers** | Yours to configure | Runtime-managed, per-CRD |
| **Admission webhooks** | Write and deploy separately | Declared in Katalog, served by Gateway |
| **Metrics** | Instrument yourself | Built-in per-CRD — queue depth, error rate, worker utilisation |
| **Operator distribution** | Binary / Docker image | OCI Katalog |
| **Testing** | Unit tests + envtest | `ork simulate` (in-memory) + `ork e2e` (real cluster) |

The escape hatch: when the declarative layer genuinely is not enough — writing a row into PostgreSQL, calling a non-HTTP API — typed hooks let you drop back to Go without giving up the declarative layer. You write the part that requires code. The runtime handles the rest.

---

## How does Orkestra compare to kro?

kro (Kubernetes Resource Orchestrator) was announced in 2024 by Google, Microsoft,
and AWS. It allows declaring `ResourceGraphDefinitions` that compose Kubernetes
resources declaratively. The core insight — operator behavior should be a declaration
— is the same insight Orkestra is built on.

The differences are significant:

| | kro | Orkestra |
|---|---|---|
| **Per-CRD isolation** | No — shared reconcile context | Yes — dedicated informer, queue, workers |
| **Multi-version CRDs** | No | Yes — declarative conversion paths |
| **Registry/distribution** | No | Yes — OCI artifacts, Artifact Hub |
| **Admission webhooks** | No | Yes — validation and mutation |
| **Observability** | No | Yes — per-CRD health endpoints, Prometheus, Control Center |
| **Hooks for external logic** | No | Yes — typed and dynamic Go hooks |

kro is a composability layer. Orkestra is a runtime. The fact that three major cloud
providers independently arrived at the same insight validates the direction — Orkestra
extends it into a complete operator runtime.

---

## Can Orkestra manage third-party CRDs?

Yes — any CRD that Kubernetes accepts, Orkestra can watch and reconcile. No fork, no
reverse engineering, no changes to the CRD definition needed.

```yaml
spec:
  crds:
    prometheus:
      apiTypes:
        group: monitoring.coreos.com
        version: v1
        kind: Prometheus
        plural: prometheuses
      operatorBox:
        onCreate:
          # governance, companion resources, defaults
```

This is how governance patterns work — you apply Orkestra's validation and mutation
model to CRDs you did not write and cannot modify.

For the full wrapping pattern — internal CRD → ecosystem resource, admission enforcement, status propagation, and e2e testing against the real tool — see the [Ecosystem Composition Guide](../guides/ecosystem/index.md).

---

## What is the path to Kubernetes core?

See [Declarative Operators: A New Model for Kubernetes Extensibility](/publications/declarative-operators-whitepaper/)
for the full argument.

The short version: Orkestra is building toward CNCF Sandbox, then a Kubernetes
Enhancement Proposal, then alpha/beta/GA integration into `kube-controller-manager`.
The prerequisite is production adoption at multiple organisations, with metrics. The
timeline depends on that adoption curve.

The Katalog and Komposer becoming native Kubernetes kinds — `kubectl get katalogs` —
is the end state. At that point, every cluster ships with a meta-controller that
understands declarative operator definitions. Platform teams write Katalogs. Kubernetes
manages them.


## I already have a controller-runtime operator. Where do I start?

Pull the migration pack and look at `04-constructor-migration`:

```bash
ork init --pack from-controller-runtime
cd from-controller-runtime/04-constructor-migration
```

Your `Reconcile` method stays completely unchanged — same signature, same body. Two lines in a constructor wire it into Orkestra:

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

Remove `SetupWithManager`, `Scheme`, and `main.go`. Orkestra provides the informer, workqueue, worker pool, leader election, panic recovery, and metrics.

Or run `ork migrate` to have the constructor injected automatically — see below.

If you want to understand the full range of options first, the [Migration Guide](../guides/migration/index.md) walks through each mode from zero-Go declarative to full constructor.

---

## What does `ork migrate` do?

`ork migrate` takes your existing controller-runtime reconciler file and produces a ready-to-run Orkestra operator. By default (`--mode toclient`) it leaves your `Reconcile` method completely untouched — no signature change, no body change. It removes `SetupWithManager` and injects the constructor:

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

It also writes a `katalog.yaml` stub, `simulate.yaml`, `e2e.yaml`, `go.mod`, `Makefile`, and `Dockerfile` alongside the rewritten file. Search for `TODO(ork migrate)` in the output to see what needs your attention.

```bash
ork migrate ./controller/webapp_controller.go -o ./my-operator
```

For a full rewrite to native Orkestra style (new Reconcile signature, struct, and call sites), use `--mode native`.

→ [ork migrate reference](../reference/cli/migrate.md) · [Migration Guide](../guides/migration/index.md)

---

## I want to try Orkestra but I don't have a cluster. What do I need?

Docker. That is all.

Two ways to get started:

```bash
# Create the cluster first, then start the runtime
ork create cluster
ork run

# Or in one command — creates the cluster and starts the runtime together
ork run --dev
```

`ork create cluster` provisions a local kind cluster and writes the kubeconfig. `ork run` starts the Orkestra runtime against it. `ork run --dev` does both in a single step — the fastest path from nothing to a running Orkestra operator.

No cloud provider account. No Minikube. No existing Kubernetes setup.

---

## Further reading

- **[Ecosystem Composition Guide](../guides/ecosystem/index.md)** — wrap ArgoCD, cert-manager, Prometheus, and Crossplane with abstraction layers
- **[Migration Guide](../guides/migration/index.md)** — move an existing controller-runtime operator to Orkestra
- **[Roadmap](../roadmap.md)** — what is shipped and what is coming
- **[Getting Started](../getting-started/index.md)** — your first operator in minutes
