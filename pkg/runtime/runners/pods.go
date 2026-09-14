// pkg/runners/pods.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkpods "github.com/orkspace/orkestra/pkg/resources/pods"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunPods resolves and applies Pod template declarations.
//
// update=false  onCreate path  — idempotent Create
// update=true   onReconcile path — Update for drift correction (delete + recreate on image drift)
//
// reconcile: true on an onCreate entry means also call Update on that
// same reconcile loop — the shorthand for "create it and keep it in sync"
// without a separate onReconcile declaration.
func RunPods(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.PodTemplateSource,
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

		logger.FromContext(ctx).Debug().
			Str("resource", "Pod").
			Str("name", name).
			Str("namespace", ns).
			Int("index", i).
			Bool("condition_passed", conditionPassed).
			Msg("pod: condition evaluation")

		if !conditionPassed {
			if update || src.Reconcile {
				if !activeNames[ns+"/"+name] {
					if err := orkpods.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("pods[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			continue
		}

		resolved, err := resolver.ResolvePodTemplate(src)
		if err != nil {
			return fmt.Errorf("pods[%d]: %w", i, err)
		}

		spec := orkpods.Resolve(resolved, resolver.OwnerName(), resolver.Profiles())

		if update {
			if err := orkpods.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("pods[%d].update: %w", i, err)
			}
		} else {
			if err := orkpods.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("pods[%d].create: %w", i, err)
			}

			if src.Reconcile {
				if err := orkpods.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("pods[%d].reconcile: %w", i, err)
				}
			}
		}
	}

	return nil
}
