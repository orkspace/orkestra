# Writing Your First Katalog

A **Katalog** is a YAML file that tells Orkestra what to do when a Custom Resource is created, updated, or deleted. This guide builds one from scratch.

---

## The Minimal Katalog

Create `katalog.yaml`:

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: Katalog
metadata:
  name: my-first-katalog

spec:
  crds:
    myapp:
      crdFile: ./crd.yaml
      crFiles:
        - ./cr.yaml
      operatorBox:
```

This tells Orkestra: read the CRD from `crd.yaml`, apply it to the cluster, and watch for `MyApp` CRs. No resources are created yet.

`crdFile` is how you declare a CRD. Orkestra reads `group`, `version`, `kind`, and `plural` directly from the file and applies it to the cluster when `ork run` starts. No separate `kubectl apply -f crd.yaml` needed.

`crFiles` is how you declare a CR. Orkestra applies it at startup. No separate `kubectl apply -f cr.yaml` needed.


---

## The CRD

Create `crd.yaml`:

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: myapps.demo.myorg.io
spec:
  group: demo.myorg.io
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
              required: [image]
              properties:
                image:
                  type: string
                replicas:
                  type: integer
                  default: 1
                port:
                  type: integer
                  default: 80
  scope: Namespaced
  names:
    plural: myapps
    singular: myapp
    kind: MyApp
```

---

## Adding a Deployment

```yaml
spec:
  crds:
    myapp:
      crdFile: ./crd.yaml
      crFiles:
        - ./cr.yaml
      operatorBox:
        reconcile:
          onCreate:
            deployments:
              - name: "{{ .metadata.name }}"
                image: "{{ .spec.image }}"
                replicas: "{{ .spec.replicas }}"
                port: "{{ .spec.port }}"
                reconcile: true
```

Values inside `{{ }}` are Go templates evaluated against the live CR. `reconcile: true` means the Deployment is drift-corrected on every reconcile — if someone edits it manually, Orkestra corrects it back.

---

## Adding a Service

```yaml
onCreate:
  deployments:
    - name: "{{ .metadata.name }}"
      image: "{{ .spec.image }}"
      replicas: "{{ .spec.replicas }}"
      port: "{{ .spec.port }}"
      reconcile: true
  services:
    - name: "{{ .metadata.name }}-svc"
      port: "80"
      targetPort: "{{ .spec.port }}"
      reconcile: true
```

---

## Conditional Resources

Use `when:` to create a resource only when a condition is met:

```yaml
services:
  - name: "{{ .metadata.name }}-public"
    port: "80"
    targetPort: "{{ .spec.port }}"
    when:
      - field: spec.exposePublicly
        equals: "true"
```

---

## Status Fields

Write values back to the CR after every reconcile:

```yaml
operatorBox:
  reconcile:
    onCreate:
      deployments:
        - name: "{{ .metadata.name }}"
          image: "{{ .spec.image }}"
          replicas: "{{ .spec.replicas }}"
          reconcile: true
  emit:
    status:
      fields:
        - path: phase
          value: "Running"
        - path: observedReplicas
          value: "{{ .spec.replicas }}"
```

The CRD must declare `subresources: status: {}` for status writes to work.

---

## Dependencies Between CRDs

If your Katalog manages multiple CRDs and one must reconcile before the other:

```yaml
spec:
  crds:
    database:
      crdFile: ./database-crd.yaml
    application:
      crdFile: ./application-crd.yaml
      dependsOn:
        database:
          condition: healthy   # wait until database workers are running and failure-free
    analytics:
      crdFile: ./analytics-crd.yaml
      dependsOn:
        database:
          condition: started   # wait only until database workers are running
```

`condition: healthy` waits until the dependency's workers are running and its consecutive failure count is zero — use this when the downstream CRD needs the upstream to be stable before it starts. `condition: started` waits only until workers are running — useful when you want ordering without blocking on health (for example, when the dependency might legitimately fail during startup and you want the downstream to proceed anyway).

---

## Running It

```bash
ork run
# Orkestra reads katalog.yaml from the current directory and starts the runtime.
```

Verify it works:

```bash
kubectl get deployments
kubectl get services
```

---

## Validate Without Running

```bash
ork validate
# Orkestra reads katalog.yaml from the current directory and reports every error without touching the cluster.
```

---

## Further reading

- **[Writing Your First Komposer](./04-writing-your-first-komposer.md)** — compose multiple Katalogs into a platform declaration
- **[Learning to Orkestrate](./01-learning-to-orkestrate/index.md)** — full progression through the example packs
