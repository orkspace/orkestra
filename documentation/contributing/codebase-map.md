# Codebase Map

Orkestra compiles into three binaries. Every package in `pkg/` and every command in `cmd/` belongs to one of them — or is shared.

---

## Binaries

### Runtime (`cmd/orkestra`)

The reconciliation engine. Watches Kubernetes resources, runs operatorBox logic, dispatches webhooks, manages CRD lifecycle. Built with `-tags runtime`.

**Core packages:**

| Package | What it does |
|---------|-------------|
| `pkg/katalog` | Loads, merges, and validates the Katalog. The link between YAML config and every runtime decision. |
| `pkg/runtime/reconciler` | `Generic Reconciler` — the reconcile loop, notification dispatch. |
| `pkg/runtime/kordinator` | Orchestrates reconcilers per CRD; manages CRD health, degradation, and dependency ordering. |
| `pkg/children` | Fetches and enriches child resources (`_pods`, `_replicaSets`, `_owner`, etc.) and builds the `.children` map available in status templates. |
| `pkg/runtime/informer` | Shared index informers and factory lifecycle. |
| `pkg/kubeclient` | Core, dynamic, and apiextensions clients; REST mapper. |
| `pkg/resources` | Built-in resource handlers: deployments, services, configMaps, jobs, etc. |
| `pkg/merger` | Komposer — multi-source Katalog merging. |
| `pkg/registry/motif` | Motif expansion — assembles reusable resource building blocks at load time. |
| `pkg/webhook` | Admission and conversion webhook server. |
| `pkg/certmanager` | TLS certificate provisioning for webhook servers. |
| `pkg/runtime/reconciler` | Generic reconciler including typed and dynamic modes. |
| `pkg/tools/generate` | Code generation for `ork generate registry` and related commands. |
| `pkg/typeregistry` | Generated stub — blank-imported by user `main.go` to wire typed extensions. |

### Gateway (`cmd/gateway`)

An HTTPS sidecar that owns all webhook endpoints (admission, conversion, deletion-protection) and receives notification dispatch from the runtime. Runs as a separate pod.

**Key packages:**

| Package | What it does |
|---------|-------------|
| `pkg/runtime/kordinator` | `BuildNotifyHandler`, `BuildGatewayKatalogHandler` — HTTP handlers served by the gateway. |
| `pkg/webhook` | Shared webhook parsing and dispatch logic. |
| `pkg/notification` | `DirectNotifier` — SMTP and Slack dispatch, used by the gateway's `/notify` handler. |

### Control Center (`cmd/controlcenter`)

A read-only web UI that aggregates status from one or more running runtimes. Written in Go with embedded HTML templates. No Kubernetes access of its own — pulls everything via the runtime's `/katalog` HTTP API.

---

## Shared packages

These are imported by more than one binary.

| Package | Notes |
|---------|-------|
| `pkg/types` | All Katalog YAML structs, registries, and generated type interfaces. |
| `pkg/konfig` | Environment variable parsing and startup configuration. |
| `pkg/logger` | Structured zerolog wrapper. |
| `pkg/utils` | Small helpers (cluster detection, env expansion, exit). |
| `pkg/labels` | Orkestra label keys and helpers. |
| `pkg/health` | Health and degradation tracking for CRDs. |
| `pkg/runtime/queue` | Per-CRD work queue with backoff and rate limiting. |
| `pkg/event` | Kubernetes event recorder. |
| `pkg/metrics` | Prometheus metrics stubs. |
| `pkg/tools/plan` | Plan/diff logic for operatorBox reconciliation. *(in development)* |
| `pkg/registry` | Runtime-level type and hook registries. |
| `pkg/registry/simulate` | Test harness for reconciler unit tests. |
| `pkg/note` | Template note functions — Go helpers exposed as template variables so operators can surface replica counts, pod health, scaling state, and more in status fields without writing code. Every new note makes Orkestra more declarative. |
| `pkg/notification` | Notification stack, throttle state, `DirectNotifier`, `GatewayNotifier`. |

---

## Reading the code

Every package with a `README.md` explains what it does, what it owns, and what it does not own. Start there. Packages without a README are small enough to read directly.

The `pkg/katalog` package is the connective tissue of the entire project. If you are unsure how a feature works end-to-end, start from the Katalog accessor for that feature and follow callers inward.

---

## CLI commands (`cmd/cli`)

The `ork` CLI is built with a `!runtime` build tag so it is excluded from the runtime binary. Commands live in `cmd/cli/`:

- `ork run` — start the runtime
- `ork generate registry` — emit `pkg/typeregistry/zz_generated_typeregistry.go`
- `ork generate bundle` — emit Kubernetes manifests for the runtime or gateway
- `ork init` — scaffold a new operator project from an example pack
- `ork validate` — validate a Katalog offline
- `ork simulate` — dry-run a reconcile loop without a cluster
- `ork control` — manage a running runtime (start, stop, reload)
