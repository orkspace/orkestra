// pkg/runners/pvs.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkpv "github.com/orkspace/orkestra/pkg/resources/pvs"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunPVs resolves and applies PersistentVolume template declarations.
// PVs are cluster-scoped; the namespace guard is intentionally not applied.
func RunPVs(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.PVTemplateSource,
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
					if err := orkpv.DeleteIfOwned(ctx, kube, owner, name); err != nil {
						return fmt.Errorf("pvs[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "PersistentVolume").
				Int("index", i).
				Msg("conditions not met — skipping resource")
			continue
		}

		resolved, err := resolver.ResolvePVTemplate(src)
		if err != nil {
			return fmt.Errorf("pvs[%d]: %w", i, err)
		}

		spec := orkpv.Resolve(resolved, resolver.OwnerName())

		if update {
			if err := orkpv.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("pvs[%d].update: %w", i, err)
			}
		} else {
			if err := orkpv.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("pvs[%d].create: %w", i, err)
			}
			if src.Reconcile {
				if err := orkpv.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("pvs[%d].reconcile: %w", i, err)
				}
			}
		}
	}
	return nil
}
