# Error Behavior

A pipeline step failure is contained, recorded, and retried. It never affects other operatorBoxes in the same process.

---

## Failure recording

When any step fails — validation, template resolution, a resource Create or Update, an external HTTP call — Orkestra:

1. Writes `status.conditions[type=Ready, status=False, reason=ReconcileError, message=<error>]` to the CR.
2. Increments the consecutive-failure counter in the operatorBox's `CRDHealth` instance.
3. Requeues the CR with exponential backoff.

The error message is truncated to 256 characters in the status condition to stay within etcd's annotation limits. The full error appears in the operator logs.

---

## Backoff

Requeue backoff is controlled by the workqueue rate limiter. The default is exponential backoff starting at 5ms and capping at 1000s. The backoff resets when a reconcile succeeds.

A CR that continuously fails (external service unreachable, a validation rule that can never pass) will eventually settle at the maximum backoff interval and stop consuming significant CPU. The `resync:` period restarts the cycle from backoff zero when the CR hasn't been processed for `resync` duration.

---

## Degraded state

After `consecutiveFailures` threshold reconcile failures in a row (default: 5), the operatorBox enters **degraded state**:

- The operatorBox is marked degraded in `CRDHealth`.
- The Control Center surfaces it as unhealthy with the failure count and last error.
- Other CRDs with `dependsOn: <this-crd>: healthy` stop processing new CRs until this operatorBox recovers.

The operatorBox exits degraded state automatically when a reconcile succeeds (consecutive-failure counter resets to zero).

---

## Panic recovery

Every reconcile runs inside `safeReconcile`, which catches panics with a `recover()`. A panicking reconcile:

- Records the panic as a failure (same path as a regular error).
- Does **not** crash the operator process.
- Does **not** affect other operatorBoxes or other CRs being reconciled concurrently.

Panics are surfaced in logs at `ERROR` level with a full stack trace. They increment the consecutive-failure counter and trigger backoff identically to regular errors.

See [Panic Recovery](03-panic-recovery.md) for the full implementation, what you see in logs and metrics, and a live example.

---

## Partial reconcile

The pipeline does not roll back completed steps on failure. If a deployment was created in step N and step N+1 fails (e.g., a service create fails), the deployment remains. The next reconcile will find the deployment exists (and drift-correct it if needed) and retry the service.

This is intentional — Kubernetes resources are idempotent to create. The operator converges toward the desired state across reconcile cycles, not within a single one.
