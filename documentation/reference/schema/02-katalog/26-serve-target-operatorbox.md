# serve.target operatorBox

Each named target in `serve.target` can carry its own `operatorBox`. When a CR is applied through a specific target, the reconciler uses that target's `operatorBox` instead of the CRD-level one — a different set of child resources, different lifecycle hooks, and optionally a different set of `preReconcile` gates.

This makes the target the unit of runtime execution: the same CRD can behave differently depending on which surface delivered the intent, without branching on `when:` conditions inside one shared box.

---

## Declaration

```yaml
spec:
  crds:
    app:
      operatorBox:               # CRD-level fallback — used by kubectl apply / unknown targets
        reconciler:
          hooks:
            location: github.com/myorg/myoperator/hooks
            function: AppHooks
            args:
              featureEnabled: '{{ .external.flags.body }}'
              inBusinessHours: '{{ inBusinessHours }}'

      serve:
        enabled: true
        target:
          v2-enabled:
            primary: true
            operatorBox:         # hooks — args forced, gate active
              preReconcile:
                enqueueGate:
                  when:
                    - field: '{{ inBusinessHours }}'
                      equals: "true"
              reconciler:
                hooks:
                  location: github.com/myorg/myoperator/hooks
                  function: AppHooks
                  args:
                    featureEnabled: "true"
                    inBusinessHours: '{{ inBusinessHours }}'

          regional:
            operatorBox:         # declarative — forEach over regions
              preReconcile:
                reconcileGate:
                  when:
                    - field: "{{ len .spec.regions }}"
                      notEquals: "0"
              onCreate:
                deployments:
                  - name: "{{ .metadata.name }}-{{ .item }}"
                    image: "{{ .spec.image }}"
                    forEach:
                      field: spec.regions
                      as: item
                namespaces:
                  - name: "{{ .metadata.name }}-{{ .item }}"
                    forEach:
                      field: spec.regions
                      as: item
```

---

## Resolution order

When the reconciler is invoked, it selects the `operatorBox` by checking the CR's annotations (most specific first):

1. Target entry whose name matches `orkestra.orkspace.io/serve-alias` (alias wins over primary)
2. Target entry whose name matches `orkestra.orkspace.io/serve-target`
3. CRD-level `operatorBox` — fallback when no annotation or no matching target

CRs applied via `kubectl apply` carry no gateway annotations, so they always use the CRD-level box.

---

## `preReconcile` gates at the target level

Per-target `operatorBox` blocks support the same `preReconcile` shape as the CRD-level box:

```yaml
operatorBox:
  preReconcile:
    enqueueGate:       # evaluated before the item enters the work queue
      when:
        - field: "{{ .spec.image }}"
          notEquals: ""

    reconcileGate:     # evaluated after dequeue, before reconciler runs
      when:
        - field: "{{ len .spec.regions }}"
          notEquals: "0"
      or:
        - field: '{{ .spec.tier }}'
          equals: premium
```

| Gate | Evaluated by | Effect when condition fails |
|------|--------------|-----------------------------|
| `enqueueGate` | Informer (before work queue) | CR is dropped from the queue — no reconcile cycle starts |
| `reconcileGate` | Kordinator (after dequeue) | Reconcile is skipped for this cycle — item is requeued |

Gate semantics are identical to those at the CRD level — the only difference is they apply only when this target's box is active. See [operatorBox preReconcile](04-operatorbox.md#prereconcile) for the full gate reference including `external:` calls.

---

## Surface switch and resource cleanup

When a CR is re-submitted via a different target (a surface switch), the reconciler detects the change by comparing two annotations:

| Annotation | Written by | Value |
|---|---|---|
| `orkestra.orkspace.io/serve-alias` | Apply handler (per-request) | The alias name, or `""` for the primary target |
| `orkestra.orkspace.io/last-surface` | Reconciler (after each cycle) | The effective target active at the end of the previous reconcile |

A mismatch between `last-surface` and the current effective target triggers a cleanup sweep before the new target's `operatorBox` runs. The sweep finds resources stamped with `orkestra-owner=<name>.<prevTarget>` and deletes them — both namespaced and cluster-scoped types.

**Why a sweep and not template expansion?** When the gateway routes a CR away from the old target, the spec fields that drove that target's `forEach` declarations may already be absent (e.g. `spec.regions` is not included in the new POST body). Template-based deletion would expand `forEach` to nothing and silently miss the orphans. The label-selector sweep is immune to spec changes.

After cleanup, `last-surface` is updated to the current target and reconciliation proceeds with the new box.

---

## `keepPreviousSurface`

To retain old-target resources after a switch (e.g. a canary scenario where both targets run simultaneously), set `keepPreviousSurface: true`. It can be declared at the CRD level or per-target:

```yaml
# CRD level — applies to all targets
serve:
  apply:
    overrides:
      keepPreviousSurface: true

# Per-target — applies only when this target becomes active
target:
  canary:
    apply:
      overrides:
        keepPreviousSurface: true
    operatorBox:
      onCreate:
        deployments:
          - name: "{{ .metadata.name }}-canary"
```

CRD-level wins if set; per-target applies otherwise.

| Value | Behaviour |
|-------|-----------|
| `false` (default) | Previous-surface resources are deleted on the first reconcile after a target switch |
| `true` | Previous-surface resources are left alive — no sweep runs |

---

## What stays fixed at the CRD level

Reconciler settings — `workers`, `resync`, and `autoscale` — are always taken from the CRD-level `operatorBox`. Everything else (`onCreate`, `onReconcile`, `onDelete`, `preReconcile`, `status`, `reconciler.hooks`, `reconciler.hooks.args`) can be overridden per-target. When a target's `operatorBox` omits a block, it falls back to the CRD-level value.

---

## Simulating a specific target

Use `spec.target` in the simulate file, or `--target` on the CLI, to route the simulation through a named target's box:

```yaml
# simulate-regional.yaml
spec:
  katalog: ./katalog.yaml
  cr: ./cr.yaml
  target: regional
  expect:
    ops:
      - cycle: 1
        verb: create
        resource: deployments
        name: my-app-eu-west
      - cycle: 1
        verb: create
        resource: namespaces
        name: my-app-eu-west
```

`preReconcile` gates are evaluated in simulation. A `reconcileGate` that blocks (e.g. `len .spec.regions != 0` when regions is Empty() will cause the simulate cycle to produce no ops — use `expect.crds` with `steady: false` to assert the gate fired rather than asserting specific resources.

---

## Where to go next

- [operatorBox](04-operatorbox.md) — full `preReconcile` gate reference, `when:` conditions, `external:` calls
- [serve.target](20-serve.md#servetarget) — target map declaration, tokens, response config
- [garbage collection](../../internal/runners/docs/02-garbage-collection.md) — two-path deletion model: template-based (CR deletion) vs sweep-based (surface switch)
- [simulate](../05-simulate/index.md) — multi-target simulation patterns
