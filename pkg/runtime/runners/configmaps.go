// pkg/runners/configmaps.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkcm "github.com/orkspace/orkestra/pkg/resources/configmaps"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunConfigMaps resolves and applies ConfigMap template declarations.
//
// ConfigMaps support the same fromConfigMap and toNamespaces patterns as Secrets:
//
// fromConfigMap — copies data from an existing ConfigMap, with declared
// data entries overriding matching keys from the source. This is the
// "base config + environment override" pattern.
//
// toNamespaces — creates one copy in each listed namespace.
//
// reconcile: true — re-reads the source ConfigMap on every reconcile
// and syncs any changes. When logLevel changes in the CR spec, the
// ConfigMap in every namespace updates automatically.
func RunConfigMaps(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.ConfigMapTemplateSource,
	update bool,
	guard func(ctx context.Context, obj domain.Object, ns string) bool,
) error {
	activeNames := make(map[string]bool, len(srcs))
	for _, s := range srcs {
		if !orktypes.EvaluateConditions(resolver.Data(), s.Conditions, s.Or, resolver.TemplateEvaluator()) {
			continue
		}
		n, _ := resolver.Resolve(s.Name)
		nsp, _ := resolver.Resolve(s.Namespace)
		if nsp == "" {
			nsp = owner.GetNamespace()
		}
		activeNames[nsp+"/"+n] = true
	}

	for i, src := range srcs {
		// 1. Evaluate conditions BEFORE resolving templates
		conditionPassed := orktypes.EvaluateConditions(resolver.Data(), src.Conditions, src.Or, resolver.TemplateEvaluator())

		// Early name/ns resolution — needed for guard check and DeleteIfOwned cleanup.
		// ResolveConfigMapTemplate resolves these again internally — intentional, cheap.
		name, _ := resolver.Resolve(src.Name)
		ns, _ := resolver.Resolve(src.Namespace)
		if ns == "" {
			ns = owner.GetNamespace()
		}

		// ── Namespace guard ───────────────────────────────────────────────────
		if guard != nil && !guard(ctx, owner, ns) {
			continue // skipped — CheckNamespace already logged the reason
		}

		if !conditionPassed {
			if update || src.Reconcile {
				if !activeNames[ns+"/"+name] {
					if err := orkcm.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("configMaps[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "ConfigMap").
				Int("index", i).
				Msg("conditions not met — skipping resource")

			continue
		}

		// 2. Resolve template expressions
		resolved, err := resolver.ResolveConfigMapTemplate(src)
		if err != nil {
			return fmt.Errorf("configmaps[%d]: %w", i, err)
		}

		// 3. Build registry spec and apply
		spec := orkcm.Resolve(resolved, resolver.OwnerName())

		// toNamespaces — distribute to multiple namespaces, guarded per target
		if len(resolved.ToNamespaces) > 0 {
			namespaces, err := resolver.ResolveStringSlice(resolved.ToNamespaces)
			if err != nil {
				return fmt.Errorf("configmaps[%d].toNamespaces: %w", i, err)
			}

			shouldSync := update || src.Reconcile
			for _, targetNs := range namespaces {
				// Guard per target namespace — skip restricted, continue to allowed
				if guard != nil && !guard(ctx, owner, targetNs) {
					continue
				}
				nsSpec := spec
				nsSpec.Namespace = targetNs
				if shouldSync {
					if err := orkcm.Update(ctx, kube, owner, nsSpec); err != nil {
						return fmt.Errorf("configmaps[%d].update namespace=%s: %w", i, targetNs, err)
					}
				} else {
					if err := orkcm.Create(ctx, kube, owner, nsSpec); err != nil {
						return fmt.Errorf("configmaps[%d].create namespace=%s: %w", i, targetNs, err)
					}
				}
			}
			continue
		}

		// Single namespace
		if update {
			if err := orkcm.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("configmaps[%d].update: %w", i, err)
			}
		} else {
			if err := orkcm.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("configmaps[%d].create: %w", i, err)
			}
			if src.Reconcile {
				if err := orkcm.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("configmaps[%d].reconcile: %w", i, err)
				}
			}
		}
	}
	return nil
}
