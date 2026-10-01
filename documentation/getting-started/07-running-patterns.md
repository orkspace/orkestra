# Running Patterns from the Registry

The Orkestra registry is a collection of published operator patterns — each one a complete, tested, ready-to-run katalog. You do not need to write any YAML to run a published pattern.

```bash
ork run postgres:v1.0.0 --dev --apply-cr
```

That command pulls the pattern, creates a local cluster, applies the example CRD and CR, and starts the runtime. The postgres operator is running and reconciling within about two minutes on a cold machine.

---

## Before you run — inspect first

`ork inspect` reads the registry manifest without downloading any files. Use it to check quality signals before committing to a pull.

```bash
ork inspect postgres:v1.0.0
```

```text
postgres:v1.0.0
  Registry:    ghcr.io
  Kind:        Katalog
  Digest:      sha256:3b070e8b...
  Pushed:      2026-06-13T05:50:33Z
  Size:        2.5 KB

  Description: A declarative PostgreSQL deployment pattern...
  Simulate:    ✓ Verified · 6 assertions · 862ms · tested 20d ago
  E2E:         ✓ Verified · 5 assertions · 2m21s · tested 20d ago

  Files:
    katalog.yaml   1.5 KB
    crd.yaml       808 B
    cr.yaml        160 B
    e2e.yaml       1.2 KB
    simulate.yaml  723 B
```

`Simulate: ✓ Verified` and `E2E: ✓ Verified` mean the author ran both gates before publishing. You are pulling a pattern that is known to work.

---

## Preview the files before pulling

Read individual files directly from the registry — no download required:

```bash
ork inspect postgres:v1.0.0 --view crd.yaml,cr.yaml
```

This prints the raw file contents from the OCI artifact. Useful for checking what fields the CR expects before applying it.

---

## Pull to cache

```bash
ork pull postgres:v1.0.0
```

Downloads the pattern to `~/.orkestra/registry/.../postgres/v1.0.0/`. Subsequent `ork run`, `ork simulate`, and `ork e2e` calls are served from disk — no repeated network call.

```text
Pulling postgres:v1.0.0
  → oci://ghcr.io/orkspace/orkestra-registry/patterns/katalogs/postgres:v1.0.0
  ✓ Cached at ~/.orkestra/registry/ghcr.io/.../postgres/v1.0.0
```

---

## Run

`ork run` accepts the same short reference. If the pattern is not cached it pulls first.

**Bring your own CR:**

```bash
ork run postgres:v1.0.0 --dev
```

Creates a local kind cluster (if needed), starts the postgres runtime. Apply your own CR when ready:

```bash
kubectl apply -f ~/.orkestra/registry/ghcr.io/.../postgres/v1.0.0/cr.yaml
```

**Full batteries-included:**

```bash
ork run postgres:v1.0.0 --dev --apply-cr
```

Applies `crd.yaml`, waits for the CRD to establish, applies `cr.yaml`, then starts the runtime. The operator has a CR waiting on its first reconcile cycle.

**Run via komposer:**

If the pattern ships a `komposer.yaml` (multi-operator composition), use `--use-komposer`:

```bash
ork run postgres:v1.0.0 --dev --use-komposer
```

---

## Using your own registry

```bash
export ORK_REGISTRY=ghcr.io/myorg/katalogs
ork inspect my-operator:v2.0.0
ork run my-operator:v2.0.0 --dev --apply-cr
```

`ORK_REGISTRY` sets the default registry for all shorthand references. Full OCI refs (`oci://ghcr.io/...`) are used as-is regardless of the env var.

---

## Run the E2E test

Every published pattern includes an `e2e.yaml`. Run it to verify the operator end-to-end against a real cluster:

```bash
ork pull postgres:v1.0.0 -o ./postgres-local
cd ./postgres-local
ork e2e
```

`ork e2e` creates a cluster, installs the operator, applies the CR, runs all expect checkpoints, and cleans up. The same test the author ran before publishing.

---

## Summary

| Command | What it does |
|---------|-------------|
| `ork inspect postgres:v1.0.0` | Browse metadata and quality signals |
| `ork inspect postgres:v1.0.0 --view cr.yaml` | Read a file without downloading |
| `ork pull postgres:v1.0.0` | Cache the pattern locally |
| `ork run postgres:v1.0.0 --dev` | Pull if needed, create cluster, start runtime |
| `ork run postgres:v1.0.0 --dev --apply-cr` | Same, plus apply example CRD and CR |
| `ork run postgres:v1.0.0 --dev --use-komposer` | Run via the pattern's komposer |
| `ork run postgres:v1.0.0 --dev --refresh` | Re-pull before running |

---

## Related

- [`ork inspect`](../reference/cli/11-inspect.md) — full flag reference
- [`ork pull`](../reference/cli/10-pull.md) — full flag reference
- [`ork run`](../reference/cli/07-run.md) — full flag reference
- [Publishing a Pattern](../guides/registry/01-patterns.md)
- [Consuming Patterns](../guides/registry/02-consuming.md)
- [Artifact signing guide](../security/10-artifact-signing.md)
