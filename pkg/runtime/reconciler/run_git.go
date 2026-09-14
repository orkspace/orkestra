// pkg/reconciler/run_git.go
//
// Git hook dispatch — runs before external calls and resource groups in
// runTemplateReconcile.
//
// The Git hook performs a minimal clone/fetch of the declared repository,
// computes the current commit hash, detects whether the commit changed since
// the previous reconcile, and injects results into the resolver context via
// resolver.WithGit().
//
// Call sequence in runTemplateReconcile:
//
//  1. resolver = NewResolver(ctx, obj)
//  2. resolver = resolver.WithCross(ReadCross(...))      ← cross-CRD first
//  3. resolver, err = runGit(ctx, gvk, resolver, ...)    ← Git second
//  4. resolver, err = runExternal(ctx, ...)              ← HTTP third
//  5. resolver, err = runDocker(ctx, ...)                ← Docker fourth
//  6. runDeployments, runServices, ... etc.
//
// Results in template context:
//
//	.git.commit     → HEAD commit hash
//	.git.changed    → "true" if commit changed since last reconcile
//	.git.path       → working directory path
//	.git.error      → error message if operation failed
//	.git.called     → "true" if Git hook executed
//	.git.succeeded  → "true" if Git operation succeeded
//
// When: conditions can gate on these:
//
//	when:
//	  - field: git.changed
//	    equals: "true"
//
// Change detection is annotation-based — the last-seen commit is stored in:
//
//	orkestra.orkspace.io/last-commit
//
// This annotation survives pod restarts, preventing spurious rebuilds.
package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/metrics"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// runGit executes the declared Git hook and returns a new resolver with
// .git.* injected.
//
// kube and gvr are used to patch the orkestra.orkspace.io/last-commit
// annotation on the CR after each successful operation — this is how
// change detection survives pod restarts without writing to disk.
//
// Returns the enriched resolver. The original resolver is unchanged.
// If spec.ContinueOnError is false (default) and the Git operation fails,
// an error is returned and reconciliation stops.
func runGit(
	ctx context.Context,
	gvk string,
	resolver *orktmpl.Resolver,
	kube kubeclient.Interface,
	obj domain.Object,
	gvr schema.GroupVersionResource,
	spec *orktypes.GitHookSpec,
) (*orktmpl.Resolver, error) {
	if spec == nil {
		return resolver, nil
	}

	log := logger.FromContext(ctx)

	// Resolve template expressions in repo, branch, and path
	repo, err := resolver.Resolve(spec.Repo)
	if err != nil {
		return resolver, fmt.Errorf("git.repo: %w", err)
	}
	branch, err := resolver.Resolve(spec.Branch)
	if err != nil {
		return resolver, fmt.Errorf("git.branch: %w", err)
	}
	if branch == "" {
		branch = "main"
	}
	path, err := resolver.Resolve(spec.Path)
	if err != nil {
		return resolver, fmt.Errorf("git.path: %w", err)
	}
	if path == "" {
		path = "/workspace/" + obj.GetName()
	}

	// Read last commit from the CR annotation — survives pod restarts.
	// On first run this is empty, so changed will always be "true".
	lastCommit := ""
	if ann := obj.GetAnnotations(); ann != nil {
		lastCommit = ann[annotationLastCommit]
	}

	log.Debug().
		Str("repo", repo).
		Str("branch", branch).
		Str("path", path).
		Str("lastCommit", lastCommit).
		Msg("git: starting operation")

	// Execute the Git operation
	start := time.Now()
	result := executeGitOperation(ctx, repo, branch, path, lastCommit)
	duration := time.Since(start).Seconds()

	// Record metrics
	metrics.RecordGitOperation(
		gvk,
		repo,
		result.Operation,
		result.Error,
		duration,
	)

	withError := "false"
	if result.Error != "" {
		withError = "true"
		log.Warn().
			Str("repo", repo).
			Str("branch", branch).
			Str("path", path).
			Str("error", result.Error).
			Msg("git: operation failed")

		// Halt reconciliation unless explicitly told to continue
		if !spec.ContinueOnError {
			return resolver, fmt.Errorf("git: %s", result.Error)
		}
	} else {
		log.Debug().
			Str("repo", repo).
			Str("branch", branch).
			Str("path", path).
			Str("commit", result.Commit).
			Str("changed", result.Changed).
			Msg("git: operation succeeded")

		// Patch annotation only when we have a new commit — avoids a write
		// on every reconcile when nothing has changed.
		if result.Commit != "" && result.Commit != lastCommit {
			if patchErr := patchLastCommitAnnotation(ctx, kube, obj, result.Commit); patchErr != nil {
				// Non-fatal: the annotation patch failing doesn't mean the Git
				// operation failed. Log the warning but don't halt reconciliation.
				log.Warn().
					Str("commit", result.Commit).
					Err(patchErr).
					Msg("git: failed to persist last-commit annotation")
			}
		}
	}

	succeeded := "false"
	if result.Error == "" {
		succeeded = "true"
	}

	// Inject into resolver
	gitMap := map[string]interface{}{
		"commit":    result.Commit,
		"changed":   result.Changed,
		"path":      result.Path,
		"error":     result.Error,
		"called":    "true",
		"withError": withError,
		"succeeded": succeeded,
	}

	resolver = resolver.WithGit(gitMap)
	return resolver, nil
}
