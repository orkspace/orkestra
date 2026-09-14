# From controller-runtime to Orkestra

You have a working Kubernetes operator. It reconciles. You've been here.

The question is not whether controller-runtime works — it does. The question is what it costs: informers, workqueues, worker pools, leader election, status, finalizers, events, metrics, webhooks — written from scratch for every CRD. The behaviour is always small. The machinery is always the same.

Orkestra removes the machinery. This pack shows the same WebApp operator expressed multiple ways so you can see what you are choosing between.

```bash
ork init --pack from-controller-runtime
```

---

## The operator

A `WebApp` CRD. When a CR is applied, the operator creates a Deployment and a Service, then writes status. The behaviour is small — the migration story is about the machinery around it.

---

## The options

### [00 — controller-runtime baseline](../from-controller-runtime/00-controller-runtime-baseline/README.md)

The starting point. A standard controller-runtime operator: `Reconcile(ctx, req)`, `SetupWithManager`, scheme registration, `main.go`, Dockerfile, Helm chart. About 150 lines of Go not counting everything around it.

```
00-controller-runtime-baseline/
```

---

### [01 — declarative (zero Go)](../from-controller-runtime/01-declarative/README.md)

The same WebApp operator as a pure Katalog. No Go, no custom binary, no image to build. Orkestra's runtime creates the Deployment and Service from declared templates.

Pick this when your operator only creates Kubernetes resources and applies rules.

```
01-declarative/
```

---

### [02 — hybrid (recommended if using hooks)](../from-controller-runtime/02-hybrid/README.md)

The Deployment is declared in the Katalog. The Service is created in Go with type-safe access to `obj.Spec.Port`. Orkestra runs declared templates first, then the hook adds what templates cannot express.

This is the 90/10 pattern: declare what Orkestra handles well, write Go for the rest.

```
02-hybrid/
```

---

### [03 — hooks only](../from-controller-runtime/03-hooks-only/README.md)

Both resources — Deployment and Service — are created in Go. No declared templates alongside the hook. Use when every resource requires computed logic that templates cannot express, or when type-safe control over the full spec matters more than keeping YAML declarations.

```
03-hooks-only/
```

---

### [04 — constructor: zero changes](../from-controller-runtime/04-constructor-migration/README.md)

The migration path. The existing `Reconcile` method is completely untouched — same signature, same body, same return types. Two lines wire it into Orkestra:

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

Remove `SetupWithManager`, `Scheme`, and `main.go`. Everything inside `Reconcile` — `r.Get`, `r.Create`, `r.Status().Update()` — stays unchanged. Orkestra provides the informer, workqueue, worker pool, leader election, panic recovery, and metrics.

```
04-constructor-migration/
```

---

### [05 — constructor: Orkestra resources](../from-controller-runtime/05-constructor-orkestra-resources/README.md)

Same constructor, but replace the manual Get / IsNotFound / Create / Patch pattern with the `pkg/resources` library:

```go
// Before — manual
existing := &appsv1.Deployment{}
err := r.kube.Get(ctx, namespace, name, existing)
if errors.IsNotFound(err) { return r.kube.Create(ctx, desired) }
patch := client.MergeFrom(existing.DeepCopy())
existing.Spec = desired.Spec
return r.kube.Patch(ctx, existing, patch)

// After — Orkestra resources
return orkdeploy.Update(ctx, kube, obj, spec)
```

`Update` handles create-if-absent, drift correction, owner references, and system labels. `DeleteIfOwned` is a no-op if the resource does not exist or belongs to a different CR.

```
05-constructor-orkestra-resources/
```

---

### [06 — ork migrate](../from-controller-runtime/06-ork-migrate/README.md)

Run `ork migrate` against the `00-controller-runtime-baseline` controller and see the constructor path generated automatically. Work through the flagged TODOs, then build and simulate.

```
06-ork-migrate/
```

---

### [07 — all options](../from-controller-runtime/07-all-options/README.md)

All five patterns running in one binary via Komposer. Each CRD has its own reconcile loop, queue, and worker pool. `hybridApp` and `hooksApp` wait for `declarativeApp` before activating.

Use this to compare patterns side by side, or as the starting point before distributing each katalog independently via OCI.

```
07-all-options/
```

---

## Choosing

| | Go required | Custom binary | What you own |
|---|---|---|---|
| **01 declarative** | No | No | Nothing — pure YAML |
| **02 hybrid** | Yes — hook only | Yes | The 10% templates can't express |
| **03 hooks only** | Yes — all resources | Yes | All child resource specs in Go |
| **04 constructor migration** | Yes — full reconciler | Yes | Reconcile unchanged; manager removed |
| **05 constructor resources** | Yes — full reconciler | Yes | Reconcile logic; resource ops simplified |
| **06 ork migrate** | Yes — tool generates it | Yes | Starting from an existing operator |
| **07 all options** | Depends on options used | Yes | All five patterns in one Komposer |

Declarative runs on the standard Orkestra runtime. All typed options require a custom runtime binary — that is what the choice costs.

---

## Running an example

```bash
ork init --pack from-controller-runtime
cd from-controller-runtime/01-declarative
ork run --dev
```

Typed operators need a build step first:

```bash
cd from-controller-runtime/02-hybrid
make registry && make build
ork run --dev
```

---

For a deeper dive into typed operators in production:

```bash
ork init --pack advanced/09-hooks
ork init --pack advanced/10-constructor
ork init --pack advanced/11-mixed-operator-pattern
```
