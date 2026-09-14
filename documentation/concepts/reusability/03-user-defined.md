# User-Defined Reuse — Notes and Profiles

Two concepts in Orkestra let operator authors define their own reusable vocabulary: **notes** and **profiles**. Both are declared once and used anywhere expressions or configuration blocks appear.

---

## Notes — a shared function library

Notes are named template functions available inside every `{{ }}` expression in a Katalog: status fields, validation conditions, enqueue gates, reconcile gates, external call URLs, args values, and more.

A note defined once is callable from every expression in the Katalog:

```yaml
notes:
  functions:
    - name: inBusinessHours
      expression: '{{ and weekday (timeInWindow "09:00" "18:00") }}'

    - name: replicasByTier
      expression: '{{ if eq .spec.tier "enterprise" }}10{{ else if eq .spec.tier "standard" }}3{{ else }}1{{ end }}'

    - name: primaryRegion
      expression: '{{ index .spec.regions 0 }}'
```

Every CRD in that Katalog can now use `{{ inBusinessHours }}`, `{{ replicasByTier }}`, `{{ primaryRegion }}` — in enqueue gates, in status fields, in validation conditions, in external call configs. The logic is declared once and has one place to change.

Notes are also the primary way to encapsulate multi-step template logic that would otherwise be repeated inline. A `replicasByTier` note is clearer than a nested `{{ if eq }}` block copied into three status fields and two validation rules.

See [Notes](../notes/index.md) for the full contract: purity, nil-safety, built-in library, and how notes interact with the resolver.

---

## Profiles — named configuration presets

A profile is a named preset that expands into a complete configuration block at load time. Where notes abstract template expressions, profiles abstract YAML structure.

A profile named `high-throughput` might expand into:

```yaml
operatorBox:
  reconciler:
    workers: 8
    resync: 10s
    queue:
      maxDepth: 2000
  preReconcile:
    reconcileGate:
      when:
        - field: '{{ .status.phase }}'
          equals: "Ready"
```

Declare it once. Every CRD that needs high-throughput reconciliation references the profile name — no repeated YAML, no drift between CRDs that should behave identically.

Profiles are resolved at load time. The runtime never sees a profile reference — only the expanded values. This means profiles compose cleanly with Komposer overrides: an override applied on top of profile expansion works on concrete values, not abstract names.

See [User-defined profiles](../profiles/10-user-defined-profiles.md) for profile definition, scoping, and how they interact with Komposer overrides.

---

## Notes and profiles together

Notes and profiles solve different parts of the same problem. Profiles handle structural repetition — the same YAML blocks across multiple CRDs. Notes handle expression repetition — the same template logic in multiple `{{ }}` contexts.

A Katalog that uses both eliminates repetition at both levels:

```yaml
notes:
  functions:
    - name: inBusinessHours
      expression: '{{ and weekday (timeInWindow "09:00" "18:00") }}'

profiles:
  reconciler:
    - name: api-service
      description: Balanced for a standard web service operator
      workers: 4
      resync: 30s
      queue:
        maxDepth: 200

crds:
  app-eu:
    reconciler:
      profile: api-service
  app-us:
  reconciler:
      profile: api-service
```

The business-hours logic is declared once as a note. The api-service structure is declared once as a profile. Both CRDs get identical behaviour from one source of truth.

---

## Related topics

- [Notes](../notes/index.md) — full contract: built-in functions, purity, nil-safety, and resolver behaviour
- [Profiles](../profiles/index.md) — all built-in profiles and how to define your own
- [Time-Dependent Workloads](../temporal/index.md) — time notes and business-hours patterns in practice
