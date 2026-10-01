// pkg/runners/jobs.go
package runners

import (
	"context"
	"fmt"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkjobs "github.com/orkspace/orkestra/pkg/resources/jobs"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const jobCompletionPollInterval = 3 * time.Second

// RunJobs resolves and applies Job template declarations for onCreate.
// Owner references are set — Jobs are garbage collected with the CR.
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

// DefaultDeleteJobTimeout is the default deadline for cleanup Jobs when
// onDelete.timeout is not set.
const DefaultDeleteJobTimeout = 5 * time.Minute

// RunDeleteJobs resolves and applies Job template declarations for onDelete.
// No owner reference is set — the Job must outlive the CR being deleted.
// Blocks until each Job reaches Complete or Failed within deadline.
func RunDeleteJobs(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.JobTemplateSource,
	guard func(ctx context.Context, obj domain.Object, ns string) bool,
	deadline time.Time,
) error {
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
			logger.FromContext(ctx).Debug().
				Str("resource", "Job").
				Int("index", i).
				Msg("conditions not met — skipping resource")
			continue
		}

		resolved, err := resolver.ResolveJobTemplate(src)
		if err != nil {
			return fmt.Errorf("delete-jobs[%d]: %w", i, err)
		}

		spec := orkjobs.Resolve(resolved, resolved.BackoffLimit, resolver.OwnerName(), resolver.Profiles())

		if err := orkjobs.CreateForDelete(ctx, kube, owner, spec); err != nil {
			return fmt.Errorf("delete-jobs[%d].create: %w", i, err)
		}

		if err := waitForJobCompletionUntil(ctx, kube, ns, name, deadline); err != nil {
			return fmt.Errorf("delete-jobs[%d].wait: %w", i, err)
		}
	}
	return nil
}

// waitForJobCompletionUntil polls until the Job is Complete or Failed within deadline.
func waitForJobCompletionUntil(ctx context.Context, kube kubeclient.Interface, namespace, name string, deadline time.Time) error {
	log := logger.FromContext(ctx)
	for time.Now().Before(deadline) {
		job, err := kube.Clientset().BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("polling job %q: %w", name, err)
		}

		for _, c := range job.Status.Conditions {
			if c.Type == batchv1.JobComplete && c.Status == "True" {
				log.Info().Str("job", name).Msg("cleanup job completed")
				return nil
			}
			if c.Type == batchv1.JobFailed && c.Status == "True" {
				return fmt.Errorf("cleanup job %q failed: %s", name, c.Message)
			}
		}

		log.Debug().Str("job", name).Msg("cleanup job still running — polling")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jobCompletionPollInterval):
		}
	}
	return fmt.Errorf("timed out waiting for cleanup job %q to complete", name)
}
