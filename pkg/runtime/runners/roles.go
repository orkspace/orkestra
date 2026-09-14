// pkg/runners/roles.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkroles "github.com/orkspace/orkestra/pkg/resources/roles"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunRoles resolves and applies Role template declarations.
//
// On onCreate: idempotent create only.
// On onReconcile (update=true): creates or updates rules on existing Roles.
// Owner references ensure cleanup when the CR is deleted.
func RunRoles(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.RoleTemplateSource,
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
					if err := orkroles.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("roles[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "Role").
				Int("index", i).
				Msg("conditions not met — skipping resource")
			continue
		}

		resolved, err := resolver.ResolveRoleTemplate(src)
		if err != nil {
			return fmt.Errorf("roles[%d]: %w", i, err)
		}

		spec := orkroles.Resolve(resolved, resolver.OwnerName())

		if update || src.Reconcile {
			if err := orkroles.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("roles[%d].update: %w", i, err)
			}
		} else {
			if err := orkroles.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("roles[%d].create: %w", i, err)
			}
		}
	}
	return nil
}
