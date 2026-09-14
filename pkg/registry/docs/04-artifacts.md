# 04 — Artifacts: Patterns and Motifs

Orkestra publishes two kinds of artifacts to OCI registries. Both share the same push/pull/info/list pipeline — the kind is detected automatically from the primary YAML file.

---

## Patterns (kind: Katalog)

### Directory layout

```
my-operator/
  katalog.yaml   ← operator declaration         (required)
  crd.yaml       ← CRD schema                   (required)
  README.md      ← human documentation          (optional)
  cr.yaml        ← example CR for the CRD       (optional)
  e2e.yaml       ← E2E test definition          (optional)
  simulate.yaml  ← simulate gate spec           (optional)
```

Typed operators (those declaring `operatorBox.hooks` or `operatorBox.constructor`)
additionally include the Go build files so a consumer can rebuild the custom runtime image:

```
my-typed-operator/
  katalog.yaml   ← operator declaration         (required)
  crd.yaml       ← CRD schema                   (required)
  go.mod         ← Go module — declares orkestra runtime version  (typed only)
  go.sum         ← Go checksum database          (typed only)
  Makefile       ← build targets (docker-build, push, etc.)       (typed only)
```

These files are included automatically when present — no extra flags needed on `ork push`.
`ork pull` prints `↳ Typed operator — go.mod, Makefile included` and warns when the
pattern's declared runtime version does not match the local `ork` binary.

### Metadata derivation

Metadata is read from `katalog.yaml` by `LoadArtifactMeta`. Fields:

```yaml
metadata:
  name: my-operator
  version: v1.0.0
  description: "Manages MyResource lifecycle"
  author: "Your Name"
  license: Apache-2.0
  tags: [databases, stateful]
```

`name` is required. `version` defaults to `latest` and `description` defaults to `"Pattern <name>"` if absent.

### Validation pipeline (on push)

Four layers run before any bytes are sent to the registry:

| Layer | What is checked |
|-------|----------------|
| `ValidateArtifactDirectory` | Required files present (`katalog.yaml`, `crd.yaml`) |
| `merger.New().Merge()` | Katalog YAML parses correctly; sources resolve |
| `katalog.Validate` | Full semantic validation: field types, GVK uniqueness, dependency graph |
| `validateCRDFile` | YAML parses; `kind: CustomResourceDefinition`; `spec.group` and `spec.names.kind` present |

### Publishing a pattern

```bash
# 1. Authenticate
docker login ghcr.io

# 2. Push — validation runs automatically before any network call
ork push my-operator:v1.0.0 ./my-operator/

# 3. Verify
ork inspect my-operator:v1.0.0
ork patterns
```

The index at `ghcr.io/orkspace/orkestra-registry/patterns/index:latest` is updated automatically after each push.

To push to a custom registry:

```bash
ork push oci://ghcr.io/myorg/patterns/my-operator:v1.0.0 ./my-operator/
# or via env var
ORK_REGISTRY=ghcr.io/myorg/patterns ork push my-operator:v1.0.0 ./my-operator/
```

For the official `ghcr.io/orkspace/orkestra-registry` registry, patterns are published by opening a PR against `github.com/orkspace/orkestra-registry`. CI validates the pattern against a live kind cluster and pushes on merge.

---

## Motifs (kind: Motif)

Motifs are reusable resource primitives — typically stateful infrastructure services (databases, message queues, caches) that are shared across multiple apps.

### Directory layout

```
my-motif/
  motif.yaml     ← motif declaration            (required)
  README.md      ← human documentation          (optional)
  example/       ← example usage directory      (optional)
```

### Metadata derivation

Metadata is read from `motif.yaml` by `LoadArtifactMeta`. The file must declare `kind: Motif`:

```yaml
kind: Motif
metadata:
  name: redis
  version: v7.2.0
  description: "Redis in-memory data store"
  author: "Your Name"
  license: MIT
  tags: [cache, stateful]
```

`name` is required. `version` defaults to `latest` and `description` defaults to `"Motif <name>"` if absent.

### Validation on push

`ValidateArtifactDirectory` checks that `motif.yaml` is present and readable. No CRD or katalog validation is performed — motifs are self-contained YAML templates.

### Publishing a motif

```bash
# 1. Authenticate
docker login ghcr.io

# 2. Push
ork push redis:v7.2.0 ./redis/

# 3. Verify
ork inspect redis:v7.2.0
ork patterns
```

The default motif registry is `ghcr.io/orkspace/orkestra-motifs`. The index at `ghcr.io/orkspace/orkestra-motifs/index:latest` is updated automatically.

To push to a custom motif registry:

```bash
ork push oci://ghcr.io/myorg/motifs/redis:v7.2.0 ./redis/
# or via env var
ORK_MOTIFS_REGISTRY=ghcr.io/myorg/motifs ork push redis:v7.2.0 ./redis/
```

---

## Extending the artifact format

### Adding a new file type to an existing kind

1. Add a `FileXxx` constant to `constant.go`.
2. Add a case to `mediaTypeForPatternFile` in `pattern.go`.
3. Add the filename to `OptionalFiles` (or `RequiredFiles`) in the relevant `PatternSpec` in `patternSpecs`.
4. Add the filename to `knownPatternFiles` in `pkg/merger/registry.go` so Git-sourced pulls attempt to fetch it.

The file is pushed as an OCI layer with the appropriate media type and an `org.opencontainers.image.title` annotation set to the filename. ORAS uses this annotation on pull to write the file to the correct path in the cache directory.

### Adding a new pattern kind

Add one entry to `patternSpecs` in `pattern.go`:

```go
KindFoo: {
    Kind:          KindFoo,
    MediaType:     "application/vnd.orkestra.foo.v1+tar+gzip",
    PrimaryFile:   "foo.yaml",
    RequiredFiles: []string{"foo.yaml"},
    OptionalFiles: []string{FileReadme},
},
```

No other changes are required. `DetectKind`, `ValidatePatternDirectory`, `LoadPatternMeta`, and the push/pull pipeline all work from `patternSpecs` automatically.
