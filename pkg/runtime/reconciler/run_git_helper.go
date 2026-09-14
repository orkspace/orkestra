// pkg/reconciler/run_git_helper.go

package reconciler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const annotationLastCommit = "orkestra.orkspace.io/last-commit"

type GitOperationResult struct {
	Operation string // "clone" | "fetch"
	Commit    string
	Changed   string // "true" | "false"
	Path      string
	Error     string
}

// executeGitOperation performs a minimal Git checkout/update.
//
// Behavior:
//   - If the working directory does not exist → clone
//   - If it exists → fetch + fast-forward
//   - Reads HEAD commit hash
//   - Detects whether commit changed by comparing to lastCommit
//
// lastCommit is the commit hash from the CR annotation set on the previous
// reconcile. Empty string means first run — changed is always "true".
//
// No panics. Always returns a result struct.
func executeGitOperation(ctx context.Context, repo, branch, path, lastCommit string) GitOperationResult {
	// Ensure working directory exists
	if err := os.MkdirAll(path, 0o755); err != nil {
		return GitOperationResult{
			Operation: "clone",
			Error:     fmt.Sprintf("mkdir: %v", err),
			Path:      path,
		}
	}

	// Determine if repo already exists
	gitDir := filepath.Join(path, ".git")
	_, err := os.Stat(gitDir)

	operation := "fetch"
	if os.IsNotExist(err) {
		operation = "clone"
	}

	var cmd *exec.Cmd

	if operation == "clone" {
		cmd = exec.CommandContext(ctx, "git", "clone", "--branch", branch, repo, path)
	} else {
		cmd = exec.CommandContext(ctx, "git", "-C", path, "fetch", "origin", branch)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return GitOperationResult{
			Operation: operation,
			Error:     fmt.Sprintf("%s: %v (%s)", operation, err, out),
			Path:      path,
		}
	}

	// Fast-forward if fetch
	if operation == "fetch" {
		ff := exec.CommandContext(ctx, "git", "-C", path, "merge", "--ff-only", "origin/"+branch)
		if out, err := ff.CombinedOutput(); err != nil {
			return GitOperationResult{
				Operation: "fetch",
				Error:     fmt.Sprintf("fast-forward: %v (%s)", err, out),
				Path:      path,
			}
		}
	}

	// Read current commit
	rev := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "HEAD")
	commitBytes, err := rev.CombinedOutput()
	if err != nil {
		return GitOperationResult{
			Operation: operation,
			Error:     fmt.Sprintf("rev-parse: %v (%s)", err, commitBytes),
			Path:      path,
		}
	}

	commit := strings.TrimSpace(string(commitBytes))

	// Detect change using the annotation value from the previous reconcile.
	// Empty lastCommit means first run — always changed.
	changed := "true"
	if lastCommit != "" && strings.TrimSpace(lastCommit) == commit {
		changed = "false"
	}

	return GitOperationResult{
		Operation: operation,
		Commit:    commit,
		Changed:   changed,
		Path:      path,
		Error:     "",
	}
}

// patchLastCommitAnnotation writes the new commit hash to the CR annotation so
// the next reconcile can detect whether the repo has changed without reading a
// file (which is lost on pod restart).
func patchLastCommitAnnotation(
	ctx context.Context,
	kube kubeclient.Interface,
	obj domain.Object,
	commit string,
) error {
	return kube.PatchAnnotations(ctx, obj, map[string]string{
		annotationLastCommit: commit,
	}, metav1.PatchOptions{})
}
