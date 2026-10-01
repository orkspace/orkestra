# Namespace protection

Namespace protection lets you declare which namespaces a CR can — or cannot — be applied to. Orkestra enforces this at two independent points.

---

## Two enforcement points

### Runtime (informer) — always active

When you declare `allowedNamespaces` or `restrictedNamespaces` on a CRD entry, the Orkestra runtime filters out events from any namespace that violates the rule. This happens before the event enters the reconcile queue — the CR may exist in etcd, but the runtime never processes it and never creates child resources for it.

This enforcement requires no extra configuration. It is always active whenever namespace rules are present on a CRD.

```yaml
spec:
  crds:
    app:
      operatorBox:
        runtime:
          allowedNamespaces:
            - production
```

A CR applied to `staging` is stored in etcd but silently dropped at the informer. No child resources are created. No error is surfaced to the user unless the webhook is also enabled.

### Admission (webhook) — optional, requires Gateway

When `security.namespaceProtection.enabled: true` is set and the Gateway is running, a `ValidatingWebhookConfiguration` is registered. The API server calls it on every CREATE and UPDATE. CRs that violate namespace rules are rejected **before etcd storage** — the client receives a 403 immediately.

```yaml
security:
  namespaceProtection:
    enabled: true
    failurePolicy: Fail
    cleanupOnShutdown: false
```

This is the recommended mode for production. Without it, a mis-namespaced CR is silently inactive — which is safe but produces no visible error for the user writing the CR. `ork validate` also catches namespace rule mismatches before any cluster interaction.

---

## Rule types

### `allowedNamespaces` — whitelist

Only the listed namespaces are accepted. Any other namespace is rejected (or silently dropped at runtime).

```yaml
crds:
  app:
    operatorBox:
      runtime:
        allowedNamespaces:
          - production
```

Webhook error when the admission path is active:

```text
Error from server: admission webhook "namespace-protection.orkestra.orkspace.io" denied the request:
[App "my-app"] namespace "staging" is not in the allowed list: [production]
```

### `restrictedNamespaces` — blacklist

The listed namespaces are rejected. Any other namespace is accepted.

```yaml
crds:
  cache:
    operatorBox:
      runtime:
        restrictedNamespaces:
          - kube-system
          - kube-public
```

Applying a Cache CR to `kube-system`:

```text
Error from server: admission webhook "namespace-protection.orkestra.orkspace.io" denied the request:
[Cache "my-cache"] namespace "kube-system" is restricted
```

Applying the same Cache CR to `default` or any other non-system namespace succeeds.

---

## Per-CRD rules

Each CRD in your Katalog carries its own namespace rules independently:

```yaml
spec:
  crds:
    app:
      operatorBox:
        runtime:
          allowedNamespaces:
            - production       # App: only production
    database:
      operatorBox:
        runtime:
          allowedNamespaces:
            - production       # Database: only production
    cache:
      operatorBox:
        runtime:
          restrictedNamespaces:
            - kube-system      # Cache: anywhere except system namespaces
            - kube-public
```

---

## `failurePolicy`

Controls what the API server does when the Orkestra webhook is **unreachable**:

| Value | Behaviour |
|-------|-----------|
| `Fail` (default) | API server rejects CREATE/UPDATE — protection holds even when Orkestra is temporarily down |
| `Ignore` | API server allows the request through — protection is bypassed when webhook is unreachable |

`Fail` is the right choice for production. `Ignore` may be appropriate during operator upgrades or in development environments where availability matters more than hard enforcement.

---

## `cleanupOnShutdown`

```yaml
security:
  namespaceProtection:
    cleanupOnShutdown: true
```

When the operator shuts down gracefully (e.g., `helm uninstall`), it removes the `ValidatingWebhookConfiguration` it created. Default is `false` — the webhook configuration persists after the operator stops, which continues to protect the cluster even during a brief downtime window.

---

## Webhook self-healing

If the `ValidatingWebhookConfiguration` is deleted manually, Orkestra recreates it immediately via a Watch-based controller. Deleting the webhook configuration is not a way to bypass namespace protection.

---

## Working example

For a complete scenario including allowed, restricted, and blocked CR examples:

```bash
ork init --pack security
cd namespace-protection
ork e2e
```

---

## Further reading

- **[Admission Control](./03-admission.md)** — deny and warn rules at admission time
- **[RBAC](./02-rbac.md)** — generating and scoping ClusterRoles
- **[Deletion Protection](./04-deletion-protection.md)** — preventing accidental CR and CRD deletion
