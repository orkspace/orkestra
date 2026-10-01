# Ordered Deletion

When a CR is deleted, Orkestra runs the `onDelete:` block before removing the finalizer. By default, all resource groups execute concurrently — deletion requests are submitted in parallel and the finalizer is removed immediately.

Orkestra provides two models when the sequence matters.

---

## When you need it

Most operators do not need ordered deletion. Owner references and Kubernetes garbage collection handle cascade deletion correctly for the common case.

Ordered deletion is for the cases where sequence matters:

- A cleanup Job must complete before its target is deleted
- Infrastructure must be deprovisioned before its credentials Secret is removed
- A final backup must complete before the workload is torn down

---

## Two models

| | Hard ordered | Condition-based |
|---|---|---|
| Mechanism | `ordered: true` + `groups:` | `when:` / `or:` on the block or group |
| Finalizer | Held until complete | Held only if Jobs are present |
| CR can get stuck | Yes (on timeout) | No |
| Guarantee | Sequential, enforced | Conditional skip |
| Use case | Safety-critical sequential cleanup | Selective execution based on CR state |

---

## Where to go next

- [Hard Ordered Deletion](01-hard-ordered.md) — `ordered: true`, groups, timeouts
- [Condition-Based Deletion](02-condition-based.md) — `when:` / `or:` sequencing without blocking
