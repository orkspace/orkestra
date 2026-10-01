# Adding a page or section

## Adding a page to an existing section

Create a `.md` file inside the relevant `documentation/` subdirectory:

```bash
# Example: new page under orkestra-registry
touch documentation/orkestra-registry/my-new-page.md
```

Start the file with a `# Heading` — the sync uses it as the page title. No front matter needed.

The page will be synced automatically and appear at `/docs/orkestra-registry/my-new-page/`.

To control where it appears in the sidebar, register it in `website/data/sidebar.yaml` under the relevant section. Pages not in `sidebar.yaml` are still reachable by URL but won't appear in navigation.

## Adding a new section

1. Create a directory under `documentation/`:

```bash
mkdir documentation/my-section
touch documentation/my-section/index.md
```

2. Add an entry to `website/data/sidebar.yaml`:

```yaml
- title: My Section
  section: my-section
  url: /docs/my-section/
```

The sync auto-creates a `_index.md` for any directory that doesn't have one — but providing an explicit `index.md` with a `# Heading` gives you control over the section landing page content.

## Sidebar structure

`website/data/sidebar.yaml` drives the left nav. Each entry is either a **section** (has sub-pages, shown as an expandable group) or a **page** (direct link, `page: true`):

```yaml
nav:
  - title: Getting Started      # section
    section: getting-started
    url: /docs/getting-started/

  - title: Deploying            # standalone page
    url: /docs/deploying/
    page: true
```

Pages within a section are discovered automatically from `website/content/docs/<section>/`. They are sorted by the `weight` field injected during sync — which tracks source file order alphabetically. Prefix filenames with numbers (`01-`, `02-`) to control order.

→ Next: [04-local](04-local.md)
