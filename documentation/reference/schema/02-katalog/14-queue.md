# queue

The `queue:` block controls how reconcile events accumulate and when the operatorBox is considered unhealthy. Every CRD gets its own isolated queue by default — queue pressure from one CRD cannot affect another.

```yaml
crds:
  myapp:
    operatorBox:
      reconciler:
        workers: 4
        resync: 30s
        queue:
          maxDepth: 500
          failureThreshold: 10
```

---

## Fields

| Field | Type | Default | Description |
|---|---|---|---|
| `maxDepth` | int | `0` (unlimited) | Depth reference for `behaviour:`. `0` means unlimited — no events are dropped based on depth. A positive value sets the limit at which `behaviour.onLimit:` fires and the 100% reference for `behaviour.onThreshold:`. Has no effect without a `behaviour:` declaration. |
| `failureThreshold` | int | `5` (`FAILURE_THRESHOLD` env) | Consecutive reconcile failures before the operatorBox transitions to degraded. Resets to zero on the next successful reconcile. |
| `shared` | bool | `false` | Use the shared global workqueue instead of an isolated per-CRD queue. Rarely needed. |
| `retryBackoff` | duration or object | — | Intra-reconcile retry backoff. Shorthand (`5s`) sets `initial` only; full form: `initial`, `max`, `multiplier`, `maxAttempts`. See [retry backoff](../../concepts/operatorbox/09-retry-backoff.md). |
| `behaviour` | object | — | Controlled back-pressure — what happens when `maxDepth` is reached or a threshold percentage is crossed. Requires `maxDepth > 0`. See [`behaviour:`](#behaviour--controlled-back-pressure) below. |

`failureThreshold` default is controlled by the `FAILURE_THRESHOLD` environment variable in the runtime deployment — set it in `values.yaml` under `runtime.config`.

---

## `maxDepth` — the depth reference

`maxDepth` sets the queue depth at which `behaviour:` triggers. Without a `behaviour:` block, setting `maxDepth` has no effect on queue behavior — events are never dropped based on depth alone.

When `behaviour:` is declared, `maxDepth` is the limit `onLimit:` fires against and the 100% reference for `onThreshold.value:`. See [`behaviour:`](#behaviour--controlled-back-pressure) below.

When a drop occurs (via `behaviour.onLimit.drop: true` or `behaviour.onThreshold:`), Orkestra logs a warning:

```json
{"level":"warn","key":"default/my-app","gvk":"...","limit":500,"depth":500,"message":"enqueue: queue depth limit reached — item dropped"}
```

The dropped event is a reconcile trigger — the CR itself is unchanged in etcd. The next resync tick re-enqueues it automatically.

### Interaction with autoscaling

When the autoscaler is active, `maxDepth` becomes the baseline. The autoscaler can raise the limit at runtime when conditions trigger (e.g., queue depth exceeds 80% of the limit) and restore it when conditions clear:

```yaml
operatorBox:
  reconciler:
    queue:
      maxDepth: 100       # baseline — what you start with
  autoscale:
    conditions:
      when:
        - field: metrics.queueDepth
          greaterThan: "80"
    do:
      workers: 8
      queueDepth: 500   # raised when conditions are met
```

**Try it:**
```bash
ork init --pack advanced
cd 12-autoscale/01-without-autoscaler   # see what happens when maxDepth is hit
ork run

cd 12-autoscale/02-based-on-own-metrics  # autoscaler raises the limit dynamically
ork run
```

---

## `failureThreshold` — when health degrades

Each reconcile failure increments a consecutive failure counter. When it reaches `failureThreshold`, the operatorBox transitions to **degraded**:

- The Control Center marks it unhealthy with the failure count and last error.
- Other CRDs with `dependsOn: <this-crd>: healthy` stop processing new CRs.
- If `rollback:` is configured, the rollback templates execute.
- The counter resets to zero on the next successful reconcile.

The default of `5` is appropriate for most operators. Increase it for operators that call external services that can be transiently unavailable — a lower threshold would cause false degraded states during brief outages:

```yaml
operatorBox:
  reconciler:
    queue:
      failureThreshold: 20   # external service can be down for a few minutes
```

Decrease it for operators managing critical infrastructure where you want immediate health signalling:

```yaml
operatorBox:
  reconciler:
    queue:
      failureThreshold: 2   # degrade fast — this CRD must be healthy
```

---

## `shared` — the shared queue

By default each CRD has its own isolated workqueue. Setting `shared: true` puts this CRD into the global shared queue. This is only useful in rare situations — for example, when a built-in Kubernetes resource (Pod, ConfigMap) is being reconciled and you want it to share the global queue rather than consuming a separate goroutine pool.

For custom CRDs, always leave `shared: false`.

---

## Global defaults

`failureThreshold` has a runtime-wide default controlled by the `FAILURE_THRESHOLD` environment variable, configurable in `values.yaml`:

```yaml
# charts/orkestra/values.yaml
runtime:
  config:
    failureThreshold: 10   # FAILURE_THRESHOLD env — default failureThreshold for all CRDs
```

A per-CRD `queue:` declaration overrides the global default for that CRD only.

`maxDepth` has no runtime-wide default — each CRD queue is unlimited unless explicitly declared.

---

## `behaviour:` — controlled back-pressure

`queue.behaviour` configures what happens when the queue approaches or reaches its capacity. Without it, the only option when `maxDepth` is hit is a silent drop logged as a warning. With it, you can choose when to drop, conditionally.

```yaml
operatorBox:
  reconciler:
    queue:
      maxDepth: 500
      behaviour:
        onLimit:
          drop: true
        onThreshold:
          value: 70
```

`maxDepth` must be greater than zero for `behaviour:` to have any effect — `ork validate` rejects a `behaviour:` declaration on an unlimited queue.

---

### `onLimit:`

Fires when the queue reaches `maxDepth`. Fields:

| Field | Type | Description |
|-------|------|-------------|
| `drop` | bool | Whether to drop the incoming item. |
| `when` | condition list | AND conditions — all must pass to drop. |
| `or` | condition list | OR conditions — at least one must pass to drop. When both `when` and `or` are present, both blocks must pass. |

Without `when` or `or`, `drop: true` drops every item that arrives when the queue is full. With conditions, the drop decision is delegated to the evaluator — items are dropped only when the conditions pass:

```yaml
behaviour:
  onLimit:
    when:
      - field: "{{ inBusinessHours }}"
        equals: "false"
```

This drops arriving items only outside business hours. During business hours, the queue still accepts items even past `maxDepth` until the evaluator gets a chance to drain it.

---

### `onThreshold:`

Fires when the queue reaches a declared percentage of `maxDepth`. Fields:

| Field | Type | Description |
|-------|------|-------------|
| `value` | int (1–100) | Queue fullness percentage at which the threshold fires. Required. |
| `drop` | bool | Always treated as `true` for `onThreshold` — declaring `false` is accepted but produces a validation warning. |
| `when` | condition list | AND conditions — all must pass to drop. |
| `or` | condition list | OR conditions — at least one must pass to drop. |

```yaml
behaviour:
  onThreshold:
    value: 70        # fires when queue is 70% full
    when:
      - field: "{{ inBusinessHours }}"
        equals: "false"
```

`onThreshold` is a preemptive signal — it fires before the queue is full, giving you a chance to shed load earlier. Combine with `onLimit` to handle both the approaching-limit and the at-limit cases:

```yaml
behaviour:
  onThreshold:
    value: 80          # start dropping at 80%
    when:
      - field: "{{ inBusinessHours }}"
        equals: "false"
  onLimit:
    drop: true         # always drop at 100% — no conditions
```

---

### Two-tier evaluation

`behaviour:` evaluation happens in two stages:

**Tier 1 — workqueue (arithmetic).** When an item arrives and the queue is at or past the threshold/limit, the workqueue checks the depth. If `drop: true` and no `when`/`or` conditions are declared, the item is dropped immediately without further evaluation.

**Tier 2 — informer (contextual).** If `when` or `or` conditions are declared, the arithmetic check sets a flag but defers the final drop decision. The informer picks it up with the full preReconcile resolver context — gate fields, notes, sentinels, external calls — and evaluates the conditions there. The item is dropped only if the conditions pass.

This means `onLimit.when: inBusinessHours == false` can reference the same template functions available in `enqueueGate.when:` — time functions, note values, and any other resolver context — because both run in the same evaluator.
