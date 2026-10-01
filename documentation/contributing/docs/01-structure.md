# How the pipeline works

```text
documentation/          ← you write here
    getting-started/
    orkestra-core/
    orkestra-registry/
    reference/
    blog/
    publications/
    contributing/
    ...

website/
    scripts/sync-docs.sh   ← copies documentation/ → website/content/docs/
    content/docs/          ← Hugo reads from here (do not edit directly)
    layouts/               ← Hugo templates
    assets/css/            ← styles
    hugo.toml              ← site config, baseURL

.github/workflows/pages-deploy.yml   ← CI: sync → build → deploy
```

## What the sync does

`sync-docs.sh` runs before every Hugo build — locally and in CI. It:

1. Wipes `website/content/docs/` entirely (stale files from deleted pages never survive)
2. Copies every `.md` file from `documentation/` into the matching path under `website/content/docs/`
3. Injects Hugo front matter (`title`, `date`, `weight`) — derived from the first `# Heading` and git history
4. Rewrites `.md` link extensions to `/` (Hugo serves at paths, not `.md` files)
5. Converts MkDocs admonitions (`!!! note`) to Hugo callout shortcodes

**Never edit files inside `website/content/docs/` directly.** They are wiped on every sync. All edits go in `documentation/`.

## What the deploy does

On every push to `main` that touches `website/`, `docs/`, or `documentation/`:

1. CI installs Hugo extended
2. Runs `bash website/scripts/sync-docs.sh`
3. Runs `hugo --source ./website --minify --baseURL "https://orkestra.sh/"`
4. Deploys the built output to Cloudflare Pages

→ Next: [02-writing](02-writing.md)
