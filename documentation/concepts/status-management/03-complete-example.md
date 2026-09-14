# Complete Example

A `Website` CRD with all three status layers declared.

## CRD definition

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: websites.demo.orkestra.io
spec:
  group: demo.orkestra.io
  versions:
    - name: v1alpha1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                image:
                  type: string
                replicas:
                  type: integer
            status:
              type: object
              x-kubernetes-preserve-unknown-fields: true
  names:
    kind: Website
    plural: websites
    singular: website
  scope: Namespaced
```

## Katalog

```yaml
- name: website
  crdFile: website-crd.yaml

  operatorBox:

    status:
      fields:
        # Layer 2 — from the CR spec
        - path: phase
          value: "Running"
        - path: observedReplicas
          value: "{{ .spec.replicas }}"
        - path: endpoint
          value: "{{ .metadata.name }}.{{ .metadata.namespace }}.svc.cluster.local"

        # Layer 3 — from child resource status
        - path: readyReplicas
          value: "{{ get .children.deployment \"status\" \"readyReplicas\" }}"
        - path: availableReplicas
          value: "{{ get .children.deployment \"status\" \"availableReplicas\" }}"

    onCreate:
      deployments:
        - image: "{{ .spec.image }}"
          replicas: "{{ .spec.replicas }}"
          reconcile: true
      services:
        - port: "80"
          targetPort: "80"
          reconcile: true
```

After a successful reconcile with two ready replicas:

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
  phase: Running
  observedReplicas: "2"
  endpoint: my-site.orkestra.svc.cluster.local
  readyReplicas: "2"
  availableReplicas: "2"
```

---

## Status in hooks

Go hooks have full access to the CR object and the Kubernetes client. Write status directly from a hook:

```go
func (r *websiteReconciler) OnReconcile(ctx context.Context, obj *apiv1.Website) error {
    // ... reconcile logic ...

    obj.Status.Phase = "Running"
    obj.Status.ReadyReplicas = actualReadyCount

    _, err := r.client.Resource(websiteGVR).
        Namespace(obj.Namespace).
        UpdateStatus(ctx, domain.ToUnstructured(obj), metav1.UpdateOptions{})
    return err
}
```

Hooks and declarative status fields are not mutually exclusive. When both are declared, Orkestra applies the declarative fields after the hook completes — the hook's writes are preserved (the patch is merged, not replaced).

---

## Disabling status management

To disable all automatic status management for a CRD:

```yaml
operatorBox:
  status:
    conditions: false
    fields: []
```

With both disabled, Orkestra makes no status patches. The CR's status is entirely managed by Go hooks or left empty.
