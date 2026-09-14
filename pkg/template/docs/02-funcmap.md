# FuncMap — functions available in template expressions

Every resolver carries a `template.FuncMap` that makes functions callable in `{{ }}` expressions. Three layers compose the FuncMap:

## 1. Built-in KEL catalog

`pkg/note` registers ~257 functions covering strings, math, time, Kubernetes conditions, replicas, pods, jobs, services, ingress, HPA, PVC, Prometheus, StatefulSets, and more. These are available in every expression in every context — admission, preReconcile, reconcile, E2E assertions, and the CLI.

```text
{{ hasCrashingPod .children.deployment }}
{{ timeSince (creationTimestamp .children.deployment) }}
{{ allReplicasReady .children.deployment }}
{{ inBusinessHours }}
```

Browse the catalog: `ork notes search <keyword>`

## 2. User-defined notes

The `notes:` block in a Katalog lets teams define their own primitives. Each note's expression is compiled as a Go template and registered under its name in the FuncMap. User notes compose with built-in notes.

```yaml
notes:
  isProduction:
    expression: '{{ eq .spec.environment "production" }}'
  deploymentReady:
    expression: '{{ allReplicasReady .children.deployment }}'
```

Registered via `resolver.WithUserNotes(reg)`. Available in any expression in that Katalog.

## 3. Sentinels

Sentinel names declared in `enqueueGate:` or `reconcileGate:` are injected as no-arg functions via `resolver.WithSentinels(declared, values)`.

```yaml
enqueueGate:
  sentinels: [generationChanged, labelsChanged]
  when:
    - field: "{{ generationChanged }}"
      equals: "true"
```

At validate time, sentinels are registered as stubs returning `""` — parse checking only, no execution. At runtime, they return their computed value from the informer's UpdateFunc comparison.

Referencing an undeclared sentinel name causes a parse error at validate time — this is how `ork validate` catches sentinel misuse before the operator runs.

→ [pkg/runtime/sentinel](../../runtime/sentinel/README.md)

---
→ Next: [Evaluation](03-evaluation.md)