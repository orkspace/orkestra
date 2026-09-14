// pkg/runners/namespaces.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkns "github.com/orkspace/orkestra/pkg/resources/namespaces"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunNamespaces resolves and applies Namespace template declarations.
//
// Namespaces are create-only — there is nothing meaningful to update
// on a Namespace after creation (no spec fields that drift). They are
// therefore always idempotent creates regardless of whether this is called
// from onCreate or onReconcile.
//
// Owner references ensure cleanup when the CR is deleted.
func RunNamespaces(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.NamespaceTemplateSource,
	update bool,
) error {
	activeNames := make(map[string]bool, len(srcs))
	for _, s := range srcs {
		if !orktypes.EvaluateConditions(resolver.Data(), s.Conditions, s.Or, resolver.TemplateEvaluator()) {
			continue
		}
		n, _ := resolver.Resolve(s.Name)
		activeNames["orkestra.io"+"/"+n] = true
	}

	for i, src := range srcs {
		// 1. Evaluate conditions BEFORE resolving templates
		conditionPassed := orktypes.EvaluateConditions(resolver.Data(), src.Conditions, src.Or, resolver.TemplateEvaluator())

		// Early name resolution — needed for DeleteIfOwned cleanup.
		name, _ := resolver.Resolve(src.Name)

		if !conditionPassed {
			if update || src.Reconcile {
				if !activeNames["orkestra.io"+"/"+name] {
					if err := orkns.DeleteIfOwned(ctx, kube, owner, name); err != nil {
						return fmt.Errorf("namespace[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "Namespace").
				Int("index", i).
				Msg("conditions not met — skipping resource")

			continue
		}

		// 2. Resolve template expressions
		resolved, err := resolver.ResolveNamespaceTemplate(src)
		if err != nil {
			return fmt.Errorf("namespace[%d]: %w", i, err)
		}

		// 3. Build registry spec and apply
		spec := orkns.Resolve(resolved, resolver.OwnerName())

		// Always create — Namespaces have no meaningful drift to correct
		if err := orkns.Create(ctx, kube, owner, spec); err != nil {
			return fmt.Errorf("namespace[%d].create: %w", i, err)
		}
	}
	return nil
}
