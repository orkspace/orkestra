# `pkg/resources/customresources` – Dynamic Client Operations for Third-Party CRDs

The `customresources` package provides idempotent, owner-reference-aware lifecycle management for arbitrary third-party Custom Resource Definitions. Unlike the typed registry packages (e.g. `deployments`, `services`) which work against the typed Kubernetes client, this package uses the **dynamic client** and REST mapper to operate on any CRD whose group/version/kind is known at runtime. It is the runtime engine that powers `customResources:` declarations in Orkestra hook templates.

---

## What Lives Here

| File | Purpose |
|---|---|
| `custom.go` | Four-function public API: `Create`, `Update`, `DeleteIfOwned`, `Resolve`. Internal helpers: `buildGVK`, `buildUnstructured`, `labelsToMap`, `validateSpec`. |
| `types.go` | `CustomResourceTemplateSource` — the raw (pre-template-evaluation) declaration struct that appears in hook YAML. |

---

## The Four-Function Contract

All registry packages, including this one, expose the same four-function surface:

### `Create(ctx, kube, owner, spec)`

Idempotent resource creation. Checks for existence first; if the resource is already present, delegates to `Update` instead of returning an error. This makes `onCreate` hooks safe to re-run. Sets owner references and Orkestra labels on every create.

### `Update(ctx, kube, owner, spec)`

Drift-correction reconcile. If the resource does not exist it creates it (missing-resource recovery). If it exists, compares the live state against the desired state and patches only when drift is detected. See [Drift Correction](#drift-correction-model) below.

### `DeleteIfOwned(ctx, kube, owner, name, namespace, apiVersion, kind)`

Guarded deletion. Resolves the GVR, fetches the resource, and deletes it only when both ownership conditions pass. See [Ownership Model](#ownership-model) below.

### `Resolve(src, ownerName)`

Converts a `CustomResourceTemplateSource` (which may still contain raw template expressions) into a `ResolvedCustomResourceSpec`. Template expression evaluation is the caller's responsibility and must happen before `Resolve` is called — see `pkg/template`. If `metadata.name` is empty after resolution, the name defaults to `<ownerName>-custom`.

---

## `hasStatus` Semantics

`HasStatus *bool` is a three-state hint that controls whether Orkestra reads or writes the `status` subresource of the target CRD:

| Value | Meaning |
|---|---|
| `nil` (omitted) | Auto-detect via REST discovery at runtime. Use this for the common case. |
| `true` | Force status awareness — Orkestra will include status reads in resolver context. |
| `false` | Suppress all status interaction — safe for CRDs that do not expose a `/status` subresource and would return a 404 or 405 on status writes. |

Orkestra reads child status to populate resolver template context. It **never writes** status to child CRs directly; status is the domain of the CRD's own controller.

---

## Drift Correction Model

`Update` performs drift detection in two independent passes before deciding to patch:

1. **`spec` block drift** — compares `existing.spec` against `desired.spec` using `reflect.DeepEqual`. Any structural difference triggers a patch.
2. **Top-level `Other` field drift** — iterates every key in the desired object that is not one of `apiVersion`, `kind`, `metadata`, `spec`, or `status`. This covers CRDs that place configuration at the document root rather than under `spec` (e.g. `data:`, `rules:`, `targets:`). A mismatch on any such key triggers a patch.

When a patch is needed, `Update` deep-copies the existing object and overwrites only the drifted fields (spec and/or other top-level keys), preserving any server-side fields (e.g. `resourceVersion`, `generation`, controller-owned annotations). Status is never touched during drift correction.

If neither pass detects drift, `Update` logs at DEBUG level and exits cleanly — no API call is made.

---

## Ownership Model

### Labels set on every managed resource

| Label | Value |
|---|---|
| `ork.io/managed` | `true` |
| `ork.io/owner` | `<ownerName>` (the Pipeline CR's name) |

These labels are merged with any user-declared labels from `metadata.labels`; user labels take lower precedence than Orkestra system labels.

### Owner references

`buildUnstructured` sets a Kubernetes `ownerReference` pointing at the Pipeline CR (controller: true, blockOwnerDeletion: true). This means Kubernetes garbage-collects the child CRD automatically when the owning Pipeline CR is deleted, without Orkestra needing an `onDelete` hook.

### `DeleteIfOwned` guard logic

`DeleteIfOwned` will **skip** deletion under two conditions, checked in order:

1. **orkdoctor-created** — if the resource carries the label `ork.io/created-by: orkdoctor`, it was provisioned by the `orkdoctor` CLI tool for diagnostic purposes and must not be deleted by the reconciler.
2. **Not owned** — if `ork.io/owner` does not match the calling owner's name, the resource belongs to a different Pipeline CR and must not be deleted.

If both checks pass, the resource is deleted via the dynamic client.

---

## `sleep:` Support

Every `ResolvedCustomResourceSpec` carries an optional `Sleep` field. When non-empty, `Create` and `Update` both call `shared.SleepIfNeeded(spec.Sleep)` before any API call. This injects an artificial latency into the reconcile loop for that specific resource.

Accepted duration units follow Orkestra's extended format: `s`, `m`, `h`, `d`, `w`, `mo`, `y`.

Typical use cases:

- **Latency simulation** — validate that upstream controllers tolerate slow CRD creation.
- **Chaos testing** — stress-test queue depth and autoscaler behaviour under reconcile backpressure.
- **Ordering probes** — verify that dependent resources handle delayed availability gracefully.

---

## See Also

- `pkg/runtime/reconciler/run_customresource.go` — how the reconciler drives `Create`/`Update` per lifecycle hook
- `pkg/runtime/reconciler/expand_customresources.go` — template expansion for `customResources:` declarations
- `pkg/template/` — the `Resolver` that evaluates Go templates before `Resolve` is called
- `pkg/resources/README.md` — top-level contract shared by all registry packages
