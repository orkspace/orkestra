# The Furniture That Became a Chair

This is the fifth time I have refactored the `operatorBox` schema.

I want to write about why, because I think the reason matters more than the
change itself.

---

## Where it started

Orkestra was built targeting declarative operators. You write a katalog, declare
your resources under `onCreate` and `onReconcile`, and the runtime manages the
rest. That was the original vision and it worked.

Then typed operators arrived — Go code implementing `domain.Reconciler`, hooked
in through constructors. Then controller-runtime compatibility. Suddenly Orkestra
had two kinds of reconcilers living in the same house, and the house had been
built for one of them.

The generic reconciler — the one that executes declarative lifecycle hooks — had
accumulated responsibilities that weren't really about reconciliation. Normalization,
namespace filtering, admission mutation, pre-reconcile gating. They all ran inside
it, wired as steps before the actual work. That was fine when there was only one
kind of reconciler. When typed operators arrived, they bypassed all of it. A typed
reconciler got a raw object and had to do its own preparation or go without.

The asymmetry was obvious in retrospect. Pre-reconcile gates — conditions that say
"only reconcile this CR if it's in the right namespace, or if this health check
passes" — those are clearly a runtime concern. They should run before any reconciler
is called, for any reconciler type. But they lived inside the generic reconciler,
invisible to typed operators.

---

## The kordinator split

At some point while working on the kordinator I drew a line.

Everything that runs before a reconciler is called — preparation, gating,
normalization, mutation, namespace filtering — belongs to the runtime. The
reconciler receives a fully-prepared request and reconciles it. That is all it
does.

The result was three new packages inside the kordinator worker loop:

```text
        Kordinator
            │
            ▼
      reconcileGate
            │
            ▼
        prepare
            │
            ▼
┌───────────┴────────────┐
│                        │
▼                        ▼ 
Object              PreparedContext
                         │
                ┌────────┴────────┐
                │                 │
                ▼                 ▼
            resolved           template
             data map          evaluation
                │                 │
                └───────┬─────────┘
                        ▼
                 PreparedRequest
                        │
        ┌─────────┬─────────┐
        ▼         ▼         ▼
      typed    generic    remote
```

No reconciler gets called until the CR has passed namespace guards, runtime-level admission, and any pre-reconcile gate. A typed operator, a declarative operator, and a remote HTTP service each receive the same fully-prepared request. None of them has to care about preparation.

The old system gave reconcilers wood, nails, and a hammer. The new system
hands them a finished chair.

---

## What the schema accumulated

Once the architecture was clear, I looked at `operatorBox` and saw the history
of every decision I hadn't made yet.

`autoscale:` at the top level of `operatorBox`. The reconciler doesn't autoscale
anything — the kordinator does. `finalizers:` listed as a reconciler concern.
Kordinator's `maintain/` package owns finalizers exclusively.

On the other side, `normalize:` and `imports:` and `forceConflict:` were on the
`CRDEntry` top level — a different address from `onReconcile:`, even though they
all feed into the same reconciliation pipeline.

The schema was a record of the order things were added, not the architecture they
belonged to.

---

## The groupings

The fix writes itself once you apply the same question to every field: who owns
this?

- **`operatorBox.observe:`** — data: cross-CRD reads, secondary watches, Kubernetes
  Events. Makes external state available inside the reconcile context and registers
  additional reconcile triggers. The reconciler reads this context; it does not
  configure it.

- **`operatorBox.preReconcile:`** — gate: should this event enter the queue, or reach
  the reconciler? enqueueGate fires at the informer. reconcileGate fires at the
  kordinator after dequeue. Events that fail either gate are silently dropped.
  The reconciler never sees this.

- **`operatorBox.runtime:`** — policy: autoscale, finalizers, namespace
  guards, deletion protection. The kordinator manages these as long-lived operator
  concerns, not per-reconcile concerns. If the reconciler never sees it, it lives here.

- **`operatorBox.reconcile:`** — work: implementation identity, lifecycle templates,
  execution tuning. If the reconciler reads it, it lives here.

- **`operatorBox.emit:`** — output: status fields and events written after every
  reconcile. The runtime stamps these; the reconciler declares them.

- **`admission:`** on `CRDEntry` — validation, mutation, conversion, webhook
  configuration. Shared policy read by the gateway at webhook time and by the
  runtime at reconcile time. Neither owns it. It is a declaration.

No field moved twice. Every move has one answer.

---

## Why this was possible

This is the fifth schema refactor. The first was about discovery — what does an
operator framework even need to express. The others were about architecture — what
owns what.

Each time I was able to do it because the katalog is a file, not a Kubernetes CRD.
Changing a CRD schema means writing migrations, versioning the API, handling live
cluster state. Changing a file format means updating YAML and moving on. I made
that choice early without fully understanding why it mattered. I understand now.

---

## What the operatorBox is

The operatorBox is the unit of reconciliation. It is the declaration that turns a
CRD into an operator — not just a schema type, but a live thing that observes,
gates, prepares, reconciles, manages, and emits.

The kordinator is its heartbeat. It prepares the context, runs the gates, calls
the reconciler, maintains the finalizers, applies the outputs. The reconciler is
its client — it receives a prepared request and reconciles exactly that.

I think this is the architecture I was building toward from the beginning without
knowing how to name it. The naming required building the wrong version several times
first.

Hopefully this is the last refactor of `operatorBox`. The groupings reflect
boundaries that won't move. When something new gets added, it will have an obvious
home.

That feels stable.
