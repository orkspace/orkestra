// pkg/runners/statefulsets.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orksts "github.com/orkspace/orkestra/pkg/resources/statefulsets"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunStatefulSets resolves and applies StatefulSet template declarations.
func RunStatefulSets(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.StatefulSetTemplateSource,
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
					if err := orksts.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("statefulsets[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "StatefulSet").
				Int("index", i).
				Msg("conditions not met — skipping resource")
			continue
		}

		resolved, err := resolver.ResolveStatefulSetTemplate(src)
		if err != nil {
			return fmt.Errorf("statefulsets[%d]: %w", i, err)
		}

		spec := orksts.Resolve(resolved, resolver.OwnerName(), resolver.Profiles())

		if update {
			if err := orksts.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("statefulsets[%d].update: %w", i, err)
			}
		} else {
			if err := orksts.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("statefulsets[%d].create: %w", i, err)
			}
			if src.Reconcile {
				if err := orksts.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("statefulsets[%d].reconcile: %w", i, err)
				}
			}
		}

		// Workload autoscale — evaluated on every reconcile after create/update.
		if src.Autoscale != nil {
			if err := EvaluateWorkloadAutoscaleStatefulSet(ctx, kube, resolver,
				owner.GetName(), ns, name, src.Autoscale); err != nil {
				return fmt.Errorf("statefulsets[%d].autoscale: %w", i, err)
			}
		}
	}
	return nil
}
