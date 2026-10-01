# Kubernetes, operators, and Orkestra

!!! tip "Already know Kubernetes?"
    For engineers who are new to Kubernetes or want a clear mental model before writing their first Katalog. If you already know what CRDs, operators, and reconciliation are, go straight to [Learning to Orkestrate](./01-learning-to-orkestrate/index.md).

---

## What Kubernetes is

Kubernetes is a platform for running software in containers. You describe what should exist, and Kubernetes continuously works to make the cluster match that description.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  replicas: 3
  # ...
```

Kubernetes reads that file and keeps three copies of `my-app` running. If one crashes, Kubernetes starts a new one. If you change `replicas` to five, Kubernetes starts two more. You declare the desired state — Kubernetes figures out how.

---

## Built-in resource types

Kubernetes ships with a set of built-in kinds:

| Kind | What it is |
|---|---|
| `Deployment` | A set of identical pod replicas that Kubernetes keeps running |
| `Service` | A stable network address that routes to a set of pods |
| `ConfigMap` | Configuration data that pods can read |
| `Secret` | Sensitive data — passwords, tokens — that pods can read |
| `Namespace` | A logical boundary for grouping resources |
| `Pod` | One or more containers running together on a node |

Each kind is stored in Kubernetes, validated against a schema, and watched by a built-in controller that keeps the cluster in the desired state.

---

## What a CRD is

`CRD` stands for Custom Resource Definition. It is how you add your own resource types to Kubernetes.

Suppose you are building a platform that manages blockchain nodes. You want your users to write:

```yaml
apiVersion: platform.myorg.io/v1
kind: BlockchainApp
metadata:
  name: my-node
spec:
  network: ethereum
  version: "1.13"
  storage: 500Gi
```

For this to work, Kubernetes needs to know what a `BlockchainApp` is — what fields it accepts, how it is stored, how it should be validated. That is what a CRD does:

!!! tip "What a CRD tells Kubernetes"
    "There is a new kind called `BlockchainApp`. Here is its schema. Store it, validate it, serve it over the API like any other resource."

After you apply the CRD, `kubectl get blockchainapps` works. The object is stored. Kubernetes treats it as a first-class resource.

What Kubernetes does **not** do is act on it. It stores the object and waits. Something else has to watch for `BlockchainApp` objects and do the actual work. That is an **operator**.

---

## What an operator is

An operator is a program that:

1. Watches a CRD for new, changed, or deleted objects
2. Reads the declared desired state
3. Creates or modifies Kubernetes resources to make that state real
4. Continuously checks that the desired state is maintained

For the `BlockchainApp` example, the operator would create a `StatefulSet`, a `Service`, and a `PersistentVolumeClaim` when a `BlockchainApp` appears — update them when the spec changes — clean them up when the object is deleted.

The loop it runs — watch, compare, act — is the **reconcile loop**.

```text
Desired state (what the CR says)
        │
        ▼
Actual state (what exists in the cluster)
        │
  Are they the same?
        │
    No ─┤─ Yes → do nothing
        │
        ▼
  Make them the same
        │
        └─── repeat on next change or resync
```

This loop is level-triggered, not edge-triggered. The operator does not track what it did last time — it looks at the current desired and actual state and makes them match. An interrupted reconcile is safe to retry.

---

## What writing an operator requires

To write an operator from scratch, you need to:

- Register your Go types with the Kubernetes API scheme
- Set up an informer that watches the API server for changes
- Create a workqueue that buffers and deduplicates rapid changes
- Write a reconcile function that handles create, update, and delete
- Add a worker pool for concurrent processing
- Manage finalizers to prevent dirty deletion
- Handle retries with exponential backoff
- Emit Kubernetes events so users can see what happened
- Expose Prometheus metrics
- Set up leader election
- Write a Dockerfile, build a binary, push an image
- Write a Helm chart or deployment manifests

All of this is infrastructure. Your business logic lives in exactly one place — the `Reconcile()` function. The community answer to this is controller-runtime (the library) or Kubebuilder / Operator SDK (the scaffolding tools). They reduce the scaffolding — you still write Go, manage a project layout, maintain generated code, and own the reconcile loop.

---

## Where Orkestra fits

The above is what writing one operator requires. The next CRD you want to manage means going through the same list again.

Orkestra makes a separation: the scaffolding is infrastructure, and `Reconcile()` is business logic. Orkestra handles the infrastructure. You declare the behaviour of your CRD in a Katalog. When you need a second operator, you add a new CRD entry to the same Katalog and declare its behaviour there.

For the declaration, Orkestra gives you six options:

| Option | What you write |
|--------|---------------|
| Declarative | Nothing — pure YAML, no binary |
| [Remote](../concepts/reconciler-model/09-remote-reconciler.md) | A HTTP endpoint |
| [Hybrid](../concepts/typed-operators/01-hooks.md) | The 10% that templates cannot express |
| [Hooks](../concepts/typed-operators/01-hooks.md) | TheGo functions at specific points in the reconcile cycle |
| [Constructor](../concepts/typed-operators/05-migration.md) | Your own `Reconcile` method; Orkestra's runtime as the host |
| [Constructor + Orkestra resources](../concepts/typed-operators/02-constructor.md) | Your reconciler with Orkestra's resource helpers instead of raw client calls |

```yaml
# katalog.yaml
apiVersion: orkestra.orkspace.io/v1
kind: Katalog
metadata:
  name: blockchain-operator
spec:
  crds:
    blockchainapp:
      crdFile: blockchainapp-crd.yaml
      operatorBox:
        reconcile:
          workers: 3
          resync: 30s
          onCreate:
            deployments:
              - name: "{{ .metadata.name }}"
                image: "{{ .spec.image }}"
                replicas: "{{ .spec.replicas }}"
                port: "{{ .spec.port }}"
                reconcile: true
            services:
              - name: "{{ .metadata.name }}-svc"
                port: "5432"
                targetPort: "{{ .spec.port }}"
                reconcile: true
        emit:
          status:
            fields:
              - path: phase
                value: "Ready"
              - path: endpoint
                value: "{{ .metadata.name }}.{{ .metadata.namespace }}.svc.cluster.local"
```

`ork run` starts the operator locally against a real cluster.

---

## Further reading

- [Kubernetes Basics](https://kubernetes.io/docs/tutorials/kubernetes-basics/) — hands-on introduction to Kubernetes
- [Learning to Orkestrate](./01-learning-to-orkestrate/index.md) — the map of all runnable examples
- [Writing your first Katalog](./03-writing-your-first-katalog.md) — go from nothing to a running operator in one file
- [Migration Guide](../guides/migration/index.md) — if you have an existing controller-runtime operator
