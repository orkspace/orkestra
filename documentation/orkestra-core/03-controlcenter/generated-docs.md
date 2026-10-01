# Generated CRD Documentation

The Control Center generates a live documentation page for every CRD in every connected Katalog. No hand-writing required — the docs are derived directly from the running operator at page-load time.

Navigate to it from any CRD card via the document icon, or from the Docs landing page at `http://localhost:8081/docs`.

---

## What it covers

Each CRD gets its own page. The sections shown depend on what the operator has configured — sections for features not in use are omitted rather than shown empty.

| Section | Always shown | Shown when |
|---|---|---|
| Overview | ✓ | — |
| Reconcile Mode | ✓ | — |
| Configuration | ✓ | endpoints not fully disabled |
| Dependencies | — | `dependsOn:` declared |
| Child Resources | — | `onCreate`/`onReconcile` templates present |
| operatorBox | — | endpoints not fully disabled |
| kubectl Reference | ✓ | — |
| Access Control | — | RBAC rules present |
| Webhooks | — | admission or conversion webhooks enabled |
| Protection | — | deletion or namespace protection enabled |
| Endpoints | ✓ | — |

---

## Overview

Shows the API version, Kind, GVR, scope (namespaced vs cluster-scoped), and the cross-read access policy for this CRD.

**Cross-read access** reflects the `crossAccess:` field in the Katalog:

- ✓ **allowed** — sibling operators can read this CRD's CR state via `cross:`
- ⊘ **denied** — `crossAccess: false` blocks all inbound cross-reads; the CRD can still read others

---

## Reconcile Mode

Explains whether the CRD runs in `dynamic` or `typed` mode and what that means for the reconciler implementation.

- **Dynamic** — reconcile logic is declared in YAML using template expressions. The Generic Reconciler owns the full CR lifecycle.
- **Typed** — Orkestra decodes each CR into a concrete Go type. Reconcile hooks receive a fully typed object. If a constructor is configured, it returns a fully custom reconciler.

---

## Configuration

Worker count, resync interval, and queue depth as configured and reported by the runtime. If the autoscaler is enabled, the current effective worker count and in-flight reconciles are shown.

---

## Child Resources

Lists the Kubernetes resource kinds the operator creates and manages on behalf of each CR — Secrets, ConfigMaps, Deployments, and so on. Sourced from the `operatorBox.templates` summary reported by the runtime. Each entry shows the kind, count, and which lifecycle phases (`onCreate`, `onReconcile`, `onDelete`) declare it.

---

## operatorBox

A compact summary of the reconciler configuration:

- **Hooks** — whether reconcile hooks are configured, and from which source/function
- **Constructor** — whether a custom constructor is in use
- **Finalizers** — declared finalizer strings

---

## Endpoints

Always shown. Lists the three per-CRD HTTP endpoints with their paths and live status:

| Endpoint | Path | Disabled by |
|---|---|---|
| Health | `/katalog/{crd}/health` | `endpoints: health: false` |
| Info | `/katalog/{crd}` | `endpoints: enabled: false` |
| CR List | `/katalog/{crd}/cr` | `endpoints: enabled: false` |

When an endpoint is disabled the row shows ⊘ and the Katalog field that controls it, so the reason is visible at a glance without opening the Katalog YAML.

---

## Disabled endpoints and the docs page

The docs page is intentionally resilient to disabled endpoints. When the info endpoint is off, static fields (GVK, GVR, Mode, scope, description) are sourced from the `/katalog` summary, which always responds. Sections that require live runtime data (Configuration, operatorBox) are hidden rather than shown with zeros.

A CRD with `endpoints: enabled: false` still gets a complete Overview, correct kubectl commands, and a clear Endpoints section explaining what is disabled and why.
