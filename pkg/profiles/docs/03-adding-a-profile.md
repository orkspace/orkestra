# 03 — Adding a profile

There are three ways to add a profile, in order of which you should reach for first.

---

## 1. User-defined profiles (recommended — no Go code required)

For org-specific presets, declare the profile in the `profiles:` block of your Katalog or Motif. No Pull Request. No code review. No binary update.

```yaml
profiles:
  resourceQuotas:
    - name: org-medium
      description: Standard allocation for a team namespace
      hard:
        pods: "25"
        cpu: "4"
        memory: "8Gi"
        requests.cpu: "2"
        requests.memory: "4Gi"

  networkPolicies:
    - name: org-allow-monitoring
      description: Allow ingress from the platform monitoring namespace
      ingress:
        - from:
            - namespaceSelector:
                team: platform
      policyTypes: [Ingress]

  limitRanges:
    - name: org-container-defaults
      limits:
        - type: Container
          default:
            cpu: 500m
            memory: 512Mi
          defaultRequest:
            cpu: 100m
            memory: 128Mi

  hpa:
    - name: org-conservative
      targetCPUUtilizationPercentage: "70"
      behavior:
        scaleDown:
          stabilizationWindowSeconds: 300

  pdb:
    - name: org-at-least-one
      minAvailable: "1"

  rollingUpdate:
    - name: org-safe
      maxSurge: "1"
      maxUnavailable: "0"
```

Reference the profile by name in your spec:

```yaml
spec:
  crds:
    namespaceclaim:
      operatorBox:
        onCreate:
          resourceQuotas:
            - name: "{{ .metadata.name }}-quota"
              profile: org-medium
          networkPolicies:
            - name: "{{ .metadata.name }}-baseline"
              podSelector: {}
              profile: org-allow-monitoring
          limitRanges:
            - name: "{{ .metadata.name }}-limits"
              profile: org-container-defaults
          deployments:
            - name: "{{ .metadata.name }}-agent"
              image: myorg/agent:v1
              rollingUpdate:
                profile: org-safe
          hpa:
            - name: "{{ .metadata.name }}-agent-hpa"
              scaleTargetRef:
                apiVersion: apps/v1
                kind: Deployment
                name: "{{ .metadata.name }}-agent"
              minReplicas: "1"
              maxReplicas: "5"
              behavior:
                profile: org-conservative
          pdb:
            - name: "{{ .metadata.name }}-agent-pdb"
              selector:
                app: agent
              behavior:
                profile: org-at-least-one
```

Run `ork validate` to confirm all profile references resolve:

```text
ork validate -f katalog.yaml
```

**Profile field placement by class:**

| Class | Profile field location |
|-------|----------------------|
| `networkPolicies` | `profile:` on the entry (top-level) |
| `resourceQuotas` | `profile:` on the entry (top-level) |
| `limitRanges` | `profile:` on the entry (top-level) |
| `hpa` | `behavior.profile:` on the HPA entry |
| `pdb` | `behavior.profile:` on the PDB entry |
| `rollingUpdate` | `rollingUpdate.profile:` on the Deployment/StatefulSet entry |

**Template expressions are supported in profile field values.** Fields containing `{{` are resolved at reconcile time and skipped at `ork validate` time.

**Profile names are validated at load time.** An unknown static name is a hard error. An unknown template expression is validated at reconcile time.

See [User-defined profiles](../../../documentation/concepts/profiles/10-user-defined-profiles.md) for the full reference.

---

## 2. Adding a name to an existing built-in class

Only relevant for contributors adding presets to the Orkestra binary itself. If your preset is org-specific, use path 1 instead.

**Example: adding `xlarge` to resource profiles.**

**1.** Add the constant in `pkg/profiles/resource.go`:

```go
ResourceXLarge ResourceProfile = "xlarge"
```

**2.** Add the expansion case in `ApplyResourceProfile`:

```go
case ResourceXLarge:
    return &orktypes.ResourceRequirements{
        Requests: map[string]string{"cpu": "2", "memory": "4Gi"},
        Limits:   map[string]string{"cpu": "8", "memory": "8Gi"},
    }, nil
```

**3.** Add to `IsValidResourceProfile`:

```go
case ResourceTiny, ..., ResourceXLarge:
    return true
```

**4.** Update the error message in `ApplyResourceProfile` to list `"xlarge"` in the allowed names.

**5.** Add a test in `resource_test.go`.

**6.** Add a fixture entry in `pkg/profiles/fixture/katalog-resource.yaml`.

**7.** Update the reference table in `docs/01-profiles.md`.

---

## 3. Adding a new profile class (new resource type)

Only needed when Orkestra adds support for a resource type that has no profile class yet. The LimitRange class is the most recent example.

**1. Add the `*ProfileDef` type** to `pkg/types/types_profiles.go` — name, description, and the fields that the profile expands into:

```go
type LimitRangeProfileDef struct {
    Name        string           `yaml:"name" json:"name"`
    Description string           `yaml:"description,omitempty"`
    Limits      []LimitRangeItem `yaml:"limits" json:"limits"`
}
```

**2. Add the class** to `ProfileRegistry` and wire `Empty`, `Lookup*`, and `Merge`:

```go
type ProfileRegistry struct {
    // ...existing fields...
    LimitRanges []LimitRangeProfileDef `yaml:"limitRanges,omitempty"`
}
```

**3. Add a `Profile` field** to the template source type in `pkg/types/types_<resource>.go`:

```go
Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
```

**4. Add a profile entry collector** in `pkg/types/hooks_<resource>_profile.go` — define `<Resource>ProfileEntry`, add `Get<Resource>Profile()` interface and implement it on the template source, and add `Collect<Resource>ProfileEntries()` on `*CRDEntry` using `VisitResources`.

**5. Create `pkg/profiles/<resource>.go`** — `Apply<Resource>Profile(name, reg)` looks up user registry first, then built-ins (or returns an error if there are no built-ins for the class). Also export `IsValid<Resource>Profile`.

**6. Update `pkg/resources/<resource>/<resource>.go`** — add `reg orktypes.ProfileRegistry` as a third parameter to `Resolve()` and apply the profile when `src.Profile != ""`.

**7. Update `pkg/runtime/runners/<resource>.go`** — pass `resolver.Profiles()` to `Resolve()`.

**8. Add `pkg/katalog/validate_<resource>_profile.go`** — `validate<Resource>Profiles()` using `Collect<Resource>ProfileEntries()`. Call it from `Validate()` in `validate.go`.

**9. Update `pkg/katalog/validate_user_profiles.go`** — three additions:

- Add a `<resource>DefNames()` helper that extracts the `Name` field from a `[]<Resource>ProfileDef` slice (same pattern as `npDefNames`, `rqDefNames`, etc.).
- Add an entry to the `checks` slice inside `validateUserProfiles()` for the new class, pointing to the new helper and (if the class has built-ins) `profiles.IsValid<Resource>Profile` as the `isBuiltin` func. Use `nil` for `isBuiltin` if the class has no built-ins (like LimitRange).
- Add `isUser<Resource>Profile()` as a method on `*Katalog` that calls `k.Profiles.Lookup<Resource>(name)` — used by `validate_<resource>_profile.go` to check user profiles before rejecting an unknown name.

**10. Wire the new class through the merger** so that `profiles:` declared in a Katalog YAML actually reaches `Katalog.Profiles` at validate and reconcile time. Without this step, all profile references will fail validation even when the name is correctly declared.

- `pkg/merger/merger.go` — add the class to the `profiles` field (it's a `ProfileRegistry`, so no change needed there — just make sure `ToProfiles()` exists).
- `pkg/merger/file.go`, `loadKatalog` — no change needed; `m.profiles = doc.Profiles` already covers all classes via the shared `ProfileRegistry`.
- `pkg/merger/file.go`, `loadKomposer` — add `accProfiles, _ = accProfiles.Merge(m.profiles, source)` alongside the existing `accSecurity`/`accProviders` lines at each import step, and `merged, _ := accProfiles.Merge(doc.Profiles, path); m.profiles = merged` at the final merge block.
- `pkg/katalog/parser.go`, `KomposeRuntimeKatalog` — add `k.Profiles = m.ToProfiles()` alongside the existing `k.Security = m.ToSecurity()` calls.

This step is only needed when adding the *first* new class to `ProfileRegistry`. If `ProfileRegistry` already has a `ToProfiles()` path (it does as of LimitRange), the new class field is carried automatically.

**11. Rebuild and validate** — `make ork && ork validate -f your-example.yaml`.

**12. Add a test fixture** in `pkg/profiles/fixture/`.

**13. Add to the use-case examples** under `examples/use-cases/profiles/` or `examples/use-cases/namespace-provisioner/`.

---

## Rules

- `Apply*Profile` must check the user `ProfileRegistry` first, then fall back to built-ins. Return an error for unknown names — never silently fall back.
- `IsValid*Profile` for built-in classes must stay in sync with the `Apply*Profile` switch. For user-only classes (like LimitRange), `IsValid*Profile` takes a registry argument.
- Profile names are case-insensitive for built-in names: normalize with `strings.ToLower`. User-defined names are matched exactly.
- Template expressions (`{{`) are always skipped at load time — do not add static validation for them in `pkg/profiles`.
- Profile and explicit fields are mutually exclusive. Enforce this in `pkg/katalog` validation, not in `pkg/profiles` or `pkg/resources`.
