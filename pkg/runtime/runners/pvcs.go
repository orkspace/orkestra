// pkg/runners/pvcs.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkpvc "github.com/orkspace/orkestra/pkg/resources/pvcs"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunPVCs resolves and applies PersistentVolumeClaim template declarations.
func RunPVCs(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.PVCTemplateSource,
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
		conditionPassed := orktypes.EvaluateConditions(resolver.Data(), src.Conditions, src.Or, resolver.TemplateEvaluator())

		name, _ := resolver.Resolve(src.Name)
		ns, _ := resolver.Resolve(src.Namespace)
		if ns == "" {
			ns = owner.GetNamespace()
		}

		if guard != nil && !guard(ctx, owner, ns) {
			continue
		}

		if !conditionPassed {
			if update || src.Reconcile {
				if !activeNames[ns+"/"+name] {
					if err := orkpvc.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("pvcs[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "PersistentVolumeClaim").
				Int("index", i).
				Msg("conditions not met — skipping resource")
			continue
		}

		resolved, err := resolver.ResolvePVCTemplate(src)
		if err != nil {
			return fmt.Errorf("pvcs[%d]: %w", i, err)
		}

		spec := orkpvc.Resolve(resolved, resolver.OwnerName())

		if update {
			if err := orkpvc.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("pvcs[%d].update: %w", i, err)
			}
		} else {
			if err := orkpvc.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("pvcs[%d].create: %w", i, err)
			}
			if src.Reconcile {
				if err := orkpvc.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("pvcs[%d].reconcile: %w", i, err)
				}
			}
		}
	}
	return nil
}
