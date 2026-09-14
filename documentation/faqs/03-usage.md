# Usage

## Can Orkestra manage built-in Kubernetes resources?

Yes. `kind: Deployment`, `kind: Pod`, `kind: Service`, and 30+ other built-in
Kubernetes kinds are supported without declaring group, version, or plural —
Orkestra enriches them automatically from its internal registry:

```yaml
- name: deployment-governance
  apiTypes:
    kind: Deployment   # ← only field needed for built-in kinds
  validation:
    - field: metadata.labels.team
      operator: exists
      message: "all deployments must declare a team owner"
      action: warn
```

!!! tip "Governance without a separate policy engine"
    This is how you apply governance to Kubernetes built-in resources without
    OPA, Kyverno, or a separate admission controller. Orkestra watches the resource,
    validates it at reconcile time, and optionally intercepts at admission time
    when `ENABLE_ADMISSION_WEBHOOK=true` or `security.webhooks.admission.enabled=true`.

Run `ork validate --full` to see exactly what Orkestra resolves for a kind-only declaration.

---

## What is the difference between validation and mutation?

**Validation** evaluates rules against a CR and either blocks it (`action: deny`)
or surfaces an advisory (`action: warn`).

**Mutation** applies defaults and normalisations to a CR before it is stored. Fields
declared with `default:` are set only when absent. Fields declared with `override:`
are always set.

Both run at two points:

- **Admission time** — when `ENABLE_ADMISSION_WEBHOOK=true`, synchronously during `kubectl apply`
- **Reconcile time** — every reconcile cycle, regardless of webhook configuration

Declare once, enforced at both points.

!!! tip "Roll out rules safely"
    Deploy new validation rules with `action: warn` first. Observe
    `controller_admission_validation_violations_total` in Prometheus to understand
    which CRs would be affected. When you are confident, change to `action: deny`.
    The Katalog change takes effect on the next Orkestra restart.

---

## Does `ENABLE_ADMISSION_WEBHOOK=true` block the API server if Orkestra is down?

No — by design. The webhook configuration uses `FailurePolicy: Ignore` by default.
If Orkestra is unreachable when the API server calls `/validate` or `/mutate`, the
operation is allowed through. Validation catches violations at reconcile time when
Orkestra restarts.

```yaml
# To change to blocking behaviour (requires high-availability Orkestra deployment):
webhooks:
  failurePolicy: Fail    # default: Ignore
```

!!! warning "Before setting Fail"
    `FailurePolicy: Fail` means Orkestra's availability directly gates all CR
    deployments. Set it only with multiple Orkestra replicas, a PodDisruptionBudget,
    and confidence that your admission rules are correct. Start with `Ignore`.

---

## How do I use `when:` conditions?

`when:` creates a resource only when a condition is met:

```yaml
services:
  - name: "{{ .metadata.name }}-public"
    port: "80"
    targetPort: "{{ .spec.port }}"
    when:
      - field: spec.exposePublicly
        equals: "true"
```

Multiple conditions in the same `when:` block are ANDed. To express OR, create two
separate resource entries with different `when:` conditions and the same name — only
one will be created, whichever condition is met first.

See [When Conditions](../reference/schema/02-katalog/06-when-conditions.md) for the full reference.

---

## How does `dependsOn` ordering work?

`dependsOn` tells Orkestra to hold one CRD's workers until another reaches a condition:

```yaml
spec:
  crds:
    database:
      operatorBox:
        reconciler:
          workers: 8
    application:
      dependsOn:
        database:
          condition: healthy   # wait until database workers are running and failure-free
    analytics:
      dependsOn:
        database:
          condition: started   # wait only until database workers are running
```

`condition: healthy` waits until the dependency's workers are running and its consecutive failure count is zero. `condition: started` waits only until workers are running — useful when you want ordering without blocking on health.

The bare list form defaults to `started`:

```yaml
dependsOn:
  - database
```

See [CRD Entry Schema](../reference/schema/02-katalog/02-crd-entry.md) for the full reference.

---

## What does `forEach` do?

`forEach` expands one resource declaration into N resources — one per element in a list field on the CR spec. Each iteration element is bound to a name and available as a template variable alongside the parent CR's fields.

```yaml
# CR spec
spec:
  replicationFactor: 3
  shards:
    - name: us-east
      region: us-east-1
      size: 50Gi
    - name: eu-west
      region: eu-west-1
      size: 50Gi

# Katalog
onCreate:
  custom:
    - apiVersion: storage.example.io/v1alpha1
      kind: Shard
      forEach:
        field: spec.shards   # iterate over this list
        as: shard            # bind each element as .shard
      metadata:
        name: "{{ .metadata.name }}-{{ .shard.name }}"
        namespace: "{{ .metadata.namespace }}"
        namespaced: true
      spec:
        region: "{{ .shard.region }}"
        size: "{{ .shard.size }}"
        replicationFactor: "{{ .spec.replicationFactor }}"
```

This creates two `Shard` CRs — `my-store-us-east` and `my-store-eu-west` — from a single declaration. If the parent CR's `spec.shards` is updated at runtime, Orkestra adds new entries and deletes removed ones on the next reconcile.

`forEach` works on the `custom:` block (Custom Resources) and on standard resource blocks (`deployments:`, `services:`, etc.).

Try it:

```bash
ork init --pack advanced/16-custom-resources
cd 05-forEach-sharding
# Follow the steps in README
```

---

## What is the difference between `normalize:` and `conversion.paths:`?

Both use note expressions to transform CR fields, but they solve different problems.

**`normalize:`** — runs in-memory before every reconcile. The raw CR in etcd is unchanged. Kubernetes never sees two versions of the CRD. Use it when you want to tolerate input shape variation within a single schema version:

```yaml
normalize:
  spec:
    schedule: "{{ cronFromAny .spec.schedule }}"
    # accepts both "*/5 * * * *" and {minute:"*/5", hour:"*",...}
    # downstream templates always see a plain cron string
```

No webhook registration. No TLS. No infrastructure cost.

**`conversion.paths:`** — runs in the gateway's `/convert` HTTPS endpoint, called by the Kubernetes API server. Use it when you have a real multi-version CRD (`v1`, `v2`) and need Kubernetes to translate between stored and requested versions:

```yaml
conversion:
  storageVersion: v1
  updateCRD: true
  paths:
    - from: v1
      to: v2
      spec:
        schedule: "{{ cronToMap .spec.schedule }}"
```

The rule: if you control the CRD and want to tolerate different input shapes — `normalize:`. If you have committed to multiple API versions and need the API server to convert between them — `conversion.paths:`.

→ [Schema Evolution](../concepts/schema-evolution/index.md)

---

## When do I need Go hooks?

The `external:` block handles HTTP already — GET, POST, bearer tokens, chained calls where each result flows into the next via `.external.{name}.body`. `when:` and `or:` handle conditional logic. For most operators, those are enough.

Hooks become necessary when the work is genuinely outside HTTP:

- **Non-HTTP protocols** — gRPC, database connections, message queue operations, anything that isn't an HTTP endpoint
- **Complex computed fields from non-HTTP sources** — deriving values that require SDK calls, database queries, or binary protocols where there is no HTTP endpoint to call

For AWS, GCP, and Stripe — providers are in development for common SDK integrations (`aws:`, `gcp:`, `stripe:` blocks in the Katalog). Until a provider covers your case, hooks are the path.

Hooks are additive: the hook runs, then Orkestra applies `onCreate`/`onReconcile` templates as normal. The template layer does not disappear.

```yaml
operatorBox:
  reconciler:
    hooks:
      location: github.com/myorg/database-operator/hooks
      function: DatabaseHooks
```

Try it:

```bash
ork init --pack advanced/09-hooks
cd 09-hooks
# Follow the steps in README
```

→ [Typed Operators — Hooks](../concepts/typed-operators/01-hooks.md) · [Migration Guide — Hybrid](../guides/migration/03-hybrid.md) · [Migration Guide — Hooks Only](../guides/migration/04-hooks-only.md)

---

## When do I need a constructor?

When you need to own the full reconcile loop — typically when integrating an existing controller-runtime operator without rewriting it.

```yaml
operatorBox:
  reconciler:
    default: false   # GenericReconciler is replaced; constructor owns everything
```

`reconciler.default: false` is the one field change. Your constructor receives `kubeclient.Interface` — Orkestra's single interface for informer, kube calls, events, and args. If you are migrating from controller-runtime, `orkadapter.ToClient(kube)` returns a `client.Client` so your existing `Reconcile` body compiles unchanged. `domain.ReconcilerFrom` adapts the `ctrl.Request` signature.

Declarative templates (`onCreate`, `onReconcile`, `status.fields`) are not applied when `reconciler.default: false` — the constructor is responsible for all state.

Try it:

```bash
ork init --pack advanced/10-constructor
cd 10-constructor
# Follow the steps in README
```

→ [Typed Operators — Constructor](../concepts/typed-operators/02-constructor.md) · [Migration Guide — Constructor](../guides/migration/05-constructor.md) · [ork migrate](../guides/migration/07-ork-migrate.md)

---

## Can I mix declarative, hooks, and constructor operators in one binary?

Yes. Each CRD in a Katalog or Komposer can use a different mode. One binary, one process:

```yaml
# komposer.yaml
spec:
  crds:
    database:          # typed hooks — Go SDK calls alongside declarative templates
      operatorBox:
        reconciler:
          workers: 5
    website:           # dynamic — pure YAML, no Go
      dependsOn:
        database:
          condition: healthy
    pipeline:          # constructor — full custom reconciler
      dependsOn:
        website:
          condition: started
```

You promote along a single axis: start pure YAML, add `reconciler.hooks:` when you need Go logic, flip `reconciler.default: false` when you need full control. No project restructure. No framework switch.

Try it:

```bash
ork init --pack advanced/11-mixed-operator-pattern
cd 11-mixed-operator-pattern
# Follow the steps in README
```

→ [Typed Operators — Mixing all three](../concepts/typed-operators/03-mixed.md)

---

## Will my CR be accepted by the webhook?

```bash
ork gate -f katalog.yaml --cr cr.yaml
```

Exit code 0 means all deny rules pass. Exit code 1 means at least one deny rule fired — the output names exactly which field and why. No cluster, no deployment, no TLS required.

---

## Why was my CR rejected?

Run `ork gate` against the CR. The output prints every fired rule, the field path, and the message declared in the Katalog:

```text
  ✗ spec.image  images must come from the internal registry (registry.internal/)
```

No logs to dig through, no cluster events to inspect.

---

## What defaults will be applied to my CR before it is stored?

`ork gate` previews every mutation that would fire:

```text
  ↳ spec.replicas     (absent) → 2    default
  ↳ spec.environment  (absent) → development  default
  ↳ spec.rateLimit    (absent) → 100  default
  ✓ 3 mutations would be applied
```

This is the state the validation webhook sees — and the state the object is stored with — after the mutation webhook runs.

---

## Does `mutateFirst: true` affect which validation rules fire?

Yes, and `ork gate` simulates it. When `mutateFirst: true` is set, `ork gate` applies mutations to a copy of the CR first, then runs validation on the mutated copy — exactly as the real webhook pipeline does. A CR missing a required field can pass validation if a mutation rule fills it in before validation runs.

---

## My CR passes `ork gate` but the webhook still rejects it. Why?

Two rule types are skipped locally:

- `operator: unique` — requires a live informer cache to check whether another CR already holds the value
- `external: <endpoint>` — requires the external endpoint to be reachable

`ork gate` prints a note for each skipped rule. If your Katalog declares either of these, verify those specific rules against a cluster.

---

## Can I test against multiple CRs in one file?

Yes. `ork gate` accepts a multi-document YAML file (documents separated by `---`). Each document is matched to its CRD by `kind` and evaluated independently.

---

## I changed a validation rule. How do I quickly check all my test CRs?

```bash
for cr in cr-*.yaml; do
  echo "── $cr"
  ork gate -f katalog.yaml --cr "$cr"
done
```

Each run exits non-zero on denial, so you can wrap this in `set -e` or pipe into a CI step.

---

## Further reading

- **[Running](./02-running.md)** — setup, configuration, and operations
- **[Ecosystem](./04-ecosystem.md)** — comparisons and the Kubernetes roadmap
