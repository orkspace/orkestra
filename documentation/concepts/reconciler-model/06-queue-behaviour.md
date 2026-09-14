# Queue behaviour

Each CRD's workqueue is unlimited by default — Orkestra enqueues every reconcile trigger without bound and never drops based on depth. `queue.behaviour` is how you add back-pressure: it lets you declare what happens when the queue approaches or reaches a declared `maxDepth`, and optionally make that decision conditional.

`maxDepth` is the reference point. Without `behaviour:`, it has no effect on whether items are dropped. With `behaviour:`, it becomes the threshold that `onLimit:` and `onThreshold:` fire against.

---

## Why conditions matter

A simple unconditional drop ignores context:

- Is this peak traffic or an off-hours maintenance window?
- Does this CR need immediate reconciliation, or can it wait for the next resync?

`behaviour:` answers these questions declaratively, using the same condition language available in gate conditions.

---

## `onLimit:` — at the cap

`onLimit` fires when the queue reaches `maxDepth`. Without conditions, `drop: true` drops every incoming item when the queue is full:

```yaml
queue:
  maxDepth: 500
  behaviour:
    onLimit:
      drop: true
```

With conditions, the drop is conditional:

```yaml
queue:
  maxDepth: 500
  behaviour:
    onLimit:
      when:
        - field: "{{ inBusinessHours }}"
          equals: "false"
```

Items arriving when the queue is full are dropped only outside business hours. During business hours, the queue accepts items beyond `maxDepth` until the worker pool drains it.

---

## `onThreshold:` — before the cap

`onThreshold` fires when the queue reaches a declared percentage of `maxDepth`. It is a preemptive signal — you can start shedding load before the queue fills completely:

```yaml
queue:
  maxDepth: 500
  behaviour:
    onThreshold:
      value: 70     # fires at 70% — 350 items
      when:
        - field: "{{ inBusinessHours }}"
          equals: "false"
```

`onThreshold` always drops — `drop: true` is the only meaningful value here. Declaring `drop: false` is accepted but produces a validation warning.

Combine `onThreshold` and `onLimit` to handle both the approaching-limit and the at-limit cases differently:

```yaml
behaviour:
  onThreshold:
    value: 80          # preemptive: drop at 80% if outside hours
    when:
      - field: "{{ inBusinessHours }}"
        equals: "false"
  onLimit:
    drop: true         # final cap: always drop at 100%
```

---

## Two-tier evaluation

`behaviour:` evaluation runs in two stages, at different points in the event path.

**Tier 1 — workqueue.** When an item arrives, the workqueue checks the current depth against `maxDepth` and the threshold. If the depth condition is met and no `when`/`or` conditions are declared, the item is dropped immediately.

**Tier 2 — informer.** If `when` or `or` conditions are declared, the workqueue sets a flag (`NeedsBehaviourEval`) but does not drop the item yet. The informer's event handler picks up the flag and evaluates the conditions with the full preReconcile resolver context — time functions, gate fields, notes, sentinels, external call results. The item is dropped only if the conditions pass at that point.

This is the same evaluation path as `preReconcile.enqueueGate.when:`. The conditions available in `behaviour.onLimit.when:` are the same conditions available in enqueue gates — because both run in the same evaluator, at the same moment in the event lifecycle.

---

## Relationship to other queue settings

| Setting | What it controls |
|---------|-----------------|
| `maxDepth` | Hard cap on queue size. `0` = unlimited (default). Enables `behaviour:`. |
| `behaviour.onLimit` | What happens when `maxDepth` is reached. |
| `behaviour.onThreshold` | What happens when queue reaches N% of `maxDepth`. |
| `retryBackoff` | Delay before re-enqueuing a failed reconcile. See [retry backoff](../operatorbox/09-retry-backoff.md). |
| `resync` | Periodic re-enqueue of all CRs, regardless of events. See [resync vs requeue](04-resync-vs-requeue.md). |

`behaviour:` controls what enters the queue. `retryBackoff` and `resync` control what exits and re-enters it on the success and failure paths. They are independent — all three can be declared together.

---

## Schema reference

→ [queue schema](../../reference/schema/02-katalog/14-queue.md)
