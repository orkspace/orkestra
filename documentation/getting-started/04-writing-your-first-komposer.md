# Writing Your First Komposer

A **Komposer** defines where Orkestra loads Katalogs from. A Katalog defines what your operator does — a Komposer defines where those Katalogs come from.

You do not need a Komposer to use Orkestra. `ork run` works without one. Komposers become useful when you want to compose Katalogs from multiple sources or apply overrides.

---

## The Simplest Komposer

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: Komposer
metadata:
  name: my-komposer

imports:
  files:
    - ./katalog.yaml
```

Run it the same way:

```bash
ork run
# Orkestra reads komposer.yaml from the current directory and starts the runtime.
```

---

## Loading Multiple Katalogs

```yaml
imports:
  files:
    - ./katalogs/app.yaml
    - ./katalogs/database.yaml
    - ./katalogs/cache.yaml
```

Orkestra merges them. If two Katalogs define the same CRD name, the later one wins — or you can use the `spec.crds` block in the Komposer itself to override specific fields.

---

## Loading From a Registry

```yaml
imports:
  registry:
    - url: ghcr.io/orkspace/orkestra-registry/postgres@v14
      oci: true
```

Pulls the Postgres operator pattern from the OCI registry and merges it with any other sources.

---

## Loading From a Helm Chart

```yaml
imports:
  helm:
    - repo: https://charts.myorg.io
      chart: platform-crds
      version: 2.1.0
```

---

## Overriding Fields

The `spec.crds` block in a Komposer always wins over imported Katalogs. Use it to tune imported patterns:

```yaml
imports:
  registry:
    - url: ghcr.io/orkspace/orkestra-registry/postgres@v14
      oci: true

spec:
  crds:
    postgres:
      operatorBox:
        reconcile:
          workers: 8
          resync: 30s
```

---

## Validating Without Running

```bash
ork validate -f komposer.yaml
```

Resolves all sources, merges everything, and reports errors — without touching the cluster.

---

## Further reading

- **[Learning to Orkestrate](./01-learning-to-orkestrate/index.md)** — progression through the full example library
- **[Komposers](../orkestra-registry/03-komposers.md)** — publishing and pulling Komposers from the registry
