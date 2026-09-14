// pkg/runners/deployments.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkdeploy "github.com/orkspace/orkestra/pkg/resources/deployments"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunDeployments resolves and applies Deployment template declarations.
//
// update=false  onCreate path  — idempotent Create
// update=true   onReconcile path — Update for drift correction
//
// reconcile: true on an onCreate entry means also call Update on that
// same reconcile loop — the shorthand for "create it and keep it in sync"
// without a separate onReconcile declaration.
func RunDeployments(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.DeploymentTemplateSource,
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
		// ResolveDeploymentTemplate resolves these again internally — intentional, cheap.
		name, _ := resolver.Resolve(src.Name)
		ns, _ := resolver.Resolve(src.Namespace)
		if ns == "" {
			ns = owner.GetNamespace()
		}

		// ── Namespace guard ───────────────────────────────────────────────────
		if guard != nil && !guard(ctx, owner, ns) {
			continue // skipped — CheckNamespace already logged the reason
		}

		logger.FromContext(ctx).Debug().
			Str("resource", "Deployment").
			Str("name", name).
			Str("namespace", ns).
			Bool("namespace_restricted", guard != nil).
			Int("index", i).
			Bool("condition_passed", conditionPassed).
			Msg("deployment: condition evaluation")

		if !conditionPassed {
			if update || src.Reconcile {
				if !activeNames[ns+"/"+name] {
					if err := orkdeploy.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("deployments[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			continue
		}

		// 2. Resolve template expressions
		resolved, err := resolver.ResolveDeploymentTemplate(src)
		if err != nil {
			return fmt.Errorf("deployments[%d]: %w", i, err)
		}

		spec := orkdeploy.Resolve(resolved, resolver.OwnerName(), resolver.Profiles())

		if update {
			if err := orkdeploy.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("deployments[%d].update: %w", i, err)
			}
		} else {
			if err := orkdeploy.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("deployments[%d].create: %w", i, err)
			}

			// reconcile: true
			if src.Reconcile {
				if err := orkdeploy.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("deployments[%d].reconcile: %w", i, err)
				}
			}
		}

		// Workload autoscale — evaluated on every reconcile after create/update.
		if src.Autoscale != nil {
			if err := EvaluateWorkloadAutoscaleDeployment(ctx, kube, resolver,
				owner.GetName(), ns, name, src.Autoscale); err != nil {
				return fmt.Errorf("deployments[%d].autoscale: %w", i, err)
			}
		}
	}
	return nil
}
