// pkg/runners/jobs.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkjobs "github.com/orkspace/orkestra/pkg/resources/jobs"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// RunJobs resolves and applies Job template declarations.
//
// Jobs are fire-and-forget — they run once to completion and are not updated
// after creation. They are therefore always idempotent creates.
//
// Jobs appear almost exclusively under onDelete for cleanup tasks that must
// complete before Orkestra removes finalizers:
//   - Draining message queues before a consumer CR is deleted
//   - Archiving state to external storage
//   - Notifying external systems of deletion
//   - Running database migrations before removing a schema CR
//
// Jobs can also appear under onCreate for one-time provisioning tasks.
//
// Owner references are NOT set on onDelete Jobs because the owner CR is
// being deleted — the Job must complete independently after the CR is gone.
func RunJobs(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.JobTemplateSource,
	guard func(ctx context.Context, obj domain.Object, ns string) bool,
) error {
	for i, src := range srcs {
		// 1. Evaluate conditions BEFORE resolving templates
		conditionPassed := orktypes.EvaluateConditions(resolver.Data(), src.Conditions, src.Or, resolver.TemplateEvaluator())

		// Early name/ns resolution — needed for guard check.
		// Jobs are terminal (no DeleteIfOwned on condition fail), but guard
		// still prevents creating jobs in restricted namespaces.
		name, _ := resolver.Resolve(src.Name)
		_ = name // resolved for guard; ResolveJobTemplate re-resolves internally
		ns, _ := resolver.Resolve(src.Namespace)
		if ns == "" {
			ns = owner.GetNamespace()
		}

		// ── Namespace guard ───────────────────────────────────────────────────
		if guard != nil && !guard(ctx, owner, ns) {
			continue // skipped — CheckNamespace already logged the reason
		}

		if !conditionPassed {
			logger.FromContext(ctx).Debug().
				Str("resource", "Job").
				Int("index", i).
				Msg("conditions not met — skipping resource")

			continue
		}

		// 2. Resolve template expressions
		resolved, err := resolver.ResolveJobTemplate(src)
		if err != nil {
			return fmt.Errorf("jobs[%d]: %w", i, err)
		}

		// 3. Build registry spec and apply
		spec := orkjobs.Resolve(resolved, resolved.BackoffLimit, resolver.OwnerName(), resolver.Profiles())

		// Jobs are always creates — no update semantics
		if err := orkjobs.Create(ctx, kube, owner, spec); err != nil {
			return fmt.Errorf("jobs[%d].create: %w", i, err)
		}
	}
	return nil
}
