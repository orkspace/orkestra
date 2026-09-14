// pkg/runners/clusterrolebindings.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkcrb "github.com/orkspace/orkestra/pkg/resources/clusterrolebindings"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunClusterRoleBindings resolves and applies ClusterRoleBinding template declarations.
//
// ClusterRoleBindings are cluster-scoped — the namespace guard is not applied.
// Ownership is tracked via the orkestra.io/owner label; auto-GC via
// OwnerReferences is not possible for cluster-scoped resources.
func RunClusterRoleBindings(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.ClusterRoleBindingTemplateSource,
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
					if err := orkcrb.DeleteIfOwned(ctx, kube, owner, name); err != nil {
						return fmt.Errorf("clusterRoleBindings[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "ClusterRoleBinding").
				Int("index", i).
				Msg("conditions not met — skipping resource")
			continue
		}

		resolved, err := resolver.ResolveClusterRoleBindingTemplate(src)
		if err != nil {
			return fmt.Errorf("clusterRoleBindings[%d]: %w", i, err)
		}

		spec := orkcrb.Resolve(resolved, resolver.OwnerName())

		if update {
			if err := orkcrb.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("clusterRoleBindings[%d].update: %w", i, err)
			}
		} else {
			if err := orkcrb.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("clusterRoleBindings[%d].create: %w", i, err)
			}
			if src.Reconcile {
				if err := orkcrb.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("clusterRoleBindings[%d].reconcile: %w", i, err)
				}
			}
		}
	}
	return nil
}
