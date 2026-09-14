# operatorBox

Defines the reconciliation strategy and lifecycle configuration for a CRD. Controls which reconciler implementation runs and how resources, status, admission, autoscaling, and rollback behave.

```yaml
operatorBox:
  # reconciler: determines which reconciler implementation runs.
  # Omit entirely for declarative-only CRDs (GenericReconciler is the default).
  reconciler:
    default: true              # true → GenericReconciler | false → custom constructor

    # Go hooks (default: true, typed mode)
    hooks:
      location: github.com/example/operator
      function: DatabaseHooks
      alias: dbhooks
      resources:
        - kind: StatefulSet
        - kind: Service
      args:
        readReplicaCount: 2
        backupEnabled: true

    # Custom reconciler (default: false)
    constructor:
      location: github.com/example/operator
      function: NewDatabaseReconciler
      alias: dbreconciler
      resources:
        - kind: StatefulSet
        - kind: Service
      args:
        maxRetries: 3
        timeoutSeconds: 300

  finalizers:
    - example.io/cleanup

  # Declarative templates (GenericReconciler only)
  onCreate:
    ...
  onReconcile:
    ...
  onDelete:
    ...

  status:
    ...               # → status.md

  when:
    ...               # → when-conditions.md

  observe:            # → observe
  preReconcile:
    external:         # → preReconcile.external section below (shared calls)
      - ...
    enqueueGate:      # → preReconcile.enqueueGate section below
      external:
        - ...
      when:
        - ...
    reconcileGate:    # → preReconcile.reconcileGate section below
      eventAware:
      external:
        - ...
      when:
        - ...
      or:
        - ...

  rollBackOnError: false
  autoscale:
    ...
```

## `reconciler`

Groups the reconciler identity fields. Omit for declarative-only CRDs — GenericReconciler is the default.

### `reconciler.include`

Loads a shared reconciler config from a file. The file's `reconciler:` block is merged under the inline config — inline fields take precedence over included ones. The path is resolved relative to the Katalog file. Cleared after expansion.

Use this to share hooks location, function, resources, and tuning across targets that only differ in `args` or `preReconcile`:

```yaml
# katalog.yaml
serve:
  target:
    v2-enabled:
      operatorBox:
        reconciler:
          include: ./shared-reconciler.yaml
          hooks:
            args:
              featureEnabled: "true"

    v2-disabled:
      operatorBox:
        reconciler:
          include: ./shared-reconciler.yaml
          hooks:
            args:
              featureEnabled: "false"
```

```yaml
# shared-reconciler.yaml
reconciler:
  hooks:
    location: github.com/myorg/operator/hooks
    function: AppHooks
    resources:
      - kind: Deployment
      - kind: Service
  workers: 3
  resync: 30s
```

Inline `hooks.args` overrides anything declared in the file's `hooks.args`. The location, function, resources, workers, and resync are inherited from the file.

### `reconciler.default`

| Value | Behaviour |
|-------|-----------|
| `true` (default) | GenericReconciler handles reconciliation. Use `onCreate`, `onReconcile`, `onDelete` for declarative templates, and `reconciler.hooks` for Go hooks. |
| `false` | Fully custom reconciler. Set `reconciler.constructor` to provide it. Templates and hooks are ignored. |

### `reconciler.hooks`

A Go function invoked by the GenericReconciler. Implements typed reconcile hooks (`OnCreate`, `OnUpdate`, `OnDelete`). Used when you need Go logic that the GenericReconciler calls instead of declarative templates.

```yaml
operatorBox:
  reconciler:
    hooks:
      location: github.com/example/operator   # Go module path
      function: DatabaseHooks                  # exported function name
      alias: dbhooks                           # import alias (auto-derived if omitted)
      runHooksFirst: false                     # see below
      managedResources:                        # RBAC + implicit watch informer per type
        - kind: StatefulSet
        - kind: Service
        - kind: CronJob
      args:                                    # arbitrary key/value pairs — see below
        readReplicaCount: 2
        backupEnabled: true
        replicationMode: async
```

Requires typed mode (`apiTypes.location` set) and `ork generate registry`.

#### `reconciler.hooks.args`

Key/value pairs declared in the Katalog and delivered to the hook function at reconcile time via `kube.Args()`. Values may be strings, booleans, integers, or nested maps.

**String values support Go template expressions.** The GenericReconciler evaluates them against the current CR before the hook runs — the full note FuncMap is available (`default`, `upper`, `lower`, etc.).

```yaml
hooks:
  args:
    # Static — passed through as-is; type is preserved.
    readReplicaCount: 2
    backupEnabled: true
    replicationMode: async
    # Dynamic — evaluated per-CR at reconcile time.
    region: "{{ default \"us-east-1\" .spec.region }}"
    backupScheduleHour: "{{ default \"2\" .spec.backupScheduleHour }}"
    database:
      engine: "{{ default \"postgres\" .spec.engine }}"
      version: "{{ default \"14\" .spec.version }}"
```

The hook sees fully-resolved values — no template syntax, no extra wiring:

```go
func onReconcile(ctx context.Context, obj *apiv1.Database) error {
    kube, _ := kubeclient.FromContext(ctx)
    region          := kube.Args().String("region")           // "eu-west-1" (from spec) or "us-east-1" (default)
    replicas        := kube.Args().Int("readReplicaCount")    // 2
    backupEnabled   := kube.Args().Bool("backupEnabled")      // true
    db              := kube.Args().Sub("database")
    engine          := db.String("engine")                    // "postgres" or spec value
    _ = region; _ = replicas; _ = backupEnabled; _ = engine
    return nil
}
```

Or bind the whole map to a typed struct:

```go
type HookArgs struct {
    Region           string `json:"region"`
    ReadReplicaCount int    `json:"readReplicaCount"`
    BackupEnabled    bool   `json:"backupEnabled"`
}

func onReconcile(ctx context.Context, obj *apiv1.Database) error {
    kube, _ := kubeclient.FromContext(ctx)
    var cfg HookArgs
    if err := kube.Args().BindArgs(&cfg); err != nil {
        return err
    }
    // use cfg.Region, cfg.ReadReplicaCount, cfg.BackupEnabled
    return nil
}
```

`kube.Args()` always returns a non-nil `Args` — absent keys return zero values, so no nil checks are needed.

#### `reconciler.hooks.external`

HTTP calls the runtime makes **before** the hook runs. Results are injected into the resolver so their values are available as `args` template expressions (`{{ .external.<name>.body }}`, `.status`, `.headers`). This keeps the hook free of HTTP client code — the Katalog owns the call, the hook receives the resolved value via `kube.Args()`.

```yaml
hooks:
  external:
    - name: flags
      url: "{{ .spec.serviceUrl }}/flags/{{ .metadata.name }}/v2Enabled"
      method: GET
      continueOnError: true
      timeout: 5s
      when:
        - field: '{{ inBusinessHours }}'
          equals: "true"
  args:
    featureEnabled: '{{ .external.flags.body }}'
    inBusinessHours: '{{ inBusinessHours }}'
```

The hook reads the resolved value with no HTTP logic:

```go
func onReconcile(ctx context.Context, obj *apiv1.App) error {
    kube, _ := kubeclient.FromContext(ctx)
    featureEnabled   := kube.Args().String("featureEnabled") == "true"
    inBusinessHours  := kube.Args().String("inBusinessHours") == "true"
    // decision logic — no http.Get, no time.Now
    return nil
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Key used in `.external.<name>.*` template expressions. |
| `url` | yes | URL to call. Supports template expressions evaluated against the current CR. |
| `method` | no | HTTP method. Default `GET`. |
| `timeout` | no | Per-call timeout. Default `5s`. |
| `continueOnError` | no | When `true`, a failed call leaves `.external.<name>.body` empty rather than aborting reconciliation. Default `false`. |
| `when` | no | AND-gate conditions. The call is skipped when any condition is false. Same `[]Condition` type as Katalog `when:` blocks — see [conditions reference](06-when-conditions.md). |
| `or` | no | OR-gate conditions. The call is skipped when no condition is true. |

The full `external:` field reference (shared with the top-level `external:` block) is in [13-external.md](13-external.md).

#### `reconciler.hooks.runHooksFirst`

Controls the order in which the hook and declared templates run within the same reconcile cycle.

| Value | Order |
|-------|-------|
| `false` (default) | Declared templates run first, then the hook. Use when the hook is additive — the ServiceAccount or Deployment already exists when the hook runs. |
| `true` | Hook runs first, then declared templates. Use when the hook creates resources that declared templates depend on. |

```yaml
reconciler:
  hooks:
    runHooksFirst: true   # hook → then declared templates
                          # false (default): declared templates → then hook
```

### `reconciler.constructor`

Replaces the GenericReconciler entirely. Requires `reconciler.default: false`.

```yaml
operatorBox:
  reconciler:
    default: false
    constructor:
      location: github.com/example/operator
      function: NewDatabaseReconciler
      alias: dbreconciler
      managedResources:        # RBAC + implicit watch informer per type
        - kind: StatefulSet
        - kind: Service
      args:
        maxRetries: 3
        timeoutSeconds: 300
        notifyOnSuccess: true
```

#### `reconciler.constructor.managedResources`

Declares the Kubernetes resource types this constructor creates, updates, or deletes. Two things happen for each entry:

- **RBAC** — `get/list/watch/create/update/patch/delete` permissions are generated for the operator ServiceAccount.
- **Implicit watch** — Orkestra starts a watch informer for the type. After the informer syncs, `r.client.Get` and `r.client.List` for that type are served from cache. When an owned resource changes and has an ownerReference pointing to the primary CR, Orkestra re-enqueues it automatically — no explicit `watch:` entry needed.

If you need a field index, event filtering, or a different key resolution strategy, declare an explicit `watch:` entry for that type. It takes priority over the implicit informer from `managedResources:`.

#### `reconciler.constructor.args`

Key/value pairs delivered to the constructor function via `kube.Args()`. The constructor receives `kube` with args already attached — no additional wiring required.

String values support Go template expressions, but because the constructor owns its reconcile loop, it evaluates them itself by calling `kube.ScopedFor(resolver.TemplateEvaluator())` after building its own resolver:

```go
func (r *PipelineReconciler) Reconcile(ctx context.Context, obj domain.Object) error {
    resolver := template.NewResolver(ctx, obj)
    kube := r.kube.ScopedFor(resolver.TemplateEvaluator())  // resolve {{ }} in args
    ns         := kube.Args().String("namespace")            // now the CR's namespace
    source     := kube.Args().String("source")               // upper-cased from spec
    maxRetries := kube.Args().Int("maxRetries")              // static, passes through
    _ = ns; _ = source; _ = maxRetries
    return nil
}
```

Integers and booleans have no template syntax — YAML parsed them as native types, so `ScopedFor` returns them as-is. Nested maps are recursed into: every string inside a nested map is evaluated; the map container itself is not a template.

`args` follow the same accessor rules as `reconciler.hooks.args` — see above.

## `finalizers`

Per-CRD finalizers. Overrides `spec.finalizers` for this CRD only.

## `onCreate` / `onReconcile` / `onDelete`

Declarative resource templates evaluated during reconcile phases:

| Hook | When it runs |
|------|-------------|
| `onCreate` | CR transitions from Pending → Active (first reconcile) |
| `onReconcile` | Every reconcile cycle — drift correction |
| `onDelete` | Before finalizer is removed — cleanup |

```yaml
onCreate:
  deployments:
    - name: "{{ .Name }}-server"
      image: postgres:14
      env:
        - name: POSTGRES_DB
          value: "{{ .Spec.Database }}"
  services:
    - name: "{{ .Name }}-svc"
      port: 5432

onDelete:
  jobs:
    - name: "{{ .Name }}-cleanup"
      image: postgres:14
      command: ["./cleanup.sh"]
```

Available resource types: `deployments`, `services`, `configmaps`, `secrets`, `jobs`, `cronjobs`, `statefulsets`, `ingresses`, `serviceaccounts`, `roles`, `rolebindings`, `pvcs`, `pdbs`, `hpas`, `namespaces`.

Templates are Go templates evaluated against the CR object. Use `{{ .Name }}`, `{{ .Namespace }}`, `{{ .Spec.* }}`, `{{ .Status.* }}`.

## `observe`

Declares the resources and Kubernetes Events Orkestra observes for changes that may trigger reconciliation.

* **`watch`** — declares secondary Kubernetes resources to observe and the primary resources they can trigger.
* **`events`** — declares Kubernetes Events to observe and the primary resources they can trigger.

See the dedicated references for the full declaration syntax and field details:

* [`watch`](27-watch.md)
* [`events`](28-events.md)

## `preReconcile`

Pre-reconcile gate conditions. Two sub-blocks control where in the pipeline the gate fires:

- **`enqueueGate`** — evaluated by the informer before the item enters the work queue.
- **`reconcileGate`** — evaluated by the kordinator after the item is dequeued, before the reconciler is called.

`external:` calls can be declared at the `preReconcile:` level (shared, available to both gates) or inside either gate (gate-specific). Calls run in order — shared first, then gate-level. Results accumulate in the resolver under `.external.<name>.*` and are available to subsequent calls and `when:`/`or:` conditions.

```yaml
operatorBox:
  preReconcile:
    external:                          # shared — results available to both gates
      - name: featureFlag
        url: "{{ .spec.flagServiceUrl }}/{{ .metadata.name }}"
    enqueueGate:
      external:                        # gate-specific, runs after shared
        - name: quota
          url: "{{ .spec.quotaUrl }}"
      when:
        - field: "{{ .external.featureFlag.body }}"
          equals: "true"
    reconcileGate:
      when:
        - field: "{{ .spec.enabled }}"
          equals: "true"
        - field: "{{ .external.quota.body }}"
          equals: "available"
      or:
        - field: "{{ .spec.environment }}"
          equals: "production"
        - field: "{{ .spec.environment }}"
          equals: "staging"
```

### `preReconcile.sentinels`

A list of sentinels available for use in this CRD preReconcile gates is declared here. Once declared, they can be used in enqueue and reconcile gates.

```yaml
operatorBox:
  preReconcile:
    sentinels:
      - generationChanged
      - labelsChanged
      - annotationsChanged
```


### `preReconcile.enqueueGate`

Evaluated by the **informer** in `handleEvent` before the item enters the work queue. When the gate fires the object is silently dropped — it never reaches the kordinator or reconciler.

| Property | Behavior |
|---|---|
| **Phase** | Before the item enters the queue |
| **On gate** | Object dropped. No queue pressure. No kordinator overhead. |
| **Health state** | No effect — kordinator is never involved |
| **Resolver** | Full chain — CR fields, profiles, notes, serve intent, external results |
| **Caveat** | If the gating field changes without a watch event (e.g. resync only), the object stays out until the next event arrives |

### `preReconcile.reconcileGate`

Evaluated by the **kordinator** after the item is dequeued. When conditions fail, the item is discarded without calling the reconciler and the CRD reports health state `gated`.

| Property         | Behavior                                                                                                                                        |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| **Phase**        | After dequeue, before the reconciler runs                                                                                                       |
| **On gate**      | Item dropped. No error. No status write.                                                                                                        |
| **Health state** | `gated` — idle, not degraded. Clears on next successful reconcile.                                                                              |
| **Resolver**     | Full chain — CR fields, profiles, notes, serve intent, external results                                                                         |
| **On CR update** | Object is re-enqueued; gate is re-evaluated against the current object state                                                                    |
| **Sentinels**    | May reference event-time sentinel values declared by `preReconcile.sentinels`                                                                   |
| **`eventAware`** | When `true`, preserves individual event identity through the queue so the gate is evaluated against the sentinel context of that specific event |
| **Default**      | `false` — normal workqueue coalescing applies                                                                                                   |

!!! tip "Event-aware reconciliation"
    `eventAware: true` is an explicit opt-in for event-preserving reconcile-gate evaluation. Without it, multiple updates for the same object may be coalesced by the workqueue before reconciliation, so sentinel conditions are evaluated using the sentinel context associated with the surviving queued item. With `eventAware: true`, each admitted event receives a distinct queue identity and retains its own sentinel context through dequeue and gate evaluation.

!!! note "Additional Note"
    `eventAware` applies to the **entire `reconcileGate` evaluation**, including `when`, `or`, `external`, and sentinel conditions. It does not mean that the reconciler receives a historical copy of the object; reconciliation still operates on the current object state.
    Use when event-specific gate evaluation is required and the **additional reconcile cycles are acceptable**.


### `preReconcile.external`

HTTP or gRPC calls declared here run before either gate. Results are available to both `enqueueGate` and `reconcileGate` conditions. Follows the same `external:` contract as `reconciler.hooks.external` — see [external reference](13-external.md).

### Comparison

| | `preReconcile.enqueueGate` | `preReconcile.reconcileGate` | Resource `when:` |
|---|---|---|---|
| Evaluated by | Informer (`handleEvent`) | Kordinator (after dequeue) | Reconciler (inside loop) |
| Phase | Before queue entry | After dequeue | Inside reconcile cycle |
| Effect | Object never queued | Reconcile cycle skipped | Individual resource skipped |
| Health on gate | No effect | `gated` (idle) | No effect |
| Supports `external:` | Yes | Yes | Yes |

All [condition operators](06-when-conditions.md#operators) are supported. `when:` requires ALL conditions to pass (AND). `or:` requires at least one (OR). Both may be specified simultaneously — both must pass.

See [Conditional Reconciliation](../../../concepts/conditional/04-conditional-reconciliation.md) for the full concept guide.

---

## `rollBackOnError`

Zero-config rollback on reconcile failure. Restores the previous known-good state when a reconcile cycle errors.

```yaml
rollBackOnError: true
```

## `autoscale`

Dynamically adjusts worker count, queue depth, and resync interval based on conditions.

```yaml
autoscale:
  interval: 15s
  cooldown: 2m
  conditions:
    when:
      - field: status.queueDepth
        operator: gt
        value: "100"
        valueType: int
  do:
    workers: 5
    queueDepth: 500
    resync: 10s
```

| Field | Description |
|-------|-------------|
| `interval` | Evaluation frequency (default: `15s`) |
| `cooldown` | Min time conditions must be false before restoring baseline (default: `2m`) |
| `conditions.when` | AND conditions — all must be true |
| `conditions.or` | OR conditions — at least one must be true |
| `do.workers` | Override concurrent goroutines when conditions are met |
| `do.queueDepth` | Override max queue depth |
| `do.resync` | Override resync interval |

---
