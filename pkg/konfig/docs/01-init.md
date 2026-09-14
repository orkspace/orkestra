# 01 — Init and configuration loading

## Calling Init

`Init` is called once at process startup, before anything else:

```go
kfg, err := konfig.Init()
if err != nil {
    log.Fatal(err)
}
```

It reads ENV variables, applies defaults, normalises the environment string, and validates the struct with `go-playground/validator`. Any missing required field (currently `cluster.namespace` and `ork.name`) returns an error immediately.

Optionally pass `.env` file paths for local development:

```go
kfg, err := konfig.Init(".env", ".env.local")
```

`.env` loading uses `godotenv.Load` and is intentionally lenient — missing files are silently ignored, so the same call works in both local and in-cluster environments.

## Namespace resolution

The cluster namespace follows a specific resolution order:

1. `ORK_NAMESPACE` ENV var (if set and non-Empty()
2. `orkestra-system` — default when running inside a Pod
3. `default` — fallback when running outside a Pod (detected by `utils.IsRunningInCluster()`, which checks for the service account token file)

This means the same binary works in local development without any ENV configuration — it just targets the `default` namespace rather than `orkestra-system`.

## Environment normalisation

The `ORK_ENV` value is normalised to one of three canonical strings:

| Input(s) | Canonical |
|----------|-----------|
| `dev`, `development` | `development` |
| `uat`, `staging` | `staging` |
| `live`, `prod`, `production` | `production` |
| anything else | `development` |

Use `kfg.IsDev()`, `kfg.IsStaging()`, and `kfg.IsProduction()` for environment checks in business logic.

## Accessor pattern

All `Konfig` fields are unexported. Callers use typed accessor methods:

```go
kfg.Cluster().Namespace     // string
kfg.Katalog().DefaultResync // time.Duration
kfg.Security()              // *SecurityConfig
kfg.Notification()          // *NotificationConfig
kfg.Health().Port           // string
kfg.Konductor().LeaseDuration // time.Duration
```

All accessors return pointers to the embedded struct — mutations reflect back on `Konfig`. Only modify through the accessor when you understand the downstream effects (e.g. the Katalog loader intentionally sets `security.*` and `notification.*` fields to apply YAML overrides).

## Document kinds

`konfig` also owns the canonical document kind strings for Orkestra's YAML formats. Use the helper functions rather than hardcoding strings:

```go
konfig.IsKatalogKind(kind)   // "Katalog"
konfig.IsKomposerKind(kind)  // "Komposer"
konfig.IsMotifKind(kind)     // "Motif"
konfig.IsValidPatternKind(kind)
konfig.ValidKindsString()    // "Katalog, Komposer, Motif, E2E"
konfig.IsValidApiVersion(v)  // checks against apiVersions slice
```

→ Next: [02-security.md](02-security.md)
