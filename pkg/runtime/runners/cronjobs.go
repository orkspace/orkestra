// pkg/runners/cronjobs.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkcron "github.com/orkspace/orkestra/pkg/resources/cronjobs"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunCronJobs resolves and applies CronJob template declarations.
//
// CronJobs are long-lived scheduled resources — created under onCreate and
// drift-corrected under onReconcile (or reconcile: true).
//
// Common use cases:
//   - Periodic sync jobs (cache warming, data replication)
//   - Scheduled backup jobs
//   - Recurring cleanup or archival tasks
//   - Health check or audit jobs on a schedule
//
// The schedule field supports both static cron expressions and dynamic
// values from the CR spec:
//
//	schedule: "0 * * * *"               static — every hour
//	schedule: "{{ .spec.syncSchedule }}" dynamic — from CR spec
func RunCronJobs(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.CronJobTemplateSource,
	update bool,
	guard func(ctx context.Context, obj domain.Object, ns string) bool,
) error {
	// Pre-pass: collect (ns/name) pairs that have at least one passing condition.
	// A failing-condition path must not delete a resource that a passing-condition
	// path in the same block will create — e.g. two paths with mutually exclusive
	// typeOf conditions both targeting {{ .metadata.name }}.
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
				// Skip deletion when another declaration with a passing condition
				// targets the same resource — that path owns the resource.
				if !activeNames[ns+"/"+name] {
					if err := orkcron.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("cronJobs[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "CronJob").
				Int("index", i).
				Msg("conditions not met — skipping resource")

			continue
		}

		// 2. Resolve template expressions
		resolved, err := resolver.ResolveCronJobTemplate(src)
		if err != nil {
			return fmt.Errorf("cronjobs[%d]: %w", i, err)
		}

		// 3. Build registry spec and apply
		spec := orkcron.Resolve(resolved, resolver.OwnerName(), resolver.Profiles())

		if update {
			if err := orkcron.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("cronjobs[%d].update: %w", i, err)
			}
		} else {
			if err := orkcron.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("cronjobs[%d].create: %w", i, err)
			}
			if src.Reconcile {
				if err := orkcron.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("cronjobs[%d].reconcile: %w", i, err)
				}
			}
		}
	}
	return nil
}
