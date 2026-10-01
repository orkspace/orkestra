# Basic Reconciliation

This guide walks through exactly what happens when Orkestra reconciles a CR — from the moment you apply it to the moment it's deleted.

---

## The Setup

We'll use the `hello-website` example from `ork init`. It has:

- `crd.yaml` — the `Website` CRD
- `katalog.yaml` — the Orkestra operator definition
- `cr.yaml` — a sample `Website` CR

Start the runtime:

```bash
ork run
```

---

## Step 1 — Orkestra starts

When `ork run` starts:

1. Reads the Katalog, finds `crdFile: ./crd.yaml`
2. Applies the CRD to the cluster
3. Creates an informer watching for `Website` CRs
4. Starts a worker pool (3 workers by default)
5. Opens health server at `localhost:8080`

You will see:

```text
INFO  CRD applied                crd=websites.demo.orkestra.io
INFO  CR applied                 name=hello-website namespace=default
INFO  Informer synced            crd=website
INFO  Workers started            crd=website  workers=3
INFO  Health server ready        addr=:8080
```

---

## Step 2 — The CR:

```yaml
apiVersion: demo.orkestra.io/v1alpha1
kind: Website
metadata:
  name: hello-website
  namespace: default
spec:
  image: nginx:1.25
```

---

## Step 3 — Reconcile happens

The moment you apply the CR:

1. The Kubernetes API server notifies Orkestra's informer
2. The CR is enqueued in the `website` workqueue
3. A worker picks it up and begins reconciling
4. `{{ .spec.image }}` resolves to `nginx:1.25`
5. The Deployment is created with owner references pointing to the CR
6. Status is updated, a `Reconciled` event is emitted on the CR
7. Prometheus counter increments: `controller_reconcile_total{result="success"}`

Check the result:

```bash
kubectl get deployments
kubectl describe website hello-website
```

---

## Step 4 — Update the CR

Edit the image:

```bash
kubectl patch website hello-website --type=merge -p '{"spec":{"image":"nginx:1.27"}}'
```

Orkestra detects the change, re-reconciles, and updates the Deployment image. Because `reconcile: true` is set, if the Deployment is edited manually between reconciles, Orkestra corrects it back.

---

## Step 5 — Delete the CR

```bash
kubectl delete -f cr.yaml
```

Orkestra:

1. Receives the delete event
2. Removes the Deployment (via owner references)
3. Removes its finalizer from the CR
4. The CR is fully deleted

---

## Observing Reconciliation

The health endpoint shows everything:

```bash
curl localhost:8080/katalog/website/health | jq
```

```json
{
  "status": "healthy",
  "lastReconcile": "2026-05-16T18:00:01Z",
  "reconcileCount": 3,
  "errorCount": 0,
  "workerCount": 3
}
```

Full detail including queue depth and active workers:

```bash
curl localhost:8080/katalog/website | jq
```

Or open the Control Center for a visual view:

```bash
ork control
# → localhost:8081
```

---

## Reconcile Lifecycle Summary

| Event | Orkestra Action |
|-------|----------------|
| CR created | Templates resolved, resources created, finalizer added |
| CR updated | Resources updated to match new CR spec |
| Resource drifts (if `reconcile: true`) | Resource corrected on next reconcile cycle |
| CR deleted | Resources removed, finalizer released |

---

## Further reading

- **[Writing Your First Katalog](./03-writing-your-first-katalog.md)** — put the reconcile model into practice with a real operator
