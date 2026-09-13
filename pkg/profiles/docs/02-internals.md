# 02 — How profiles work

## Expansion pattern

Profiles are named shortcuts. When a user writes:

```yaml
resources:
  profile: medium
```

Orkestra replaces the entire `resources` block at katalog load time — before any resource builder or runtime code runs — with:

```yaml
resources:
  requests:
    cpu: 250m
    memory: 256Mi
  limits:
    cpu: "1"
    memory: 1Gi
```

The runtime (`pkg/resources`) and all downstream builders only ever receive fully-expanded structs. A profile name never reaches a resource builder.

## Lifetime of a profile

```
katalog.yaml loaded
       │
       ▼
pkg/katalog: validateResourceProfiles     ← rejects unknown names
pkg/katalog: validateSecurityProfiles     ← rejects unknown names + mixed usage
pkg/katalog: validateAutoscaleProfile     ← rejects unknown names + expands in place
       │
       ▼
pkg/katalog enrichment                    ← other profiles expanded via ResolveResources,
                                              ResolveContainerSecurityContext, etc.
       │
       ▼
pkg/resources: builders           ← receive only expanded structs, no profile names
       │
       ▼
Kubernetes API                            ← resource, security context, probe timings set
```

## Dependency graph

```
pkg/types      ←── pkg/profiles ←── pkg/katalog
                         │
                         └──────────── pkg/resources/shared
```

`pkg/profiles` imports only `pkg/types` and `pkg/utils`. Both `pkg/katalog` and `pkg/resources` import `pkg/profiles`. Neither imports the other — `pkg/profiles` is the clean meeting point.

## Template expressions

Profile values containing `{{` (Go template expressions) are skipped at katalog load time and validated at reconcile time instead. This allows profiles to be set from CR fields:

```yaml
resources:
  profile: '{{ .spec.resourceProfile | default "medium" }}'
```

Validation in `pkg/katalog` calls `isTemplateExpr(e.Profile)` before checking whether the name is known.

→ Next: [03-adding-a-profile.md](03-adding-a-profile.md)
