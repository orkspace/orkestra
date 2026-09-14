// pkg/runners/services.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orksvc "github.com/orkspace/orkestra/pkg/resources/services"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunServices resolves and applies Service template declarations.
// Same update/reconcile: true semantics as RunDeployments.
func RunServices(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.ServiceTemplateSource,
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

		name, _ := resolver.Resolve(src.Name)
		ns, _ := resolver.Resolve(src.Namespace)
		if ns == "" {
			ns = owner.GetNamespace()
		}

		// ── Namespace guard ───────────────────────────────────────────────
		// Called after namespace resolution so the actual target namespace
		// is known. Before this point the namespace may be a template expression.
		if guard != nil && !guard(ctx, owner, ns) {
			continue // skipped — CheckNamespace already logged the reason
		}

		if !conditionPassed {
			if update || src.Reconcile {
				if !activeNames[ns+"/"+name] {
					if err := orksvc.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("services[%d]: conditional cleanup: %w", i, err)
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
		resolved, err := resolver.ResolveServiceTemplate(src)
		if err != nil {
			return fmt.Errorf("services[%d]: %w", i, err)
		}

		// 3. Build registry spec and apply
		spec := orksvc.Resolve(resolved, resolver.OwnerName())

		if update {
			if err := orksvc.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("services[%d].update: %w", i, err)
			}
		} else {
			if err := orksvc.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("services[%d].create: %w", i, err)
			}

			// reconcile: true
			if src.Reconcile {
				if err := orksvc.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("services[%d].reconcile: %w", i, err)
				}
			}
		}
	}
	return nil
}
