# Delete

When a CR receives a deletion timestamp, Orkestra switches to the delete path.

---

## What happens

1. **Deletion timestamp detected** — the CR is in the cache with `.metadata.deletionTimestamp` set
2. **`onDelete:` runs** — hooks and templates in the `onDelete:` block execute. If `ordered: true`, groups run sequentially. See [Ordered Deletion](../ordered-deletion/)
3. **Finalizer removed** — after `onDelete:` completes, Orkestra removes its finalizer from the CR
4. **GC** — namespace-scoped child resources with owner references are garbage-collected by Kubernetes automatically. Cluster-scoped resources (Namespaces, ClusterRoles, ClusterRoleBindings, PVs) cannot cascade through owner references — Orkestra's `CleanupFinalizer` ensures they are explicitly deleted before the CR is removed

---

## Owner references

Every namespace-scoped child resource created by Orkestra's templates gets the parent CR as its owner reference — Kubernetes GC cleans them up automatically when the parent is deleted. Cluster-scoped resources cannot receive a namespace-scoped owner reference, so Orkestra handles their deletion directly.

Declare `onDelete:` only for cleanup Kubernetes GC cannot provide: external infrastructure, Jobs that must complete before deletion, or credentials that must be revoked.

---

## What the finalizer protects

Orkestra always adds a `CleanupFinalizer` (`orkestra.orkspace.io/cleanup`) to every managed CR — regardless of whether `onDelete:` is declared. Without it, Kubernetes would proceed with deletion immediately, tearing down the CR before cluster-scoped resources could be cleaned up or `onDelete:` Jobs could run.

When `onDelete:` is not declared, the finalizer is removed as soon as the cluster-scoped cleanup pass completes. When `onDelete:` is declared, it runs first.
