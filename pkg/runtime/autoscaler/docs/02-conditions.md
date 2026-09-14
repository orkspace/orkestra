# 02 — Conditions

## Structure

```yaml
operatorBox:
  autoscale:
    conditions:
      or:          # OR — at least one must match
        - time: ...
        - dayOfWeek: ...
        - cron: ...
        - field: ...  # inline metric
      when:           # AND — all must match
        - field: metrics.queueDepth
          greaterThan: "200"
    do:
      workers: 8
```

**`or` is OR, `when` is AND.**

Full condition expression: `(or passes OR or is Empty() AND (all when entries pass OR when is Empty()`.

## or — time window

```yaml
or:
  - time:
      after: "08:00"
      before: "20:00"
```

Both `after` and `before` are optional. Either alone is valid. Times are in 24-hour `HH:MM` format, evaluated against the local clock of the operator process.

## or — day of week

```yaml
or:
  - dayOfWeek:
      in: [Monday, Tuesday, Wednesday, Thursday, Friday]
```

```yaml
or:
  - dayOfWeek:
      notIn: [Saturday, Sunday]
```

`in` and `notIn` are mutually exclusive. Day names are case-insensitive.

## or — cron

```yaml
or:
  - cron: "0 9 * * 1-5"   # 09:00 every weekday
    duration: 9h            # window stays open for 9 hours
```

The cron expression uses standard five-field syntax (via `robfig/cron`). When the cron fires, a window opens and stays open for `duration`. Without `duration`, the window stays open for one evaluation interval (point-in-time).

Window state is tracked per cron expression string in `state.CronWindowsOpenAt` via `types.TickCronWindow`. This is stateful across evaluation ticks: a cron fire that occurs between two ticks is not missed — the window remains open until `duration` elapses. `TickCronWindow` is a general-purpose function; any Orkestra component (future job runner, etc.) can bring its own `map[string]time.Time` and call it on each evaluation cycle.

## or — inline metric

```yaml
or:
  - field: metrics.workersBusyPercent
    greaterThan: "90"
```

An inline metric in `or` participates in the OR — if the metric threshold is met, the whole `or` block passes even if time/day conditions do not.

## when — metric conditions

```yaml
when:
  - field: metrics.queueDepth
    greaterThan: "500"
  - field: metrics.errorRatePercent
    lessThan: "5"
  # Cross-binary: add source.endpoint when the CRD is in a different binary
  - field: cross.managed-database.metrics.queueDepth
    greaterThan: "200"
    source:
      endpoint: "http://database-operator:8080/katalog/managed-database"
```

All `when` entries must pass (AND). Supported metric fields:

| Field | Source |
|-------|--------|
| `metrics.workersBusyPercent` | semaphore — in-flight / capacity × 100 |
| `metrics.workersIdlePercent` | semaphore — 100 − busy |
| `metrics.queueDepth` | current workqueue length |
| `metrics.reconcileDurationP95Ms` | rolling P95 of last 256 reconcile durations |
| `metrics.errorRatePercent` | errors / total reconciles × 100 |

For cross-CRD metrics see [03 — Cross-Metrics](03-cross-metrics.md).

## Condition evaluation rules

- An unknown `field` value returns `""` and evaluates as **false** (conservative — does not trigger override).
- Both `greaterThan` and `lessThan` compare as floating-point numbers. Non-numeric values are always false.
- Empty `or` and empty `when` both evaluate as **pass** — omitting them means "always apply the override".

---

**Next →** [03 — Cross-Metrics](03-cross-metrics.md)
