<div align="center">
  <img src="./documentation/assets/logo.png" alt="Orkestra" height="96" />

  <h1>Orkestra</h1>
  <p><strong>Kubernetes operators without the infrastructure.</strong></p>
  <p>
    Reconciliation as a runtime service.<br/>
    Security as a runtime service.<br/>
    Intent Delivery as a runtime service.
  </p>

  <p>
    <a href="https://github.com/orkspace/orkestra/releases"><img src="https://img.shields.io/github/v/release/orkspace/orkestra" alt="Release" /></a>
    <a href="https://artifacthub.io/packages/search?repo=orkestra"><img src="https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/orkestra" alt="Artifact Hub" /></a>
    <img src="https://img.shields.io/badge/Go-1.26+-00ADD8.svg" alt="Go" />
    <img src="https://img.shields.io/badge/Kubernetes-1.28+-326CE5.svg" alt="Kubernetes" />
    <img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License" />
  </p>

  <p>
    <a href="https://orkestra.sh/docs/getting-started">Quick Start</a> ·
    <a href="https://orkestra.sh">Docs</a> ·
    <a href="https://github.com/orkspace/orkestra/discussions">Discussions</a> ·
    <a href="https://join.slack.com/t/orkspace-group/shared_invite/zt-42i4idb0h-WYUF6JryDFMkky95ZJWHBg">Early Access Slack</a>
  </p>
</div>

---

Every Kubernetes operator carries three kinds of infrastructure no one wanted to build:

- **Reconciliation infrastructure** — informers, workqueues, worker pools, leader election, retries, backoff, finalizers, status patching, panic recovery
- **Security infrastructure** — admission webhooks, validation rules, mutation rules, RBAC generation, TLS management
- **Intent delivery infrastructure** — CR construction, caller interfaces, field routing, value translation, schema evolution

None of this is the reason the operator exists. All of it is the cost of entry.

Orkestra absorbs all three. You declare behavior — or keep your existing `Reconcile` function — and the runtime handles the rest.

---

## If you already have a controller-runtime operator

Two lines. Your `Reconcile` method is completely untouched.

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

Remove `SetupWithManager`, `Scheme`, and `main.go`. Orkestra provides the informer, workqueue, worker pool, leader election, panic recovery, metrics, retries, health endpoints, and admission webhooks. Or run `ork migrate` to have the constructor injected automatically:

```bash
ork migrate ./controller/webapp_controller.go -o ./my-operator
```

→ [Migration Guide](https://orkestra.sh/docs/guides/migration) · [ork migrate reference](https://orkestra.sh/docs/reference/cli/migrate)

---

## If you are starting from scratch

No Go required. Declare what the operator should do:

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: Katalog
metadata:
  name: website-operator
spec:
  crds:
    website:
      crdFile: ./crd.yaml
      crFiles: [./cr.yaml]
      operatorBox:
        onCreate:
          deployments:
            - name: "{{ .metadata.name }}"
              image: "{{ .spec.image }}"
              replicas: "{{ .spec.replicas }}"
              reconcile: true
          services:
            - name: "{{ .metadata.name }}-svc"
              port: 80
              targetPort: "{{ .spec.port }}"
              reconcile: true
```

```bash
ork run
```

Orkestra reads the Katalog, installs the CRD, starts the operator, creates the Deployment and Service, sets owner references, writes status, emits events, corrects drift, and exposes health, metrics, and a control center.

Not a single line of Go.

---

## For the security and delivery problem

The same Katalog that declares an operator also declares who can reach it and how. `serve.enabled: true` on a CRD entry surfaces it through the Gateway API — token-scoped, admission-enforced, with field translation and provenance built in. Callers post flat fields in their own vocabulary; the gateway validates, annotates, and applies. The caller never sees a CRD schema or a Kubernetes object.

```yaml
crds:
  application:
    serve:
      enabled: true
      target: app
      fields:
        image:
          label: "Container Image"
          required: true
        environment:
          label: "Environment"
          enum: ["staging", "production"]
```

```bash
curl -X POST http://localhost:8443/api/v1/apply \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"target":"app","name":"payments","image":"...","environment":"staging"}'
```

The gateway owns the delivery boundary — validation, mutation, token scoping, provenance stamping, field routing, CR construction. Everything after the CR lands in etcd belongs to the operator. It works with any reconciler: Orkestra runtime, Argo CD, Flux, Crossplane, or a plain controller.

→ [Self-Service and Intent Delivery](https://orkestra.sh/docs/concepts/self-service/gateway-as-delivery-layer/)

---

## What every CRD gets

Every CRD declared in a Katalog becomes a complete, isolated operator. Nothing to configure.

| | |
|---|---|
| **Informer** | Watches your exact GVK. In-memory cache. Zero API calls on read. |
| **Workqueue** | Per-CRD. Rate-limited. Deduplicated. Isolated from every other CRD. |
| **Worker pool** | Configurable concurrency. A panic in one CRD does not affect any other. |
| **Drift correction** | `reconcile: true` — desired state is enforced on every cycle. |
| **Owner references** | Child resources deleted when the CR is deleted. No `onDelete` logic needed. |
| **Finalizers** | CRs protected from dirty deletion automatically. |
| **Events** | Every reconcile is a traceable Kubernetes event. |
| **Leader election** | One active instance. Followers hold warm caches. Failover in under 15s. |
| **Status** | `Ready` condition + your own status fields written after every reconcile. |
| **Health API** | `/katalog/{crd}/health`, `/katalog/{crd}/cr`, `/metrics` — per CRD. |
| **Prometheus metrics** | Reconcile totals, queue depth, error rate — labeled by GVK. |
| **Admission webhooks** | Validation and mutation declared in the Katalog. No webhook server to write or deploy. |
| **RBAC** | `ork generate rbac` derives ClusterRoles from the Katalog. No manual authoring. |
| **Deletion protection** | Orkestra and everything it manages cannot be accidentally `kubectl delete`. |
| **Control Center** | Realtime visibility per CRD, per Katalog, across instances. Auto-generated operator docs — overview, reconcile mode, child resources, kubectl reference, access control. |
| **Developer portal** | `serve.enabled: true` on any CRD surfaces a self-service form in the Control Center. Callers submit intent in their vocabulary — no kubectl, no YAML, no Kubernetes knowledge required. |

---

## Getting started

### Install
```bash
# Install (macOS)
brew install orkspace/tap/ork orkspace/tap/orkcc

# Install (Linux)
curl -sSL https://get.orkestra.sh | bash
```

> **Windows**
> Download `ork_windows_amd64.zip` and `orkcc_windows_amd64.zip` from the
> [latest release](https://github.com/orkspace/orkestra/releases).  
> Extract the archives and add the folder containing `ork.exe` and `orkcc.exe` to your `PATH`.

### Initialize and run
```bash
ork init
ork run
```

> No cluster? Add `--dev` to create a temporary kind cluster. Requires Docker.

`ork init` scaffolds a `katalog.yaml`, `crd.yaml`, and `cr.yaml` in the current directory.

**→ [Learning to Orkestrate](https://orkestra.sh/docs/getting-started/learning-to-orkestrate)** — the guided path from first operator to full platform. Every capability has a runnable example.

---

### Control Center

```bash
ork control
```
> → localhost:8081 · username:password → orkestra

![Control Center — multi-Runtime view](./documentation/assets/controlcenter/public/control-center.png)

![Control Center — per-Runtime panel](./documentation/assets/controlcenter/public/control-panel.png)

![Control Center — auto-generated operator docs](./documentation/assets/controlcenter/public/operator-docs.png)

Six Runtimes. 75 CRDs. One Control Center.

> Live deployment: [cc.orkestra.sh](https://cc.orkestra.sh)

---

## Numbers

| | Traditional (75 operators) | Orkestra |
|---|---|---|
| **Processes** | 75 | 6 runtimes + 1 control center |
| **Memory** | 3.75 GB – 15 GB | ~79 MB per runtime (measured) |
| **CRDs under management** | 75 | 75 |
| **First operator** | 3–6 weeks | Under 1 hour |
| **Lines of Go** | 400+ per operator | 0 |
| **Adding a new CRD** | Days to weeks | Minutes |

79 MB is a live measurement from a 10-CRD runtime (`process_resident_memory_bytes` from the `/metrics` endpoint — [raw scrape](./documentation/assets/controlcenter/public/metrics.txt)). The reduction works because Orkestra pays the cost of client-go, leader election, and health servers once per runtime. Per-CRD cost is a goroutine pool and an in-memory cache — the same isolation model as `kube-controller-manager`. A panic in one CRD is caught by `safeReconcile`; the others keep running.

---

## What Orkestra is not

**Not an operator framework — an operator runtime.** A framework gives you libraries and conventions. Orkestra gives you a runtime: the reconciliation loop, security layer, and delivery surface are the runtime's job. You write the behavior.

**Not a replacement for Go.** Hooks and constructors exist for exactly this reason. ~90% of operators are declarative; ~10% need code. Orkestra handles the 90% and gives the 10% a clean seam — the same informer, queue, health, and metrics infrastructure, with a single function to implement.

**Not GitOps.** Katalogs define long-lived API contracts resolved at startup. Treat Katalog changes like any other runtime change — deploy through a pipeline.

**Not a product — a primitive layer.** Notes, autoscaler, serve mode, Katalogs — none of these are products. They are primitives ready for composition.

---

## Documentation

| | |
|---|---|
| [Migration Guide](https://orkestra.sh/docs/guides/migration) | Bring an existing controller-runtime operator into Orkestra — zero changes to your reconciler |
| [Why Orkestra](https://orkestra.sh/blog/why-orkestra) | What Orkestra is, how it works, and why it's different |
| [Foundations](https://orkestra.sh/docs/foundations) | The decisions that shaped the design — and why they hold |
| [Trust and Failure Model](https://orkestra.sh/publications/trust-and-failure-model) | What happens when things go wrong |
| [Self-Service and Intent Delivery](https://orkestra.sh/docs/concepts/self-service/gateway-as-delivery-layer/) | The gateway as a delivery surface — security, field routing, and provenance without changing the operator |
| [Getting Started](https://orkestra.sh/docs/getting-started) | First operator in under an hour |
| [Learning to Orkestrate](https://orkestra.sh/docs/getting-started/learning-to-orkestrate) | Every capability, as a runnable example |
| [Katalog Reference](https://orkestra.sh/docs/reference/schema/katalog/) | Complete field reference |
| [Orkestra Registry](https://orkestra.sh/docs/orkestra-registry/) | OCI distribution for operators |
| [Security](https://orkestra.sh/docs/security/) | How Orkestra is secure by default |

---

## Community

[Issues](https://github.com/orkspace/orkestra/issues) · [Discussions](https://github.com/orkspace/orkestra/discussions) · [Contributing](./CONTRIBUTING.md)

---

Apache 2.0 — see [LICENSE](./LICENSE)
