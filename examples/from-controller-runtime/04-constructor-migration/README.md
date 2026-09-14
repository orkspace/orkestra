# 04 — Constructor Migration

Your existing `Reconcile` method runs inside Orkestra unchanged. Compare [reconciler/webapp_reconciler.go](reconciler/webapp_reconciler.go) with [00-controller-runtime-baseline/controller/webapp_controller.go](../00-controller-runtime-baseline/controller/webapp_controller.go). The differences are exactly three:

1. **`SetupWithManager` is gone.** Orkestra provides the informer, workqueue, worker pool, leader election, panic recovery, and metrics.
2. **`Scheme` is gone.** Orkestra handles scheme registration at startup.
3. **`NewWebAppReconciler` is added.** Two lines wire the reconciler in:

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

`orkadapter.ToClient` wraps Orkestra's interface as a `client.Client` — the same type your struct already holds. `domain.ReconcilerFrom` adapts the `ctrl.Request` signature. Nothing inside `Reconcile` changes.

---

> **Before you start:** Update [katalog.yaml](katalog.yaml) — set `author:` to your name or org and `metadata.name` to your operator name. Replace `ghcr.io/myorg` throughout this guide with your own image registry and org. Set your katalog registry before pushing:
> ```bash
> export ORK_REGISTRY=ghcr.io/myorg/katalogs
> ```

---

## Step 1 — Generate the type registry

```bash
make registry
```

Generates `pkg/typeregistry/zz_generated_typeregistry.go` from your Katalog. Re-run whenever `apiTypes` changes.

---

## Step 2 — Build

```bash
make build
```

---

## Step 3 — Validate

```bash
make validate
```

---

## Step 4 — Simulate

```bash
ork simulate
```

---

## Step 5 — Run locally

Apply the CRD:

```bash
kubectl apply -f crd.yaml
```

```bash
ork run
```

In another terminal, apply the CR:

```bash
kubectl apply -f cr.yaml
kubectl get webapps
kubectl get deployments
kubectl get services
```

---

## Step 6 — Build and push the production image

```bash
export IMAGE_REPO=ghcr.io/myorg/webapp-constructor
export IMAGE_TAG=1.0.0
make release
```

## Step 7 — Update [values.yaml](values.yaml) with your image

```yaml
runtime:
  image:
    repository: ghcr.io/myorg/webapp-constructor
    tag: 1.0.0
```

## Step 8 — Push the katalog pattern to the registry

```bash
export ORK_REGISTRY=ghcr.io/myorg/katalogs
ork push .
```

---

## Cleanup

```bash
chmod +x cleanup.sh && ./cleanup.sh
```

---

## Next

[05 — Constructor: Orkestra resources](../05-constructor-orkestra-resources/README.md) — replace the manual Get / IsNotFound / Create / Patch with `orkdeploy.Update` and `orksvc.Update`.
