# Operational state on the CR

Each CR managed by Orkestra carries a live snapshot of its operator's runtime state. The runtime stamps this data onto the CR after every reconcile — so reading the CR is reading the operator.

---

## What the runtime stamps

After a reconcile cycle completes, Orkestra patches two top-level fields into the CR's status:

- **`.health`** — the CRD's aggregate health: status, phase, readyCount, totalCount, and any custom health fields declared via `serve.health:`.
- **`.metrics`** — counters and gauges from the reconcile loop: reconcileCount, errorCount, queueDepth, and any custom metrics declared via `serve.metrics:`.

These fields are written by the runtime itself — not by your hooks or reconciler code — and are authoritative for the lifetime of the CR.

---

## Why this matters

Before, observing an operator's health required an HTTP call to its `/katalog/{crd}/health` endpoint. That works at query time, but it cannot be used as a reconcile-time condition or a validation rule, because those run synchronously inside the reconcile loop.

With operational state on the CR, those same values are always available as ordinary CR fields. Any condition that can read a CR field can now read the operator's live state:

```yaml
preReconcile:
  gate:
    when:
      - field: "{{ .health.status }}"
        equals: "healthy"
```

```yaml
validation:
  rules:
    - field: "{{ .health.readyCount }}"
      operator: gte
      value: "1"
      message: "operator must have at least one ready instance"
```

---

## Admission and the conditional fetch

At admission time the webhook needs the same data, but it cannot read it from the CR being created — the CR does not exist yet. Instead, the webhook fetches it from the running runtime's HTTP endpoint, and only when a rule actually references it.

`HasHealthField()` and `HasMetricsField()` on the validation and mutation config inspect every rule's field expression before making any HTTP call. A CRD with no health or metrics rules pays zero HTTP cost at admission time. A CRD that references `.health.status` gets the live value fetched once and injected into the resolver context for all rules in that admission request.

---

## Cross-CRD access

Operational state is also readable by other operators. When a CRD declares a `cross:` source pointing at another CRD, Orkestra resolves the target CR and its stamped health and metrics fields are available in the referencing CRD's reconcile context:

```yaml
cross:
  - name: database
    crd: databases.example.io
    matchLabels:
      app: "{{ .Name }}"
```

Inside conditions or templates, `.cross.database.health.status` reads the database operator's stamped health — no HTTP call, no extra wiring.

---

## `serve:` — what you expose

The `serve:` block in your Katalog declares which CR fields the runtime should promote to the health and metrics snapshot and expose via the live API. Fields not declared in `serve:` are still stamped if they are part of the standard health/metrics schema; `serve:` extends the snapshot with domain-specific fields your operator owns.

→ [Serve schema](../../reference/schema/02-katalog/20-serve.md)

---

## Where to go next

- [Live API](../live-api/index.md) — the HTTP endpoints the runtime exposes per CRD
- [ONCOP](../oncop/index.md) — how cross-CRD reads resolve operational state across operators
- [Gating](../gating/index.md) — using `.health.*` and `.metrics.*` in preReconcile gate conditions
