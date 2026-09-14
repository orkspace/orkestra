# 07 — Adding a New Resource Type

This document walks through every file that must change to add a new resource type. The worked example is `ingresses.go`. Every step is labelled so you can use the checklist at the bottom to track progress.

**Recent additions you can use as further reference:**
- `pkg/runtime/runners/roles.go` + `pkg/resources/roles/role.go` — namespaced Role (RBAC)
- `pkg/runtime/runners/rolebindings.go` + `pkg/resources/rolebindings/rolebinding.go` — RoleBinding; demonstrates the immutable `roleRef` delete-recreate pattern in `Update`

## Overview of files to touch

```text
pkg/types/
    katalog_spec_hooks.go       — add IngressTemplateSource field to HookTemplates
    ingress.go                  — declare IngressTemplateSource (new file)

pkg/resources/
    ingresses/
        types.go                — ResolvedIngressSpec (new file)
        ingress.go              — Create, Update, Delete, DeleteIfOwned, Resolve (new file)

pkg/template/
    resolver.go                 — add ResolveIngressTemplate method

pkg/runtime/runners/
    ingresses.go                — RunIngresses function (new file)

pkg/runtime/reconciler/
    run_foreach.go              — add expandForEachIngresses
    run_template_reconcile.go   — wire runners.RunIngresses into runResourceGroup
```

---

## Step 1 — Declare the template source type

Create `pkg/types/ingress.go`:

```go
package types

// IngressTemplateSource is one entry in the ingresses: list under onCreate/onReconcile.
type IngressTemplateSource struct {
    Name        string      `yaml:"name"`
    Namespace   string      `yaml:"namespace,omitempty"`
    Host        string      `yaml:"host,omitempty"`
    ServiceName string      `yaml:"serviceName"`
    ServicePort string      `yaml:"servicePort"`
    Path        string      `yaml:"path,omitempty"`
    TLSSecret   string      `yaml:"tlsSecret,omitempty"`
    Labels      []KV        `yaml:"labels,omitempty"`
    Annotations []KV        `yaml:"annotations,omitempty"`
    Conditions  []Condition `yaml:"when,omitempty"`
    Or       []Condition `yaml:"or,omitempty"`
    Reconcile   bool        `yaml:"reconcile,omitempty"`
    ForEach     *ForEachSpec `yaml:"forEach,omitempty"`
}
```

Required fields:
- `Conditions` / `Or` — conditions support, maps to `when:` / `or:`.
- `Reconcile` — the `reconcile: true` shorthand.
- `ForEach` — forEach support (list field: `.item` = element; map field: `.item` = key, `.value` = map value).
- `Name`, `Namespace` — always present.

Add resource-specific fields for whatever the Kubernetes resource needs (`Host`, `ServiceName`, etc.).

---

## Step 2 — Add the field to HookTemplates

In `pkg/types/katalog_spec_hooks.go`, add the new field to `HookTemplates`:

```go
type HookTemplates struct {
    Secrets         []SecretTemplateSource         `yaml:"secrets,omitempty"`
    ConfigMaps      []ConfigMapTemplateSource       `yaml:"configMaps,omitempty"`
    ServiceAccounts []ServiceAccountTemplateSource  `yaml:"serviceAccounts,omitempty"`
    Deployments     []DeploymentTemplateSource      `yaml:"deployments,omitempty"`
    Services        []ServiceTemplateSource         `yaml:"services,omitempty"`
    Jobs            []JobTemplateSource             `yaml:"jobs,omitempty"`
    CronJobs        []CronJobTemplateSource         `yaml:"cronJobs,omitempty"`
    Ingresses       []IngressTemplateSource         `yaml:"ingresses,omitempty"` // ← add this
}
```

The YAML key (`ingresses`) is what operators write in their Katalog files.

---

## Step 3 — Create the registry package

Create `pkg/resources/ingresses/types.go`:

```go
package ingresses

// ResolvedIngressSpec is the fully-evaluated spec passed to Create/Update/Delete.
// All template expressions have already been resolved before this struct is populated.
type ResolvedIngressSpec struct {
    Name        string
    Namespace   string
    Host        string
    ServiceName string
    ServicePort int32
    Path        string
    TLSSecret   string
    Labels      map[string]string
    Annotations map[string]string
}
```

Create `pkg/resources/ingresses/ingress.go`:

```go
package ingresses

import (
    "context"
    "fmt"

    "github.com/orkspace/orkestra/domain"
    "github.com/orkspace/orkestra/pkg/konfig"
    "github.com/orkspace/orkestra/pkg/kubeclient"
    "github.com/orkspace/orkestra/pkg/logger"
    "github.com/orkspace/orkestra/pkg/utils"
    orktypes "github.com/orkspace/orkestra/pkg/types"
    networkingv1 "k8s.io/api/networking/v1"
    "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Create(ctx context.Context, kube kubeclient.KubeClient,
    owner domain.Object, spec ResolvedIngressSpec) error {

    ns := resolveNamespace(owner, spec)

    _, err := kube.Clientset().NetworkingV1().Ingresses(ns).Get(ctx, spec.Name, metav1.GetOptions{})
    if err == nil {
        logger.Debug().Str("ingress", spec.Name).Msg("ingress already exists — skipping create")
        return nil
    }
    if !errors.IsNotFound(err) {
        return fmt.Errorf("ingress.Create: checking existence of %q: %w", spec.Name, err)
    }

    ingress := buildIngress(owner, spec, ns)
    _, err = kube.Clientset().NetworkingV1().Ingresses(ns).Create(ctx, ingress, metav1.CreateOptions{})
    if err != nil {
        return fmt.Errorf("ingress.Create: %q in %q: %w", spec.Name, ns, err)
    }
    logger.Info().Str("ingress", spec.Name).Str("namespace", ns).Msg("ingress created")
    return nil
}

func Update(ctx context.Context, kube kubeclient.KubeClient,
    owner domain.Object, spec ResolvedIngressSpec) error {

    ns := resolveNamespace(owner, spec)

    existing, err := kube.Clientset().NetworkingV1().Ingresses(ns).Get(ctx, spec.Name, metav1.GetOptions{})
    if err != nil {
        if errors.IsNotFound(err) {
            return Create(ctx, kube, owner, spec)
        }
        return fmt.Errorf("ingress.Update: get %q: %w", spec.Name, err)
    }

    // Check for drift — update rules, host, TLS
    drifted := false
    updated := existing.DeepCopy()
    desired := buildIngress(owner, spec, ns)

    // Replace rules when host or paths differ
    if len(existing.Spec.Rules) == 0 || existing.Spec.Rules[0].Host != spec.Host {
        updated.Spec.Rules = desired.Spec.Rules
        drifted = true
    }
    if len(existing.Spec.TLS) == 0 && len(desired.Spec.TLS) > 0 {
        updated.Spec.TLS = desired.Spec.TLS
        drifted = true
    }

    if !drifted {
        logger.Debug().Str("ingress", spec.Name).Msg("ingress in sync — no update needed")
        return nil
    }

    _, err = kube.Clientset().NetworkingV1().Ingresses(ns).Update(ctx, updated, metav1.UpdateOptions{})
    if err != nil {
        return fmt.Errorf("ingress.Update: %q: %w", spec.Name, err)
    }
    logger.Info().Str("ingress", spec.Name).Str("namespace", ns).Msg("ingress updated")
    return nil
}

func DeleteIfOwned(ctx context.Context, kube kubeclient.KubeClient,
    owner domain.Object, name, namespace string) error {

    existing, err := kube.Clientset().NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
    if errors.IsNotFound(err) {
        return nil
    }
    if err != nil {
        return err
    }
    if existing.Labels[labels.OrkestraOwner] != owner.GetName() {
        return nil // not ours
    }
    return kube.Clientset().NetworkingV1().Ingresses(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func Resolve(src orktypes.IngressTemplateSource, ownerName string) ResolvedIngressSpec {
    spec := ResolvedIngressSpec{
        Name:        src.Name,
        Namespace:   src.Namespace,
        Host:        src.Host,
        ServiceName: src.ServiceName,
        Path:        src.Path,
        TLSSecret:   src.TLSSecret,
        Labels:      make(map[string]string),
        Annotations: make(map[string]string),
    }
    if spec.Name == "" {
        spec.Name = ownerName + "-ingress"
    }
    // Parse port
    if src.ServicePort != "" {
        fmt.Sscanf(src.ServicePort, "%d", &spec.ServicePort)
    }
    for _, l := range src.Labels {
        spec.Labels[l.Key] = l.Value
    }
    for _, a := range src.Annotations {
        spec.Annotations[a.Key] = a.Value
    }
    // System labels — always added
    spec.Labels[labels.ManagedKey]       = labels.ManagedValue
    spec.Labels[labels.OrkestraOwner] = ownerName
    return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildIngress(owner domain.Object, spec ResolvedIngressSpec, ns string) *networkingv1.Ingress {
    pathType := networkingv1.PathTypePrefix
    path := spec.Path
    if path == "" {
        path = "/"
    }

    ing := &networkingv1.Ingress{
        ObjectMeta: metav1.ObjectMeta{
            Name:        spec.Name,
            Namespace:   ns,
            Labels:      spec.Labels,
            Annotations: spec.Annotations,
			OwnerReferences: common.ResolveOwnerReferences(owner),
        },
        Spec: networkingv1.IngressSpec{
            Rules: []networkingv1.IngressRule{
                {
                    Host: spec.Host,
                    IngressRuleValue: networkingv1.IngressRuleValue{
                        HTTP: &networkingv1.HTTPIngressRuleValue{
                            Paths: []networkingv1.HTTPIngressPath{
                                {
                                    Path:     path,
                                    PathType: &pathType,
                                    Backend: networkingv1.IngressBackend{
                                        Service: &networkingv1.IngressServiceBackend{
                                            Name: spec.ServiceName,
                                            Port: networkingv1.ServiceBackendPort{
                                                Number: spec.ServicePort,
                                            },
                                        },
                                    },
                                },
                            },
                        },
                    },
                },
            },
        },
    }

    if spec.TLSSecret != "" {
        ing.Spec.TLS = []networkingv1.IngressTLS{
            {
                Hosts:      []string{spec.Host},
                SecretName: spec.TLSSecret,
            },
        }
    }

    return ing
}
```

---

## Step 4 — Add ResolveIngressTemplate to the resolver

In `pkg/template/resolver.go`, add:

```go
// ResolveIngressTemplate evaluates all template expressions in an IngressTemplateSource.
func (r *Resolver) ResolveIngressTemplate(src orktypes.IngressTemplateSource) (orktypes.IngressTemplateSource, error) {
    var err error
    if src.Name, err = r.resolveRequired(src.Name, "ingress.name"); err != nil {
        return src, err
    }
    src.Namespace, _ = r.Resolve(src.Namespace)
    src.Host, _      = r.Resolve(src.Host)
    src.ServiceName, _ = r.Resolve(src.ServiceName)
    src.ServicePort, _ = r.Resolve(src.ServicePort)
    src.Path, _      = r.Resolve(src.Path)
    src.TLSSecret, _ = r.Resolve(src.TLSSecret)
    for i := range src.Labels {
        src.Labels[i].Key, _   = r.Resolve(src.Labels[i].Key)
        src.Labels[i].Value, _ = r.Resolve(src.Labels[i].Value)
    }
    for i := range src.Annotations {
        src.Annotations[i].Key, _   = r.Resolve(src.Annotations[i].Key)
        src.Annotations[i].Value, _ = r.Resolve(src.Annotations[i].Value)
    }
    return src, nil
}
```

Look at `ResolveDeploymentTemplate` or `ResolveServiceTemplate` in the same file for the exact helper method names (`resolveRequired` vs `Resolve`).

---

## Step 5 — Write the runner

Create `pkg/runtime/runners/ingresses.go`. The canonical shape is in [pkg/runtime/runners/docs/01-runner-contract.md](../../runners/docs/01-runner-contract.md); apply it here with `Ingress` as the resource type:

```go
// pkg/runtime/runners/ingresses.go
package runners

import (
    "context"
    "fmt"

    "github.com/orkspace/orkestra/domain"
    "github.com/orkspace/orkestra/pkg/kubeclient"
    "github.com/orkspace/orkestra/pkg/logger"
    orkingress "github.com/orkspace/orkestra/pkg/resources/ingresses"
    orktmpl "github.com/orkspace/orkestra/pkg/template"
    orktypes "github.com/orkspace/orkestra/pkg/types"
)

func RunIngresses(
    ctx      context.Context,
    kube     kubeclient.KubeClient,
    resolver *orktmpl.Resolver,
    owner    domain.Object,
    srcs     []orktypes.IngressTemplateSource,
    update   bool,
    guard    func(ctx context.Context, obj domain.Object, ns string) bool,
) error {
    activeNames := make(map[string]bool, len(srcs))
    for _, s := range srcs {
        if !orktypes.EvaluateConditions(resolver.Data(), s.Conditions, s.Or, resolver.TemplateEvaluator()) {
            continue
        }
        n, _   := resolver.Resolve(s.Name)
        nsp, _ := resolver.Resolve(s.Namespace)
        if nsp == "" {
            nsp = owner.GetNamespace()
        }
        activeNames[nsp+"/"+n] = true
    }

    for i, src := range srcs {
        conditionPassed := orktypes.EvaluateConditions(resolver.Data(), src.Conditions, src.Or, resolver.TemplateEvaluator())

        name, _ := resolver.Resolve(src.Name)
        ns, _   := resolver.Resolve(src.Namespace)
        if ns == "" {
            ns = owner.GetNamespace()
        }

        if guard != nil && !guard(ctx, owner, ns) {
            continue
        }

        if !conditionPassed {
            if update || src.Reconcile {
                if !activeNames[ns+"/"+name] {
                    if err := orkingress.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
                        return fmt.Errorf("ingresses[%d]: conditional cleanup: %w", i, err)
                    }
                }
            }
            logger.FromContext(ctx).Debug().
                Str("resource", "Ingress").
                Int("index", i).
                Msg("conditions not met — skipping resource")
            continue
        }

        resolved, err := resolver.ResolveIngressTemplate(src)
        if err != nil {
            return fmt.Errorf("ingresses[%d]: %w", i, err)
        }

        spec := orkingress.Resolve(resolved, resolver.OwnerName())

        if update {
            if err := orkingress.Update(ctx, kube, owner, spec); err != nil {
                return fmt.Errorf("ingresses[%d].update: %w", i, err)
            }
        } else {
            if err := orkingress.Create(ctx, kube, owner, spec); err != nil {
                return fmt.Errorf("ingresses[%d].create: %w", i, err)
            }
            if src.Reconcile {
                if err := orkingress.Update(ctx, kube, owner, spec); err != nil {
                    return fmt.Errorf("ingresses[%d].reconcile: %w", i, err)
                }
            }
        }
    }
    return nil
}
```

---

## Step 6 — Add expandForEachIngresses

In `pkg/runtime/reconciler/run_foreach.go`, add:

```go
// ─────────────────────────────────────────────────────────────────────────────
// Ingress expansion
// ─────────────────────────────────────────────────────────────────────────────

func expandForEachIngresses(
    resolver *orktmpl.Resolver,
    srcs []orktypes.IngressTemplateSource,
) []orktypes.IngressTemplateSource {
    if !anyHasForEach(len(srcs), func(i int) *orktypes.ForEachSpec { return srcs[i].ForEach }) {
        return srcs
    }
    var result []orktypes.IngressTemplateSource
    for _, src := range srcs {
        if src.ForEach == nil {
            result = append(result, src)
            continue
        }
        for i, fi := range resolveForEachItems(resolver.Data(), src.ForEach.Field) {
            ir := itemResolver(resolver, fi, src.ForEach.As, i)
            expanded := src
            expanded.ForEach = nil
            expanded.Name, _        = ir.Resolve(src.Name)
            expanded.Namespace, _   = ir.Resolve(src.Namespace)
            expanded.Host, _        = ir.Resolve(src.Host)
            result = append(result, expanded)
        }
    }
    return result
}
```

---

## Step 7 — Wire into runResourceGroup

In `pkg/runtime/reconciler/run_template_reconcile.go`, inside `runResourceGroup`, add the call after the CronJobs call:

```go
if err := runners.RunIngresses(ctx, kube, resolver, obj,
    expandForEachIngresses(resolver, t.Ingresses), update, guard); err != nil {
    return err
}
```

The ordering within `runResourceGroup` does not affect correctness — pick a logical position (e.g. after Services, since Ingresses route to Services).

---

## Step 8 — Build and verify

```bash
go build ./...
```

A clean build is the acceptance criterion. No new tests are required for the runner itself — the registry package functions (`Create`, `Update`, `DeleteIfOwned`) are the testable units.

---

## Checklist

- [ ] `pkg/types/ingress.go` — `IngressTemplateSource` with `Conditions`, `Or`, `Reconcile`, `ForEach`
- [ ] `pkg/types/katalog_spec_hooks.go` — `Ingresses []IngressTemplateSource` field on `HookTemplates`
- [ ] `pkg/resources/ingresses/types.go` — `ResolvedIngressSpec`
- [ ] `pkg/resources/ingresses/ingress.go` — `Create`, `Update`, `DeleteIfOwned`, `Resolve`
- [ ] `pkg/template/resolver.go` — `ResolveIngressTemplate`
- [ ] `pkg/runtime/runners/ingresses.go` — `RunIngresses` with activeNames pre-pass
- [ ] `pkg/runtime/reconciler/run_foreach.go` — `expandForEachIngresses`
- [ ] `pkg/runtime/reconciler/run_template_reconcile.go` — call `runners.RunIngresses` in `runResourceGroup`
- [ ] `go build ./...` passes

## Common mistakes

**Forgetting the activeNames pre-pass.** Omitting it causes create/delete loops when two declarations target the same resource with mutually exclusive conditions. All runners that call `DeleteIfOwned` must have the pre-pass.

**Passing `owner` to EvaluateConditions instead of `resolver.Data()`.** The `owner` object does not have `.children.*`, `.external.*`, or `.cross.*` — conditions referencing those fields will silently fail.

**Resolving name/namespace only once.** `ResolveXxxTemplate` re-resolves internally. The early resolution in B2 is separate and intentional — it is used for the guard and `DeleteIfOwned` before the full resolution in B5.

**Forgetting `reconcile: true` support.** The `if src.Reconcile { Update(...) }` block in the non-update branch is how `reconcile: true` works. Without it, the shorthand is silently ignored.

**Not nil-checking the guard.** `guard` is nil when the CRD has no namespace restrictions. A nil dereference panics.

**Not adding the system labels in `Resolve`.** Without `LabelOrkestraOwner`, `DeleteIfOwned` will never match and stale resources will accumulate.

---

**↑ Back to** [README](README.md)
