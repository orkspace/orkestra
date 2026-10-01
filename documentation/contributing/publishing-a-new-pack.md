# Publishing a New Pack

Adding an example to an existing pack requires no code changes — create the directory and files, done. Adding a **new top-level pack** requires changes in four places. Miss any of them and CI fails.

---

## Adding to an existing pack

Create the example directory under the right pack with the required files:

```text
examples/beginner/05-my-example/
  katalog.yaml
  crd.yaml
  cr.yaml
  README.md
  cleanup.sh      # optional
```

No code changes needed. CI picks it up automatically.

---

## Adding a new pack

A new top-level pack (e.g., `examples/maintenance/`) touches four files beyond the directory itself.

### 1. Create the pack directory

```text
examples/new-pack/
  example-1/
    katalog.yaml
    crd.yaml
    cr.yaml
    README.md
```

### 2. Update [examples/embed.go](https://github.com/orkspace/orkestra/blob/main/examples/embed.go)

Add the new pack name to the `//go:embed` directive. The current line is:

```go
//go:embed beginner intermediate advanced security use-cases registry-guide
var FS embed.FS
```

Add your pack name alongside the others:

```go
//go:embed beginner intermediate advanced security use-cases registry-guide new-pack
var FS embed.FS
```

The CLI uses this embedded filesystem to serve examples for `ork init --pack`.

### 3. Update [cmd/cli/init_packs.go](https://github.com/orkspace/orkestra/blob/main/cmd/cli/init_packs.go)

Add the pack to the `Packs` map. The `Pack` struct requires `Name`, `Description`, and `Path`:

```go
var Packs = map[string]Pack{
    // existing packs ...
    "new-pack": {
        Name:        "new-pack",
        Description: "One sentence describing what this pack covers.",
        Path:        "new-pack",
        Order:       9, // controls position in --list-packs output; increment from the last entry
    },
}
```

`Path` is the directory name inside the embedded FS. Set it to the full subdirectory path when the pack is nested.

Also add a helper and a `firstExample()` case so `ork init --list-packs` shows the right starting point:

```go
func (p Pack) isNewPackPack() bool { return p.Name == "new-pack" }
```

```go
case p.isNewPackPack():
    return "my-first-example"
```

### 4. Update [.github/workflows/package-examples.yml](https://github.com/orkspace/orkestra/blob/main/.github/workflows/package-examples.yml)

Add a `tar` command for the new pack in the packaging step:

```yaml
tar -czf "dist/examples_new-pack_${TAG}.tar.gz" \
  -C examples/new-pack .
```

Also add a line to the summary `echo` block so the pack appears in the CI build report:

```yaml
echo "| New Pack | Description of the pack | \`examples_new-pack_${TAG}.tar.gz\` |" >> "$GITHUB_STEP_SUMMARY"
```

### 5. Update [.github/workflows/sign-and-release.yml](https://github.com/orkspace/orkestra/blob/main/.github/workflows/sign-and-release.yml)

Add the artifact to the release upload list alongside the existing pack entries:

```yaml
dist/examples_new-pack_${{ github.ref_name }}.tar.gz
```

### 6. Update [examples/README.md](https://github.com/orkspace/orkestra/blob/main/examples/README.md)

Add the pack to the `--pack` list and add a new section in the learning path:

```markdown
ork init my-operator --pack new-pack   # One sentence description
```

```markdown
### New Pack — `--pack new-pack`

One sentence describing what belongs here.

| Example | What you learn |
|---------|----------------|
| [my-first-example](./new-pack/my-first-example/) | What this example teaches. |
```

### 7. Update [documentation/getting-started/01-learning-to-orkestrate/index.md](../getting-started/01-learning-to-orkestrate/index.md)

Add a row to the packs table:

```markdown
| [New Pack](./09-new-pack.md) | One sentence focus description |
```

---

## Checklist

- [ ] `examples/<pack>/` created with all required files
- [ ] `examples/embed.go` — pack name added to `//go:embed` (preserve existing entries)
- [ ] `cmd/cli/init_packs.go` — `Pack` struct added to `Packs` map with `Name`, `Description`, `Path`, `Order`
- [ ] `cmd/cli/init_packs.go` — `isPack()` helper and `firstExample()` case added
- [ ] `.github/workflows/package-examples.yml` — `tar` command added, summary `echo` added
- [ ] `.github/workflows/sign-and-release.yml` — artifact path added to release upload list
- [ ] `examples/README.md` — pack added to the `--pack` list and a new section in the learning path
- [ ] `documentation/getting-started/01-learning-to-orkestrate/index.md` — row added to the packs table
- [ ] E2E workflow added (optional but recommended)
