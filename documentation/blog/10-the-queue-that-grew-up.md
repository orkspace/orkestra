# The Queue That Grew Up

There is a particular kind of cleanup pass that every solo builder
knows. The one you do when the project has been growing fast and you
finally let yourself look at the parts you haven't touched in months.
You're not adding features. You're just — looking. Tidying. Paying
attention to things you've been ignoring.

That's what found the queue.

---

## A single file with a readme

I had been reorganising `pkg/katalog`. The validate files had
accumulated — `validate_serve.go`, `validate_tokens.go`,
`validate_sentinels.go`, each one growing — and I was inspired by how
Kubernetes itself organises validation into a dedicated sub-package. So
I moved them. `pkg/katalog/validate/`. Cleaner. More honest about what
the package actually did.

That was satisfying. And then I arrived at `pkg/runtime/queue`.

One file. A short readme. The entire package could be described in two
sentences: the informer is calling with an event, here is a `QueueItem`
to hold the key and the GVK, here is a `Workqueue` to hold the items.
When the queue reached `maxDepth`, items were dropped and a warning was
logged.

There was even an example to demonstrate the drop behaviour. I had
written it proudly at some point. Now I read it and something felt
wrong.

Not broken. Just wrong.

---

## The drop that nobody configured

The drop was unconditional. The queue was full? Drop the item. Log a
warning. Move on.

I started asking: who decided this was the right behaviour? Nobody did.
I had copy-followed a pattern I was familiar with — the workqueue drops when
full, that's how it works — without asking whether Orkestra should
expose that decision to the operator author.

What if you didn't want to drop? What if a dropped item during a
critical window was a problem? What if you wanted to drop only outside
business hours, or only for certain resource types, or only below a
certain priority?

The queue had capacity information. It just wasn't using it for
anything except a binary full/not-full decision.

That led to `queue.behaviour`. `onLimit` and `onThreshold` — two shapes
for saying what the queue should do based on its own depth, with the
same `when:` and `or:` conditions available everywhere else in Orkestra.
That part is covered in the [gates writing](09-three-gates.md). 
But it was this cleanup pass that opened the door.

---

## Reading the source

With the behaviour work done, I was satisfied with the package. But
something else had been nagging at me. I had always taken deduplication
for granted — the docs said the workqueue handles it, I believed the
docs, I never read the implementation.

So I did.

It was simpler than I expected. Not magic. Not complex. A comparison.

If `default/my-app` is in the queue and another event arrives for
`default/my-app`, the workqueue asks: is this the same key? Yes. Replace
what's waiting with the new event — you're the latest, you're what
matters. Process once.

Elegantly efficient. And immediately interesting.

Because if the key is the comparison, then changing the key changes the
identity. An item carrying `default/my-app + sentinels` would not
compare equal to `default/my-app + different sentinels`. They would be
distinct items. Both would survive.

I had been thinking about sentinel-based gate evaluation for a while.
The `reconcileGate` could declare sentinels — `generationChanged`,
`labelsChanged` — and if those sentinels were present in the event, the
gate would pass. But the gate was receiving events that had potentially
been coalesced. Ten edits, one queue item, the last sentinel context
wins. The first nine were gone.

What if the sentinel map was a pointer? Each event would produce a
distinct pointer. The workqueue's equality check would fail because
pointer A ≠ pointer B even if the underlying maps are identical. Every
event would survive deduplication as a distinct item.

I added the test. It worked.

---

## The cost and the implicit assumption

Working code isn't always right code.

The problem was that using sentinel presence to imply event awareness
was implicit. An operator author who declared `reconcileGate.sentinels`
didn't necessarily know they were opting into event-per-item queue
semantics. They just wanted the gate to check whether a label had
changed. The fact that this meant ten reconcile cycles for ten rapid
edits instead of one — that was invisible to them.

I thought about how Orkestra handles other things with real cost. The
[`enrich:` capability](/docs/concepts/operatorbox/enrich/) 
— which fetches live resources from the API server
to make things like `podCount`, `hasCrashingPod` and other 
[enrichment targets](/docs/concepts/operatorbox/enrich/targets/) available in the resolver — is
explicit. You declare the `enrich:` block and you understand that means 
[API calls](/docs/concepts/operatorbox/enrich/cost-and-when/). 
The cost is visible because the declaration is visible.

Event awareness needed the same treatment.

---

## The merge that didn't work

Before landing on the explicit declaration, I tried something else.
What if events were merged before the reconcileGate evaluated?

Event 1: `generationChanged: true`, `labelsChanged: false`
Event 2: `generationChanged: false`, `labelsChanged: true`

A merge would produce: `generationChanged: true`, `labelsChanged: true`.
The gate checking for either would pass.

But that gate was now passing for a state that never existed. There was
no moment where both were simultaneously true. The merge had created a
ghost event — a conjunction of two separate truths that were only true
individually, never together.

That was wrong in a specific way. If your gate asks "did the generation
change?" the right answer is yes for event 1 and no for event 2. A
merged answer of yes for both is not an approximation of the truth. It
is a different truth entirely.

Merging didn't work. The individual events had to remain individual.

---

## Separating item from identity

The solution took a few days to arrive.

The insight was that the `QueueItem` and the queue identity were the same
thing in the original design, and they didn't have to be.

```go
type QueueItem struct {
    Key     string
    GVK     string
    EventID uint64
}
```

`EventID` controls identity, not content. When `EventID` is zero, the
workqueue's equality check fires on `Key` and `GVK` alone — normal
deduplication. When `EventID` is non-zero, every item is distinct
because every item carries a unique counter value. The sentinel values
live separately, keyed by the full identity including `EventID`.

The separation meant: **sentinel availability does not imply event
awareness**. You can have sentinels with deduplication. You can have
event awareness without sentinels. The two concerns are orthogonal.

```go
type queueItemIdentity struct {
    Key     string
    GVK     string
    EventID uint64
}

sentinels map[queueItemIdentity]map[string]string
```

The sentinel values are stored off to the side, indexed by the full
identity. When the informer sees an event `QueueItem` and asks for its
sentinels, the `Workqueue` looks up by identity and hands back a copy.
The queue itself stays clean — it never knows what a sentinel means. It
only knows identities.

---

## Six instead of three

The original three enqueue methods became six.

Before: `Enqueue`, `EnqueueWithKey`, `EnqueueWithSentinels`.

After: each of those three now has an event-aware variant —
`EnqueueWithEventSentinels`, `EnqueueWithKeyEventSentinels`, and so on.

That doubling looks like complexity but it's actually clarification.
The six methods make the two dimensions explicit: primary vs secondary
path (does the informer know the key, or does it need to resolve it?)
and event-aware vs coalescing (does each event need individual
identity?). Four combinations, six methods because primary
non-event-aware doesn't need both Enqueue and EnqueueWithKey to be
event-aware.

The routing through those methods is determined by two things: whether
the informer is handling a primary or secondary event, and what
`effectiveBox.reconcileGate.eventAware` resolves to. The informer
factory checks both and picks the right path. Neither the informer nor
the queue needs to know what the other is doing. The factory is the
only place that knows both.

> The `effectiveBox` being `operatorBox.preReconcile.reconcileGate.eventAware`.
>
> "Effective" because it is target aware.
> See [Orkestra Execution Model](/docs/concepts/execution-model/).

---

## What the queue became

The queue package went from one file with a readme to the quiet center
of a pipeline that three different gates now depend on.

Queue behaviour makes the queue a participant in admission — the first
tier of a two-tier evaluation where the queue checks arithmetic and
flags items for the informer to evaluate with full resolver context.

Event awareness makes the queue a participant in reconcile gate
evaluation — the `eventAware` declaration on `reconcileGate` has no
meaning until the enqueue path honours it by constructing the
appropriate `QueueItem` identity. The gate declares the policy. The
queue implements it.

And the sentinel context that travels from informer through queue to
kordinator to reconcile gate — that journey is only possible because
the queue holds sentinel values by identity, separate from the item,
and hands them back intact when the kordinator asks.

The queue does none of this by knowing what reconciliation means.

It does it by knowing, precisely, what its job is:

> **Can and should this work enter the execution pipeline according to the
> queue's runtime state and configured behaviour?**

Everything downstream is someone else's responsibility.

That boundary is what allowed the queue to become more without becoming
something it shouldn't be.