# Deletion protection

Deletion protection prevents CRDSs, their CRs — and Orkestra's own infrastructure — from being accidentally (or maliciously) deleted. It works by attaching a label to every protected resource and registering a validating webhook that intercepts DELETE requests on those resources.

```yaml
security:
  operatorBox:
    runtime:
      deletionProtection:
        enabled: true
        cleanupOnShutdown: true
        failurePolicy: Fail
```

---

## How it works

When deletion protection is enabled, Orkestra does two things:

1. **Labels every managed CR** with `orkestra.io/deletion-protection: "true"`. In runtime mode, the reconciler applies and maintains this label automatically. In gateway-only mode, you apply it yourself.

2. **Registers a `ValidatingWebhookConfiguration`** that intercepts every DELETE request. If the target resource carries the protection label, the request is denied before it reaches etcd.

```bash
kubectl delete app my-app
# Error from server: admission webhook "delete-protection.orkestra.orkspace.io" denied the request:
# [App "my-app"] deletion is blocked by Orkestra deletion protection.
```

### Orkestra's own infrastructure is protected too

Every resource the Helm chart creates — the `Deployment`, `Service`, `ServiceAccount`, `ClusterRoleBinding`, TLS `Secret`, and webhook configurations — carries the protection label. The webhook protects them the same way it protects your CRs. You cannot accidentally `kubectl delete` the runtime, the gateway or the control center out of the cluster while deletion protection is active.

---

## Strict mode

By default, deletion protection blocks DELETE operations but the protection label itself can be removed with `kubectl label`. Removing the label silently unprotects the resource.

Strict mode closes this gap:

```yaml
security:
  operatorBox:
    runtime:
      deletionProtection:
        enabled: true
        strictMode: true
```

With strict mode on, Orkestra registers a second webhook (`strict-mode.orkestra.orkspace.io`) that intercepts UPDATE operations on any labeled resource and blocks any request that removes the `orkestra.io/deletion-protection` label:

```text
Error from server: admission webhook "strict-mode.orkestra.orkspace.io" denied the request:
[Orkestra Security] The App "my-app" in namespace "default" carries the deletion-protection label.
Removing this label is blocked because strictMode is enabled.

To unprotect this resource:
- Opt out in the katalog using: '<crd>.deletionProtection.strictMode: false'
```

**To unprotect a resource under strict mode**, change the Katalog — not the label. This requires the same access level as operating Orkestra itself. There is no label-level escape hatch.

---

## Per-CRD overrides

The global `deletionProtection` setting applies to every CRD in your Katalog. You can override three settings per CRD:

```yaml
spec:
  crds:
    app:
      operatorBox:
        runtime:
          deletionProtection:
            protectCRD: true      # protect the CRD object itself (default: true)
            protectCRs: false     # protect instances of this CRD (default: true)
            strictMode: false     # override the global strictMode for this CRD (default: inherits global)
```

### `protectCRs: false` — instances can be deleted

When `protectCRs: false`, the reconciler does not add the deletion-protection label to instances of this CRD. Existing instances that already carry the label will have it removed on the next reconcile cycle.

Use this when you want to register a CRD with Orkestra (for reconciliation, status management, etc.) but not prevent users from deleting instances.

### `protectCRD: false` — the CRD resource itself can be deleted

When `protectCRD: false`, Orkestra does not protect the CRD object (`apps.security.orkestra.io`) itself. The CRD can be deleted from the cluster. Note that if the CRD is deleted, all instances are garbage-collected by Kubernetes.

### `strictMode: false` — this CRD's instances can have the label removed

When `strictMode: false` on a CRD (even while global `strictMode: true`), the strict-mode webhook allows the deletion-protection label to be removed from instances of this CRD. Orkestra signals this by adding the exemption label `orkestra.io/strict-mode-exempt: "true"` to every instance.

The webhook checks for this exemption label before enforcing strict mode:
- Exempt label present → label removal is allowed
- Exempt label absent → label removal is blocked

---

## The three protection profiles

These per-CRD settings produce four distinct combinations. The examples below use global `strictMode: true`.

### Fully protected (default)

```yaml
deletionProtection:
  # all defaults
```

- Instance has: `deletion-protection=true`
- No exemption label
- Cannot be deleted. Cannot have label removed. Fully locked.

### Protected, strict mode exempt

```yaml
deletionProtection:
  strictMode: false   # this CRD opts out of strict mode
```

- Instance has: `deletion-protection=true`, `strict-mode-exempt=true`
- Cannot be deleted via `kubectl delete`
- The deletion-protection label CAN be removed (`kubectl label`) because the exemption label tells the webhook to allow it
- Use this for resources that need protection but where an administrator may need to override it manually

### Unprotected instances, protected CRD

```yaml
deletionProtection:
  protectCRs: false   # instances are not protected
```

- Instances have: no deletion-protection label
- Instances can be deleted freely
- The CRD object itself is still protected
- `ork run` will remove the deletion-protection label from existing instances on its first reconcile cycle

### Warning: `protectCRD: false` + `protectCRs: true`

This combination is semantically inconsistent: you are asking Orkestra to protect instances of a CRD that can itself be deleted. Orkestra will log a validation warning at startup but will still honour the settings as declared.

---

## How the reconciler removes the label (two-phase)

When a CRD moves from `protectCRs: true` to `protectCRs: false`, the reconciler must remove the deletion-protection label from existing instances. This is non-trivial under strict mode because the strict-mode webhook blocks any UPDATE that removes the deletion-protection label — unless the resource also carries the exemption label in the same update.

Orkestra handles this automatically in two phases:

**Phase 1 (first reconcile after the config change)**

The reconciler removes `deletion-protection` and ensures `strict-mode-exempt: true` is present in the same patch. The webhook sees the exemption label in the new object and allows the update.

**Phase 2 (next reconcile)**

`deletion-protection` is now gone. The webhook's objectSelector no longer matches the resource, so the webhook is not called. The reconciler removes the `strict-mode-exempt` label freely.

You do not need to manage this transition manually. Change the Katalog; the reconciler handles the rest over two cycles.

---

## Gateway-only mode

In gateway-only mode (no `ork run`), there is no reconciler to manage labels. The webhook still enforces the protection contract — but **you are responsible for applying the labels yourself**:

```bash
# Protect a resource manually
kubectl label app my-app orkestra.io/deletion-protection=true

# Remove protection (only possible if strict mode is off or the resource has the exemption label)
kubectl label app my-app orkestra.io/deletion-protection-
```

The webhook does not care how the label got there. It only checks whether the label is present at the time of the DELETE request.

For drift correction (reconciling label state back to what the Katalog declares), you need the runtime. In gateway-only mode, label state is whatever you make it.

---

## Scenarios and edge cases

### "I want to delete a protected resource right now"

1. If strict mode is **off**: `kubectl label <kind> <name> orkestra.io/deletion-protection-` removes the label, then `kubectl delete` succeeds.
2. If strict mode is **on**: set `strictMode: false` for the CRD in your Katalog, apply the change, wait for the reconciler to add the exemption label, then `kubectl label` to remove the protection label, then `kubectl delete`.
3. If you need it **immediately**: set `deletionProtection.enabled: false` globally, restart Orkestra Gateway (the webhook is deregistered), then delete.

### "The operator was deleted and now nothing enforces protection"

If deletion protection itself is deleted while protection is active, the webhook is gone and CRs can be deleted. This is why Orkestra self-heals its own webhook configurations — see the [self-healing section](03-admission.md#webhook-self-healing). And why the Helm chart protects the operator's own Deployment with the same label.

### "I manually added the exemption label to a strictly protected resource"

The reconciler will remove it on the next reconcile cycle. The reconciler's job is to enforce the label state declared in the Katalog. Manual drift is corrected automatically in runtime mode.

### "A new resource was created after I disabled protectCRs"

The reconciler will not add the deletion-protection label to it. The label is only applied to resources that Orkestra reconciles while `protectCRs: true`.

---

## Working example

For a complete scenario including per-CRD overrides and the standard deletion-protection flow, run:

```bash
ork init --pack security
cd deletion-protection
```

Then:

```bash
ork validate        # inspect the security posture of each CRD
ork run --dev       # run Orkestra locally (--dev creates a kind cluster if needed)
ork e2e             # declarative end-to-end testing
```

Follow the steps in the README