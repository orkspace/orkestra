# Runtime query

The gateway needs to make decisions at admission time about state that only the runtime knows: is this value unique among existing CRs? Is the operator currently healthy? What is the current queue depth?

This state has no Kubernetes representation — it is not stored in etcd, it is not in the CR's spec or status, and it cannot be retrieved by listing Kubernetes objects. It lives in the runtime's informer cache and worker metrics.

---

## The problem: runtime-native state at admission time

When Orkestra operated as a single binary, this was straightforward — admission and the runtime shared memory.

After the split into runtime + gateway, Kubernetes became the communication channel between them. Kubernetes works well for state that belongs in Kubernetes: object specs, statuses, conditions, labels. But some state belongs to the runtime's own domain and was never meant to be a Kubernetes concern.

The first approach was to write health and metrics as annotations onto the CR. This worked for preReconcile gates — annotations are visible in the resolver context, so `reconcileGate.when:` conditions could read them as `.health.*` and `.metrics.*` template fields. But annotations are written by the runtime after reconcile, not before — they are not available at admission time when the object doesn't exist in etcd yet.

---

## The solution: a direct query interface

The gateway queries the runtime directly over HTTP, using the runtime's own live API. Three pieces of state are available:

**Uniqueness** — whether another CR already holds a given field value, queried from the runtime's informer cache. Best-effort: two concurrent admissions can both pass; the runtime's reconciler performs an authoritative live check and resolves the conflict if both succeed.

Used by the `unique` validation operator:

```yaml
validation:
  rules:
    - field: spec.domain
      operator: unique
      message: "domain must be unique across all App CRs"
      action: deny
```

**Health** — the operator's current health summary, queried live from the runtime's health endpoint. Available as `.health.*` in validation and mutation rule conditions.

**Metrics** — the operator's current queue and reconcile metrics, queried live from the runtime. Available as `.metrics.*` in validation and mutation rule conditions.

Health and metrics are live operational stats — not annotation reads, not cached values.

---

## Why this is not ONCOP

ONCOP (`cross:`) is operator-to-operator observation for use inside the reconcile pipeline. An operator reads another operator's CR data to enrich its own templates.

The runtime query is component-to-component: the **gateway** reads the **runtime's** own internal state at **admission time**, carrying information that has no Kubernetes encoding. It is not about CRs reading other CRs — it is about Orkestra components communicating state that is native to themselves.

---

## Usage in gate conditions

Once the query client is wired into the resolver, its results are available in gate conditions exactly like annotation-based data:

```yaml
validation:
  rules:
    - field: "{{ .health.status }}"
      equals: healthy
      message: "operator must be healthy before new CRs are accepted"
      action: deny

    - field: "{{ .metrics.queueDepth }}"
      operator: lte
      value: "80"
      message: "queue is at capacity"
      action: deny

    - field: spec.domain
      operator: unique
      action: deny
```

→ [Admission](01-admission.md)  
→ [query package](../../reference/cli/index.md)
