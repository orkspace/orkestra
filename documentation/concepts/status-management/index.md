# Status Management

Orkestra writes CR status automatically. Every managed CR gets a machine-readable health signal after every reconcile. For operators that need richer status — replica counts, endpoints, phase strings, child resource state — a declarative `status:` block in the Katalog provides it without writing a single line of Go.

Status in Orkestra works in three layers, each building on the previous one.

---

## Layer 1 — Standard conditions (automatic)

After every reconcile cycle, Orkestra patches the CR's `/status` subresource with a standard Kubernetes `Ready` condition and `observedGeneration`. No Katalog declaration required.

On success:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: ReconcileSucceeded
      message: ""
      lastTransitionTime: "2026-03-29T09:02:33Z"
      observedGeneration: 1
  observedGeneration: 1
```

On failure:

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      reason: ReconcileError
      message: "deployment: image pull failed: rpc error..."
      lastTransitionTime: "2026-03-29T09:03:01Z"
      observedGeneration: 1
```

This makes `kubectl get` immediately informative. External tools that watch for `Ready=True` — ArgoCD health checks, dashboards, monitoring scripts — work out of the box.

!!! note "CRD must declare the status subresource"
    Layer 1 requires `subresources: status: {}` in the CRD definition. Without it, Orkestra's status patch receives a 404 and is silently ignored — reconciliation is not affected.

    ```yaml
    spec:
      versions:
        - name: v1alpha1
          subresources:
            status: {}
    ```
    To opt out of automatic conditions for a specific CRD:

    ```yaml
    operatorBox:
      emit:
        status:
          conditions: false
    ```

---

## Where to go next

- [Declarative Status Fields](01-declarative-fields.md) — Layer 2: writing spec values and computed strings to status
- [Child Resource Propagation](02-child-propagation.md) — Layer 3: reading live child resource state into status
- [Complete Example](03-complete-example.md) — all three layers together, plus hooks and disabling
- [Transient Fields](04-transient-fields.md) — clearing stale values when a condition clears (`clearOnFalse`)
