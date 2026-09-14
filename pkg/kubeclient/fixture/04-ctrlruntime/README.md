# controller-runtime style constructor — WebApp

The reconciler is written in pure controller-runtime style. It holds a
`client.Client`, receives a `reconcile.Request`, and calls `client.Get` /
`client.Create` / `client.Patch` exactly as it would in a kubebuilder project.

Two adapter calls at the constructor boundary wire it into Orkestra:

```go
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
    return domain.ReconcilerFrom(&WebAppReconciler{
        Client: orkadapter.ToClient(kube),
    })
}
```

- `orkadapter.ToClient(kube)` — wraps Orkestra's `kubeclient.Interface` as a
  `sigs.k8s.io/controller-runtime/pkg/client.Client`
- `domain.ReconcilerFrom(r)` — wraps the `ctrl.Request` reconciler signature
  as a `domain.Reconciler` so Orkestra's operatorBox can call it

The `Reconcile` method is completely untouched. No signature change, no return
type change, no call-site rewrites. Bring an existing controller-runtime
reconciler, write two lines in a constructor, and it runs in Orkestra.

**Requirement:** `ork` CLI — install from [orkestra-install](https://github.com/orkspace/orkestra#getting-started)

---

## Step 1 — Generate the registry

```bash
make registry
```

## Step 2 — Build

```bash
make clean && make build
ork validate katalog.yaml
ork simulate
```

## Step 3 — Run

```bash
ork run

kubectl get deployment 04-ctrlruntime-demo -o wide
kubectl get webapp 04-ctrlruntime-demo -o jsonpath='{.status.phase}' && echo
```

## E2E

```bash
make docker push IMAGE_REPO=yourregistry/webapp-operator IMAGE_TAG=latest

ork e2e \
  --set runtime.image.repository=yourregistry/webapp-operator \
  --set runtime.image.tag=latest
```

## Cleanup

```bash
./cleanup.sh
```
