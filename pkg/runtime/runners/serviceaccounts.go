// pkg/runners/serviceaccounts.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orksa "github.com/orkspace/orkestra/pkg/resources/serviceaccounts"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunServiceAccounts resolves and applies ServiceAccount template declarations.
//
// ServiceAccounts are create-only — there is nothing meaningful to update
// on a ServiceAccount after creation (no spec fields that drift). They are
// therefore always idempotent creates regardless of whether this is called
// from onCreate or onReconcile.
//
// Owner references ensure cleanup when the CR is deleted.
func RunServiceAccounts(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.ServiceAccountTemplateSource,
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
					if err := orksa.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("serviceAccounts[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "ServiceAccount").
				Int("index", i).
				Msg("conditions not met — skipping resource")

			continue
		}

		// 2. Resolve template expressions
		resolved, err := resolver.ResolveServiceAccountTemplate(src)
		if err != nil {
			return fmt.Errorf("serviceaccounts[%d]: %w", i, err)
		}

		// 3. Build registry spec and apply
		spec := orksa.Resolve(resolved, resolver.OwnerName())

		// Always create — ServiceAccounts have no meaningful drift to correct
		if err := orksa.Create(ctx, kube, owner, spec); err != nil {
			return fmt.Errorf("serviceaccounts[%d].create: %w", i, err)
		}
	}
	return nil
}
