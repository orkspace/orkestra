// pkg/runners/clusterroles.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkcr "github.com/orkspace/orkestra/pkg/resources/clusterroles"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunClusterRoles resolves and applies ClusterRole template declarations.
//
// ClusterRoles are cluster-scoped — the namespace guard is not applied.
// Ownership is tracked via the orkestra.io/owner label; auto-GC via
// OwnerReferences is not possible for cluster-scoped resources.
func RunClusterRoles(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.ClusterRoleTemplateSource,
	update bool,
) error {
	activeNames := make(map[string]bool, len(srcs))
	for _, s := range srcs {
		if !orktypes.EvaluateConditions(resolver.Data(), s.Conditions, s.Or, resolver.TemplateEvaluator()) {
			continue
		}
		n, _ := resolver.Resolve(s.Name)
		activeNames[n] = true
	}

	for i, src := range srcs {
		conditionPassed := orktypes.EvaluateConditions(resolver.Data(), src.Conditions, src.Or, resolver.TemplateEvaluator())

		name, _ := resolver.Resolve(src.Name)

		if !conditionPassed {
			if update || src.Reconcile {
				if !activeNames[name] {
					if err := orkcr.DeleteIfOwned(ctx, kube, owner, name); err != nil {
						return fmt.Errorf("clusterRoles[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "ClusterRole").
				Int("index", i).
				Msg("conditions not met — skipping resource")
			continue
		}

		resolved, err := resolver.ResolveClusterRoleTemplate(src)
		if err != nil {
			return fmt.Errorf("clusterRoles[%d]: %w", i, err)
		}

		spec := orkcr.Resolve(resolved, resolver.OwnerName())

		if update {
			if err := orkcr.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("clusterRoles[%d].update: %w", i, err)
			}
		} else {
			if err := orkcr.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("clusterRoles[%d].create: %w", i, err)
			}
			if src.Reconcile {
				if err := orkcr.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("clusterRoles[%d].reconcile: %w", i, err)
				}
			}
		}
	}
	return nil
}
