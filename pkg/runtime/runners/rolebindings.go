// pkg/runners/rolebindings.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkrb "github.com/orkspace/orkestra/pkg/resources/rolebindings"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunRoleBindings resolves and applies RoleBinding template declarations.
//
// On onCreate: idempotent create only.
// On onReconcile (update=true): creates or updates (or recreates when roleRef changed).
// Owner references ensure cleanup when the CR is deleted.
func RunRoleBindings(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.RoleBindingTemplateSource,
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
					if err := orkrb.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("roleBindings[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "RoleBinding").
				Int("index", i).
				Msg("conditions not met — skipping resource")
			continue
		}

		resolved, err := resolver.ResolveRoleBindingTemplate(src)
		if err != nil {
			return fmt.Errorf("roleBindings[%d]: %w", i, err)
		}

		spec := orkrb.Resolve(resolved, resolver.OwnerName())

		if update || src.Reconcile {
			if err := orkrb.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("roleBindings[%d].update: %w", i, err)
			}
		} else {
			if err := orkrb.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("roleBindings[%d].create: %w", i, err)
			}
		}
	}
	return nil
}
