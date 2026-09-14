# Three gating points

Orkestra evaluates whether to act on a CR at three distinct points in the lifecycle. Each point has access to different information, runs at a different time, and produces a different kind of outcome.

```text
kubectl apply      →  Admission            →  etcd
                   ↓ denied/allowed
informer event     →  preReconcile         →  workqueue → reconciler
                   ↓ dropped/enqueued
reconcile cycle    →  Reconcile            →  child resources
                   ↓ skipped/created
```

---

## 1. Admission — at the API server boundary

**When**: synchronously, before the CR is stored in etcd. Every client — `kubectl`, the Control Center, a CI pipeline — passes through this gate.

**Evaluated by**: the Gateway's webhook handler.

**What it can gate on**:
- Field values in the incoming CR (`.spec.*`, `.metadata.*`)
- Notes — KEL functions (built-in catalog and user-defined via the `notes:` block), composing operational knowledge like `{{ inBusinessHours }}`, time, string, and Kubernetes-domain expressions
- Intent payload (`.request.*`) — when the CR arrives through the Gateway serve API in target mode, the raw intent fields are available before field translation runs
- Uniqueness — whether another CR already holds this value, queried live from the runtime's informer cache
- Operator health and metrics (`.health.*`, `.metrics.*`) — live operational stats from the runtime that will reconcile this CR, queried directly at admission time
- External HTTP responses (`.external.*`) — calls declared in `validation.external:` or `mutation.external:`
- Mutation: defaulting or overriding fields before validation rules run

**Outcome**: allow or deny the API request. A denied admission returns an error to the caller; the CR is never written to etcd.

**Health effect**: none — admission operates before any operator health state exists for this CR.

→ [Admission](../../orkestra-core/02-gateway/01-admission.md)

---

## 2. preReconcile — at the queue boundary

**When**: after the API server stores the CR and the informer delivers a watch event, before the item enters the workqueue or before the reconciler is called.

**Two sub-gates within preReconcile**:

Both sub-gates share the same resolver context, built from the CR at event time. Both are also **target-aware** — when the CR carries a target annotation (serve target mode), the gate conditions evaluated are those of the matching target's operatorBox, not the top-level one.

### `enqueueGate` — before the workqueue

Evaluated by the informer's event handler. An item that fails the enqueue gate is dropped silently — it never enters the workqueue and produces no health state change.

What it can gate on:
- Current object state (`.spec.*`, `.status.*`, `.metadata.*`)
- Notes — KEL functions (built-in catalog and user-defined via `notes:`) available in any template expression
- Intent payload (`.request.*`) — when the CR was submitted through the serve API in target mode
- Operator health and metrics (`.health.*`, `.metrics.*`) — written by the runtime as annotations, readable as template fields
- Sentinels — what changed between the old and new version of the object (`generationChanged`, `labelsChanged`, and the full `metav1.Object` coverage)
- Queue depth — `behaviour.onLimit` and `behaviour.onThreshold` fire here, using the same resolver

### `reconcileGate` — after dequeue, before the reconciler

Evaluated by the kordinator after the item is dequeued. An item that fails the reconcile gate is held back with a `gated` health state — it does not enter the reconcile pipeline.

What it can gate on:
- Current object state, notes, intent payload, health and metrics — same as `enqueueGate`
- Sentinels (computed at enqueue time, carried through the queue to dequeue time)
- External HTTP calls (`external:` declarations) — with `failPolicy:` controlling what happens when a call fails

**Outcome**:
- `enqueueGate`: item dropped, no health effect
- `reconcileGate`: item gated, health state set to `gated` (idle, not degraded)

→ [Conditional reconciliation](../conditional/04-conditional-reconciliation.md)  
→ [Queue behaviour](../reconciler-model/06-queue-behaviour.md)

---

## 3. Reconcile — inside the reconcile loop

**When**: during the reconcile pipeline, for each resource declared in `onCreate`/`onReconcile`/`onDelete`.

**Evaluated by**: the generic reconciler or your typed `Reconcile()` method.

**What it can gate on**:
- Current object state at reconcile time
- Notes — same KEL vocabulary available in every other gate
- Cross-CRD observation (`.cross.*` fields from other operator informer caches or ONCOP)
- External call results (`.external.*`)

**Outcome**: individual resources are created, updated, skipped, or deleted. Other resources in the same reconcile cycle are unaffected by a skipped resource.

→ [Reconcile pipeline](../operatorbox/01-reconcile-pipeline/index.md)

---

## Comparison

| | Admission | `enqueueGate` | `reconcileGate` | Reconcile resource `when:` |
|---|---|---|---|---|
| **Evaluated by** | Gateway webhook | Informer | Kordinator | Reconciler |
| **When** | Before etcd write | Before queue entry | After dequeue | Inside reconcile loop |
| **Scope** | The incoming request | The watch event | The dequeued item | One resource |
| **Old object visible** | Yes (on update) | Yes (via sentinels) | No (via carried sentinels) | No |
| **Cross-CRD data** | Via runtime query | No | External calls only | `.cross.*` full access |
| **Health effect** | None | None | `gated` (idle) | None |
| **Re-trigger** | Caller retries | Next watch event | Next watch event | Same reconcile cycle |

---

## How the tiers compose

A CR passes through all three tiers on its path from `kubectl apply` to a running Deployment. Each tier is independent — a CR that clears admission can still be held at preReconcile; a CR that clears preReconcile can still have individual resources conditionally skipped inside the reconcile loop.

Admission is the only tier with write-time authority — it can change the incoming object (mutation) or block it from being stored at all. The other two tiers act on objects that are already in etcd and do not modify the stored object as part of the gate decision itself.
