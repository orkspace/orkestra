# 05 — Top-Level Field Accumulation

When a Komposer references multiple source Katalogs, each source may declare its own top-level `security:`, `notification:`, and `providers:` blocks. These fields are accumulated so that `ork generate rbac` and `ork generate configmap` against a Komposer produce the same output as running them against the source Katalogs directly.

## The problem without accumulation

`loadKatalog` sets `m.security`, `m.notification`, and `m.providers` as side-effects when it loads each source Katalog. Without accumulation, the last source loaded overwrites all earlier ones. The Komposer's own (possibly Empty() block then further overwrites whatever the last source set.

Result: the Komposer appears to have no security or providers, even though its sources declare them.

## The solution

`loadKomposer` declares three accumulators before the source loops:

```go
var accSecurity     orktypes.KatalogSecurity
var accNotification *orktypes.KatalogNotification
var accProviders    []orktypes.KatalogProviderRequirement
```

After each source is loaded, the side-effects on `m` are captured:

```go
accSecurity     = mergeKatalogSecurity(accSecurity, m.security)
accNotification = mergeKatalogNotification(accNotification, m.notification)
accProviders    = append(accProviders, m.providers...)
```

At the end, the Komposer's own block is layered on top:

```go
m.security     = mergeKatalogSecurity(accSecurity, doc.Security)
m.notification = mergeKatalogNotification(accNotification, doc.Notification)
if len(doc.Providers) > 0 {
    m.providers = doc.Providers  // Komposer's own list replaces entirely
} else {
    m.providers = accProviders   // use accumulated list from imports
}
```

## Merge semantics per field

### `security` — `mergeKatalogSecurity(base, override)`

- Pointer fields (`DeletionProtection`, `Webhooks`, `Conversion`, `NamespaceProtection`): non-nil override wins; nil falls through to base.
- `ServiceName` string: non-empty override wins.
- Rationale: a Komposer should be able to tighten or loosen security without re-declaring every source field.

### `notification` — `mergeKatalogNotification(base, override)`

- If override is nil, return base unchanged.
- If base is nil, return override.
- If both non-nil: teams are merged by name — override teams win per key; base teams not in override are kept.
- `Defaults`: override's Defaults replaces base Defaults entirely if non-nil.
- Rationale: source Katalogs may declare team routing for their own CRDs; the Komposer can add or replace teams without wiping source teams.

### `providers` — append + replace

- All source providers are appended into `accProviders`.
- If the Komposer declares its own `providers:` list (non-Empty(), that list replaces `accProviders` entirely.
- Rationale: a Komposer that declares providers explicitly knows exactly what it needs; one that doesn't should inherit whatever its imports require.
