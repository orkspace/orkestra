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

```
(Empty() ──→ Pending
Pending ──→ create build Job ──→ Running/build
Running/build → build succeeded ──→ create test Job ──→ Running/test
Running/test  → test succeeded  ──→ create notify Job ──→ Running/notify
Running/notify→ notify succeeded ──→ Succeeded

any phase → any Job failed ──→ Failed
```

Each arrow is one reconcile cycle. The queue fires on resync (every 10s by
default) and on watch events (Job completion triggers immediately). The
operator does not poll — it responds to state changes.

---

## How it was done before

The Go constructor from
[`examples/advanced/10-constructor/reconciler/pipeline_reconciler.go`](../advanced/10-constructor/reconciler/pipeline_reconciler.go)
implements the same pipeline. Here is the phase dispatch:

```go
func (r *PipelineReconciler) Reconcile(ctx context.Context, key string) error {
    // ... cache lookup, deep copy, deletion handling ...

    switch pipeline.Status.Phase {
    case "", apiv1.PipelinePhasePending:
        return r.handlePending(ctx, pipeline)
    case apiv1.PipelinePhaseRunning:
        return r.handleRunning(ctx, pipeline)
    case apiv1.PipelinePhaseSucceeded, apiv1.PipelinePhaseFailed:
        return nil
    default:
        return fmt.Errorf("unknown phase %q", pipeline.Status.Phase)
    }
}
```

And the Pending handler that creates the first Job:

```go
func (r *PipelineReconciler) handlePending(ctx context.Context, p *apiv1.Pipeline) error {
    firstStep := p.Spec.Steps[0]
    jobSpec := orkjobs.Resolve(
        orktypes.JobTemplateSource{
            Name:      fmt.Sprintf("%s-%s", p.Name, firstStep.Name),
            Namespace: p.Namespace,
            Image:     p.Spec.Image,
            Command:   firstStep.Command,
        }, p.Name,
    )
    if err := orkjobs.Create(ctx, r.kube, p, jobSpec); err != nil {
        return fmt.Errorf("creating step job %q: %w", firstStep.Name, err)
    }

    now := metav1.NewTime(time.Now())
    p.Status.Phase = apiv1.PipelinePhaseRunning
    p.Status.CurrentStep = firstStep.Name
    p.Status.StartTime = &now
    r.ev.Eventf(p, corev1.EventTypeNormal, "PipelineStarted", "...")

    return r.patchStatus(ctx, p)
}
```

The full file is over 200 lines. It handles: cache reads, finalizer management,
owner references, status patching, event emission, phase dispatch, Job
creation, completion detection via Job status conditions, and advancing
to the next step or terminal state.

Every change to the state machine — a new step, a different terminal
condition, a changed phase name — requires editing Go, rebuilding the
binary, pushing a Docker image, and rolling the deployment.

---

## How it is done now

The same pipeline, declared in a Katalog:

```yaml
operatorBox:
  default: true

  onReconcile:
    jobs:
      - name: "{{ .metadata.name }}-build"
        image: "{{ .spec.image }}"
        command: ["sh", "-c", "{{ index (index .spec.steps 0) \"command\" | join \" \" }}"]
        when:
          - field: status.phase
            operator: notExists       # only on first reconcile

      - name: "{{ .metadata.name }}-test"
        image: "{{ .spec.image }}"
        command: ["sh", "-c", "{{ index (index .spec.steps 1) \"command\" | join \" \" }}"]
        when:
          - field: status.phase
            equals: "Running/build"
          - field: children.job.status.succeeded
            greaterThan: 0           # build Job completed

      - name: "{{ .metadata.name }}-notify"
        image: "{{ .spec.image }}"
        command: ["sh", "-c", "{{ index (index .spec.steps 2) \"command\" | join \" \" }}"]
        when:
          - field: status.phase
            equals: "Running/test"
          - field: children.job.status.succeeded
            greaterThan: 0           # test Job completed

  # Define the custom status fields.
  # Works with any custom value type.
  status:
    fields:
      - path: phase
        value: "Pending"
        when:
          - field: status.phase
            operator: notExists      # first reconcile only

      - path: phase
        value: "Running/build"
        when:
          - field: status.phase
            operator: in
            value: "Pending,"        # Pending or empty

      - path: phase
        value: "Running/test"
        when:
          - field: status.phase
            equals: "Running/build"
          - field: children.job.status.succeeded
            greaterThan: 0

      - path: phase
        value: "Running/notify"
        when:
          - field: status.phase
            equals: "Running/test"
          - field: children.job.status.succeeded
            greaterThan: 0

      - path: phase
        value: "Succeeded"
        when:
          - field: status.phase
            equals: "Running/notify"
          - field: children.job.status.succeeded
            greaterThan: 0

      - path: phase
        value: "Failed"
        when:
          - field: children.job.status.failed
            greaterThan: 0
```

No Go. No binary build. No deployment cycle. A new step is one more
`jobs:` entry and two more `status.fields:` entries. Readable by anyone
on the team. Reviewable in a pull request without understanding Go.

---

## The two new primitives that make this possible

**`operator: notExists` in `when:` conditions**

Detects that a field has not yet been written — specifically, the first
reconcile before any status exists. When `status.phase` is absent from the
informer's cached object, `notExists` passes. After the first reconcile writes
`"Pending"`, `notExists` fails for every subsequent cycle.

**`when:` on `status.fields` entries**

Status fields are no longer written unconditionally. Each field entry carries
an optional `when:` block evaluated against the full CR state — including
`.status.*` and `.children.*`. The last field entry whose conditions pass wins.

This override semantics is the state machine. Declare terminal states last:

```yaml
- path: phase
  value: "Running/build"    # written first — easy conditions
  when: [...]

- path: phase
  value: "Succeeded"        # written later — harder conditions, overrides above
  when: [...]

- path: phase
  value: "Failed"           # written last — overrides everything when a Job fails
  when: [...]
```

---

## What Orkestra still provides

Switching from a constructor to a declarative Katalog does not remove any
runtime guarantees:

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

### 1. Install the CRD

```bash
kubectl apply -f crd.yaml
```

### 2. Start the runtime

```bash
ork run -f katalog.yaml
```

### 3. Apply both CRs

```bash
kubectl apply -f cr.yaml
```

This creates two pipelines: `build-and-test` (succeeds) and
`failing-pipeline` (fails at the build step).

### 4. Watch the state machine

In a separate terminal:

```bash
k get pipelines -n default -w
```

Expected output:
```
NAME               PHASE            STEP     AGE
build-and-test     Pending                   0s
failing-pipeline   Pending                   0s
build-and-test     Running/build    build    3s
failing-pipeline   Running/build    build    3s
failing-pipeline   Failed                    9s
build-and-test     Running/test     test     11s
build-and-test     Running/notify   notify   17s
build-and-test     Succeeded                 21s
```

`failing-pipeline` drives to `Failed` when its build Job exits non-zero.
`build-and-test` completes all three steps in sequence.

### 5. Inspect the phase transitions

```bash
# Full status for the successful pipeline
kubectl get pipeline build-and-test -o yaml | grep -A10 "status:"
```

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: ReconcileSucceeded
  phase: Succeeded
  currentStep: ""
  image: alpine:3.19
```

```bash
# Full status for the failed pipeline
kubectl get pipeline failing-pipeline -o yaml | grep -A8 "status:"
```

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: ReconcileSucceeded
  phase: Failed
  currentStep: ""
```

Note: the Ready condition is `True` even for the `Failed` pipeline. Ready
reflects whether the *operator* reconciled successfully — it did. The `phase`
field reflects the *pipeline*'s outcome. These are different concerns and
deliberately separate.

### 6. Inspect the Jobs

```bash
kubectl get jobs
```

```
NAME                         COMPLETIONS   DURATION
build-and-test-build         1/1           3s
build-and-test-test          1/1           4s
build-and-test-notify        1/1           3s
failing-pipeline-build       0/1           6s    ← failed
```

The test and notify Jobs for `failing-pipeline` are never created — their
`when:` conditions never pass because the build Job never succeeds.

Owner references are set on every Job. When the Pipeline CR is deleted,
all Jobs are cascade-deleted by Kubernetes garbage collection. The operator
wrote no deletion code.

### 7. Check the metrics

```bash
ork proxy

curl localhost:8080/katalog/pipeline | jq '{
  reconcileTotal: .reconcileTotal,
  queueDepth: .queueDepth
}'
```

### 8. Clean up

```bash
chmod +x cleanup.sh && ./cleanup.sh
```

---

## When the constructor is still right

The declarative model handles state machines that manage Kubernetes resources.
The constructor remains the right answer when:

- **Steps call external APIs.** If step 2 is "call the payment processor and
  wait for confirmation," that is a side effect. Declarative templates cannot
  express side effects. Write a hook or constructor.

- **Transition logic requires complex Go business logic.** If deciding
  whether to advance requires parsing a JSON report and checking 15 fields
  across nested structures, a note may not cover it.

- **You are migrating an existing controller-runtime reconciler.** The
  constructor's `Reconcile(ctx, key) error` signature maps directly from
  `Reconcile(ctx, req) (ctrl.Result, error)`. The migration is mechanical
  and the constructor is the right migration target.

For everything else — phased execution, sequential Jobs, status-driven
conditionals, multi-step workflows over Kubernetes resources — the declarative
model is simpler, faster to iterate on, and accessible to the whole team.

---

## Further reading

- [`examples/advanced/10-constructor/`](../advanced/10-constructor/README.md) — the
  Go constructor this example replaces, preserved as a reference implementation
- [`docs/papers/declarative-state-machines.md`](../../docs/publications/declarative-state-machines.md)
  — the full comparison with code samples and the migration guide
- [`docs/concepts/notes.md`](../../docs/runtime-manual/concepts/notes.md) — the note library
  used in template expressions
- [`docs/concepts/conditional-status-fields.md`](../../docs/runtime-manual/concepts/conditional-status-fields.md)
  — how `when:` on status fields works internally