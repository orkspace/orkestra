# Concepts

## What is Orkestra?

Orkestra is the Kubernetes operator runtime. It turns CRDs into
fully functional operators without controllers, reconcilers, or conversion code.

You declare what a CRD should do — create a Deployment and a Service, apply
defaults, validate fields, convert between versions. Orkestra runs the operator.
The code you would have written does not exist.

!!! tip "The one-sentence version"
    Every operator framework before Orkestra reduced the code you write.
    Orkestra removes the need to write code at all.

See [Your CRD Is Enough](/blog/your-crd-is-enough/) for the full picture.

---

## Is Orkestra an operator?

Not in the way the term is usually meant.

Orkestra runs in a cluster, watches resources, and reacts to events — so by the loose definition, yes. But that framing misses what it actually is. The closer analogy is `kube-controller-manager`: it runs as a pod, it watches CRDs, but no one calls it an operator. It is the thing that makes controllers run.

Orkestra is the same shape. It does not reconcile your CRD. It produces a complete, isolated operator for your CRD from a Katalog declaration — its own informer, queue, worker pool, health state, metrics. At runtime you have an operator for your CRD. At the source level you have a YAML file, not a controller.

The tell is [cmd/orkestra/main.go](https://github.com/orkspace/orkestra/blob/main/cmd/orkestra/main.go). There is no `Reconcile`, no `ctrl.SetupWithManager`, no scheme registration for your types. Those are not missing — they are generated at startup from the Katalog per CRD.
Traditionally, operators have that boilerplate in the code — the reconciler, the controller setup, the scheme registration. Orkestra puts it in the runtime.


→ [Declarative Operators whitepaper](/publications/declarative-operators-whitepaper/) — the super-operator model in full

---

## Does Orkestra install my CRDs?

No — and that is the point.

Orkestra is built on the premise that your CRD already has everything needed to manage it. It does not need another CRD to manage it. You bring the CRD. Orkestra turns it into an operator.

Install your CRD the same way you always have — `kubectl apply`, Helm, GitOps. Once it exists in the cluster, point a Katalog at it and Orkestra starts managing it.

→ [Why Katalog and Komposer are not CRDs](./05-why-not-crds.md) — the full reasoning, and what it means for your cluster

---

## Do I need to write Go code?

No — for the common case.

Orkestra provides these capabilities declaratively, with no Go:

- Informers watching your exact GVK and version
- Workqueue with configurable depth, backoff, and rate limiting
- Worker pool with configurable concurrency
- Drift correction (`reconcile: true` on any template resource)
- Owner references and cascade deletion
- Kubernetes event emission
- Leader election
- Health endpoints and Prometheus metrics
- Multi-version CRD conversion
- Admission-time validation and mutation

Go hooks are available when you need them — complex external API calls not covered by the `external:` block, complex
conditional logic not covered by the `when:` and `or:` blocks. But hooks are additive. The
declarative layer handles everything else.

If the reconcile logic already lives in a running service — or if Go is not the language you want — `reconcile.remote` is a third path: point Orkestra at an HTTP endpoint in any language. The service receives the CR as JSON and returns resources and status. No Go. No YAML templates. No Kubernetes SDK. See [Remote Reconciler](../concepts/reconciler-model/09-remote-reconciler.md).

!!! note "When Go becomes necessary"
    The 10-20% of operator logic that genuinely requires code — creating a user
    inside PostgreSQL, reading another cluster's state
    — is handled by hooks. Hooks coexist with declarative templates. You do not
    choose one or the other.

---

## How does Orkestra differ from Helm or Kustomize?

Different category entirely.

| | Helm | Kustomize | Orkestra |
|---|---|---|---|
| **What it does** | Renders templates once | Patches manifests once | Runs a continuous operator loop |
| **When it runs** | At deploy time | At deploy time | Continuously, while the cluster runs |
| **Drift correction** | No | No | Yes — corrects on every reconcile cycle |
| **Watches CRs** | No | No | Yes — every change event triggers reconcile |
| **Versioning** | Chart versions | Kustomization | Per-CRD operator stacks, declarative conversion |
| **Dependencies** | Chart dependencies | Kustomization bases | `dependsOn` ordering with ready signals |

Orkestra is an operator runtime. Helm and Kustomize are deployment tools. They
solve adjacent problems and compose naturally — you can use a Helm chart as a
Katalog source in a Komposer.

---

## What is a Katalog?

A Katalog is a YAML document that declares how Orkestra should manage one or more
CRDs. It is not a Kubernetes CRD itself — it is a file.

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: Katalog
metadata:
  name: website-operator

spec:
  crds:
    website:
      apiTypes:
        group: demo.orkestra.io
        version: v1alpha1
        kind: Website
        plural: websites
      operatorBox:
        reconcile:
          onCreate:
            deployments:
              - image: "{{ .spec.image }}"
                replicas: "{{ .spec.replicas }}"
                reconcile: true
```

!!! note "Why Katalog is not a CRD"
    Orkestra deliberately keeps Katalog and Komposer as plain YAML files, not
    Kubernetes CRDs. See [Why Katalog and Komposer Are Not CRDs](./05-why-not-crds.md)
    for the full reasoning.

See the [Katalog Schema](../reference/schema/02-katalog/01-top-level.md) for all available fields.

---

## What is a Komposer?

A Komposer composes multiple Katalogs from different sources into one unified
runtime configuration.

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: Komposer
metadata:
  name: platform-komposer

imports:
  registry:
    - url: ghcr.io/orkspace/orkestra-registry/postgres@v14
      oci: true
  files:
    - ./katalogs/website.yaml
  helm:
    - repo: https://charts.myorg.io
      chart: platform-crds
      version: 2.1.0

spec:
  crds:
    postgres:
      operatorBox:
        reconcile:
          workers: 8      # override for production
```

The `spec.crds` inline block always wins on name conflict — it is the override
mechanism. Platform teams publish Katalogs; application teams compose and override.

See the [Komposer Schema](../reference/schema/03-komposer/index.md) for all options.

---

## Are Katalog, Komposer, Motif, E2E, and Simulate Kubernetes CRDs?

No. None of them are registered as `CustomResourceDefinition` objects in the cluster.

They are plain YAML files loaded from the local filesystem or OCI registry at startup. A `Katalog` struct in Go has no `metav1.TypeMeta`, no `metav1.ObjectMeta`, no `DeepCopyObject()`. The Katalog loader calls `os.ReadFile()` and `yaml.Unmarshal()` — no Kubernetes client, no API server call.

The same applies to Komposer, Motif, E2E, and Simulate. They are instruction sets for the runtime. They live on disk and in OCI artifacts, not in etcd.

!!! note "The Artifact Hub display metadata"
    `charts/orkestra/Chart.yaml` lists these types under `artifacthub.io/crds` — that is Artifact Hub display metadata only. The Helm chart deploys no CRD YAML for any of them.

→ [Why Katalog and Komposer are not CRDs](./05-why-not-crds.md) — the full design reasoning

---

## What is a Motif?

A Motif is a reusable operatorBox fragment. It packages everything one pattern of operator behavior needs — resources, status fields, validation rules, mutation rules — under a named, parameterized declaration. Katalogs import Motifs instead of repeating those declarations.

A Motif can declare:

- **`resources:`** — Kubernetes and custom resources to create (`deployments`, `statefulSets`, `services`, `custom`, and more), split into `onCreate` and `onReconcile` phases
- **`status:`** — status fields merged into the importing CRD's status layer
- **`admission:`** — validation and mutation rules contributed alongside the Katalog's own rules

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: Motif
metadata:
  name: postgres
  version: v1

inputs:
  - name: image
    required: true
  - name: volumeSize
    default: "10Gi"

resources:
  statefulSets:
    - name: "{{ .metadata.name }}-db"
      image: "{{ index .inputs \"image\" }}"
      storageSize: "{{ index .inputs \"volumeSize\" }}"

status:
  fields:
    - path: dbEndpoint
      value: "{{ .metadata.name }}-db.{{ .metadata.namespace }}.svc.cluster.local"

admission:
  validation:
    rules:
      - field: spec.image
        operator: exists
        message: "spec.image is required"
        action: deny
```

A Katalog imports it with `with:` bindings:

```yaml
operatorBox:
  reconcile:
    imports:
      - motif: postgres
        with:
          image: "{{ .spec.dbImage }}"
          volumeSize: "{{ .spec.storage }}"
```

Orkestra expands the Motif at Katalog load time — bindings resolved, resources and rules merged into the operatorBox as if declared inline.

Publish a Motif by pushing its directory to the registry:

```bash
ork push postgres:v1 ./motifs/postgres/
```

Import it in a Katalog by OCI address, or by bare name if `ORK_MOTIFS_REGISTRY` is set:

```yaml
operatorBox:
  reconcile:
    imports:
      - motif: ghcr.io/myorg/motifs/postgres:v1   # full OCI ref
      - motif: postgres                            # bare name — resolved via ORK_MOTIFS_REGISTRY
```

Motifs can share the same registry address as Katalogs — separate them with folders (`/katalogs/`, `/motifs/`) rather than separate registries.

→ [Motif schema reference](../reference/schema/01-motif/index.md)

---

## What is the note expression language?

Notes are the pure transformation functions available inside every `{{ }}` expression in a Katalog — `onCreate`, `onReconcile`, `status.fields`, `when:` conditions, `normalize:`, `mutation:`, `conversion.paths:`.

```yaml
status:
  fields:
    - path: phase
      value: '{{ boolTernary .spec.suspend "Suspended" "Active" }}'
    - path: endpoint
      value: "{{ .metadata.name }}.{{ .metadata.namespace }}.svc.cluster.local"

onCreate:
  secrets:
    - name: "{{ .metadata.name }}-creds"
      once: true
      data:
        password: "{{ randomAlphanumeric 32 }}"
```

Notes are **pure** (same input → same output), **safe** (nil/empty input never panics), and **stateless** (no I/O, no external calls). They are not Go `text/template` — they are a separate vocabulary built on top of it.

There are over 100 notes across domains: strings, math, conditionals, type coercion, cron expressions, random generation, Kubernetes object navigation, replica state, Job lifecycle, Service networking, and more.

Discover them from the terminal:

```bash
ork notes                      # list all notes
ork notes search job           # search by keyword
ork notes show jobSucceeded    # full detail and example
```

→ [Notes reference](../reference/orkestra-notes/index.md)

---

## What is the Orkestra Registry?

The Orkestra Registry is the distribution layer for operator patterns. Where traditional
ecosystems distribute binaries, the registry distributes **behavior** — Katalogs, Motifs,
and Komposers published as OCI artifacts that any Orkestra runtime can pull and interpret.

Pull a Postgres operator pattern with one line in a Komposer. No binary. No deployment.
Just a Katalog.

The default registry is `ghcr.io/orkspace/orkestra-registry`. Point Orkestra at your own
registry — for internal patterns, air-gapped environments, or private Motif libraries:

```bash
ORK_REGISTRY=ghcr.io/myorg/katalogs         # Katalog registry
ORK_MOTIFS_REGISTRY=ghcr.io/myorg/motifs    # Motif registry
```

!!! tip "The npm analogy"
    The Orkestra Registry is Orkestra's package manager for operator behavior.
    Patterns are versioned, composable, and overridable. You import them like
    dependencies, not like binaries.

---

## What is the super-operator model?

The super-operator model is the principle that each CRD gets a complete, isolated
operator stack while sharing the runtime infrastructure.

In traditional frameworks, one-operator-per-CRD means one binary, one deployment,
one informer factory, one leader election lease per CRD. The isolation is at the
process level — expensive.

In Orkestra, one-operator-per-CRD means one informer, one queue, one worker pool,
one reconciler per CRD — all inside a single process. The isolation is at the logic
level. The runtime infrastructure (API server connection, informer factory, health
server, leader election) is shared.

This gives you the isolation guarantee of the one-operator-per-CRD principle at a
fraction of the resource cost.

!!! note "The kube-controller-manager analogy"
    This is exactly how `kube-controller-manager` works. It runs the Deployment
    controller, the ReplicaSet controller, the Job controller, and dozens of others
    in one process. Each controller is isolated — they share only the infrastructure.
    Orkestra applies this proven model to custom resources.

---

## Does Orkestra support multi-version CRDs?

Yes — with zero conversion code.

Each CRD version is a separate entry in the Katalog with its own complete operator
stack. Each entry's informer watches its specific GVK — the API server converts
objects to the requested version before delivering them. Conversion rules are
declared alongside reconcile templates:

```yaml
- name: website-v1
  admission:
    conversion:
      storageVersion: v1
      paths:
        - from: v1alpha1
          to: v1
          spec:
            image: "{{ .spec.image }}"
            seo:
              enabled: false   # v1alpha1 has no seo field — supply default
```

**Production results:** 62 conversions, 0 failures, sub-millisecond average latency.

!!! note "No separate webhook deployment"
    Conversion runs on Orkestra Gateway — the same server that serves
    `/validate` and `/mutate`. No separate conversion webhook binary. No separate
    TLS certificate. No separate deployment.

---

## What is the OPPRE execution model?

OPPRE is the name for the execution model Orkestra implements:
**O**bserve, **P**re-reconcile, **P**repare, **R**econcile, **E**mit.

Each phase is owned by a distinct layer:

| Phase | Layer | Responsibility |
|---|---|---|
| Observe | Informer | Watch CRDs, secondary resources, and Kubernetes Events; produce reconcile triggers |
| Pre-reconcile | Kordinator | Gate events before the queue (enqueueGate) and before the reconciler (reconcileGate) |
| Prepare | Kordinator | Validate, mutate and enrich the CR with resolver context, profiles, notes, intent, and cross-references |
| Reconcile | Reconciler | Receive a fully-prepared request and reconcile exactly that |
| Emit | Post-reconcile | Write status, emit events, stamp health and metrics onto the CR |

The `operatorBox` schema follows this order: `observe → preReconcile → runtime → reconcile → emit`.

One mental model applies everywhere conditions, gates, and outputs are declared.

See [Execution Model](../concepts/execution-model/index.md) for the full picture.

---

## Further reading

- **[Running](./02-running.md)** — setup, configuration, and operations
- **[Usage](./03-usage.md)** — validation, mutation, built-in kinds
- **[Ecosystem](./04-ecosystem.md)** — comparisons and the Kubernetes roadmap
