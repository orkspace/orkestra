# Hard Ordered Deletion

`ordered: true` in the `onDelete:` block makes resource groups execute sequentially. Each group must be fully deleted — every resource confirmed gone from the cluster — before the next group starts. The finalizer is not removed until the last group clears.

---

## YAML

```yaml
operatorBox:
  reconcile:
    onDelete:
      ordered: true
      timeout: 10m       # optional — default 5m for the entire sequence
      groups:
        - name: drain    # optional — appears in logs and events
          jobs:
            - name: "{{ .metadata.name }}-drain"
              image: "{{ .spec.image }}"
              command: ["./drain.sh"]

        - name: workloads
          deployments:
            - name: "{{ .metadata.name }}"
          services:
            - name: "{{ .metadata.name }}-svc"

        - name: credentials
          secrets:
            - name: "{{ .metadata.name }}-credentials"
          configMaps:
            - name: "{{ .metadata.name }}-config"
  crdFile: my-operator-crd.yaml
```

Multiple resource types within one group are deleted concurrently — only the boundary between groups enforces ordering.

---

## How it works

For each group in sequence:

1. Delete all resources in the group concurrently (foreground deletion)
2. Poll the API server every 3 seconds until all resources return 404
3. Move to the next group

Foreground deletion (`DeletePropagationForeground`) marks the object for deletion and Kubernetes begins garbage collection. The poll confirms true absence by querying the authoritative API server — not just that the deletion was accepted.

---

## Timeout

The `timeout:` field applies to the entire ordered deletion sequence, not per group. When the timeout is exceeded, the deletion stops and the finalizer is **not** removed. The CR stays in a terminating state. The error is logged and emitted as a Warning event on the CR.

Default: `5m`. Set it high enough for your longest-running cleanup Job.

A CR stuck in terminating because a cleanup Job timed out is safer than a CR whose finalizer was removed while cleanup was incomplete. Fix the underlying issue — the cleanup Job failure, the network problem, the external API timeout — and the deletion will complete on the next attempt.

---

## What ordered deletion does not do

**It does not retry failed deletions.** If a resource cannot be deleted — RBAC issue, API server error, finalizer on the child resource itself — the error is logged and the group waits for the timeout.

**It does not handle external state.** External API calls must be wrapped in a Job, which ordered deletion waits for.

**It does not guarantee child resources are fully reconciled before deletion.** If group 0 contains a Job that was just created and has not yet pulled its image, the Job is deleted in its not-yet-started state. Design cleanup Jobs to be idempotent.

---

## Without `ordered`

Without `ordered: true`, the `onDelete:` block submits all resource deletions concurrently. Jobs in the `onDelete:` block are an exception — Orkestra creates them without an owner reference and waits for each to reach `Complete` or `Failed` before removing the finalizer. All other resources are removed via owner references and Kubernetes garbage collection.

```yaml
onDelete:
  jobs:
    - name: "{{ .metadata.name }}-cleanup"
  deployments:
    - name: "{{ .metadata.name }}"
```
