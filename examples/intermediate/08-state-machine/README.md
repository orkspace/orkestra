# Declarative State Machine — Pipeline

**A multi-step pipeline operator. No Go. No constructor. No hooks.**

The Kubebuilder documentation addresses state machines directly. It describes
them as "one of the more complex patterns in Kubernetes operator development"
and provides a Go constructor as the only answer. Orkestra's example 10 showed
the same pattern in Go — 200 lines, a typed struct, finalizer management,
phase dispatch, Job creation, status patching, event emission.

This example replaces all of it with 60 lines of YAML.

---

## What a state machine operator does

A pipeline drives through defined phases in sequence. Each phase does one
thing and writes its result to status. The next reconcile reads that result
and decides what to do next. The progression is automatic — the operator
never "remembers" what it did, it only reads what is true right now.

Each arrow is one reconcile cycle. The queue fires on resync (every 10s by
default) and on watch events (Job completion triggers immediately). The
operator does not poll — it responds to state changes.

---

## How it was done before

The Go constructor from
[`examples/advanced/10-constructor/reconciler/pipeline_reconciler.go`](https://github.com/orkspace/orkestra/blob/main/examples/advanced/10-constructor/reconciler/pipeline_reconciler.go)
implements the same pipeline.

The full file is over 200 lines. It handles: cache reads, finalizer management,
owner references, status patching, event emission, phase dispatch, Job
creation, completion detection via Job status conditions, and advancing
to the next step or terminal state.

Every change to the state machine — a new step, a different terminal
condition, a changed phase name — requires editing Go, rebuilding the
binary, pushing a Docker image, and rolling the deployment.

---

## How it is done now

The same pipeline, declared in [`katalog.yaml`](katalog.yaml). Open it alongside this README.

No Go. No binary build. No deployment cycle. A new step is one more
`jobs:` entry and two more `status.fields:` entries. Readable by anyone
on the team. Reviewable in a pull request without understanding Go.

---

## Reusable fragments

[`katalog.yaml`](katalog.yaml) declares everything inline. As the state machine grows — more phases, more job types, more status fields — the single file grows with it. Two patterns split it into named fragments:

[**Motifs**](https://orkestra.sh/docs/orkestra-registry/motifs/) — reusable operator fragments that can also be distributed as OCI artifacts and shared across katalogs:

- [`motif/admission.yaml`](./motif/admission.yaml) — validation rules
- [`motif/resources.yaml`](./motif/resources.yaml) — job templates
- [`motif/status.yaml`](./motif/status.yaml) — emit.status.fields

[`motif-katalog.yaml`](motif-katalog.yaml) shows the same operator using motif imports.

[**Include**](https://orkestra.sh/docs/concepts/composition/include/) — local file fragments for admission rules and status fields. Same shape as the inline blocks, split into separate files without the Motif wrapper. Job templates are still imported from the shared motif:

- [`include/admission.yaml`](./include/admission.yaml) — validation rules list
- [`include/status.yaml`](./include/status.yaml) — emit.status.fields list

[`include-katalog.yaml`](include-katalog.yaml) shows the same operator mixing `include:` paths with a motif import for resources. Use include when fragments are local to this katalog; use motifs when they need to be shared.

The runtime behaviour of all three is identical.

---

## The two primitives that make this possible

**`operator: notExists` in `when:` conditions**

Detects that a field has not yet been written — specifically, the first
reconcile before any status exists. When `status.phase` is absent from the
informer's cached object, `notExists` passes. After the first reconcile writes
`"Pending"`, `notExists` fails for every subsequent cycle.

**`when:` on `status.fields` entries**

Status fields are not written unconditionally. Each field entry carries an optional `when:` block evaluated against the full CR state — including `.status.*` and `.children.*`. The last field entry whose conditions pass wins.

This override semantics is the state machine. Declare terminal states last — they override any running state when their conditions are met.

---

## What Orkestra still provides

Switching from a constructor to a declarative Katalog does not remove any runtime guarantees:

| | Go Constructor | Declarative Katalog |
|---|---|---|
| Informer watching Pipeline CRD | ✓ | ✓ |
| Workqueue with deduplication | ✓ | ✓ |
| Worker pool (configurable) | ✓ | ✓ |
| safeReconcile panic recovery | ✓ | ✓ |
| Finalizer management | Manual in Go | ✓ Automatic |
| Owner references | Manual in Go | ✓ Automatic |
| Kubernetes events | Manual in Go | ✓ Automatic |
| Status Layer 1 (Ready condition) | Manual in Go | ✓ Automatic |
| Prometheus metrics | Partial (manual wiring) | ✓ Automatic |
| Build required | Yes | No |
| Deployment required | Yes | No |
| Readable by non-Go engineers | No | Yes |

---

## Steps

### 1. Start the runtime

```bash
ork run -f katalog.yaml

# You can swap katalog.yaml for motif-katalog.yaml or include-katalog.yaml —
# all three produce the same operator behaviour.
```

### 2. Apply both CRs

```bash
kubectl apply -f cr.yaml
```

This creates two pipelines: `build-and-test` (succeeds) and
`failing-pipeline` (fails at the build step).

### 3. Watch the state machine

In a separate terminal: `kubectl get pipelines -w`

`failing-pipeline` drives to `Failed` when its build Job exits non-zero.
`build-and-test` completes all three steps in sequence.

### 4. Inspect the Jobs

```bash
kubectl get jobs
```

The test and notify Jobs for `failing-pipeline` are never created — their
`when:` conditions never pass because the build Job never succeeds.

Owner references are set on every Job. When the Pipeline CR is deleted,
all Jobs are cascade-deleted by Kubernetes garbage collection. The operator
wrote no deletion code.

Note: the Ready condition is `True` even for the `Failed` pipeline. Ready
reflects whether the *operator* reconciled successfully — it did. The `phase`
field reflects the *pipeline*'s outcome. These are different concerns and
deliberately separate.

### 5. Check the metrics

```bash
curl localhost:8080/katalog/pipeline
```
### 6. Clean up

```bash
chmod +x cleanup.sh && ./cleanup.sh
```