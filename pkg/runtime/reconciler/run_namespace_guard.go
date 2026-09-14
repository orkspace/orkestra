// pkg/reconciler/run_namespace_guard.go
package reconciler

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// NamespaceGuardResult holds the outcome of a namespace restriction check.
type NamespaceGuardResult struct {
	Allowed   bool
	Namespace string
	Pattern   string // matched restricted or allowed pattern (for logs)
	Reason    string // "restricted", "not-allowed", or ""
}

// CheckNamespace determines whether a target namespace is permitted for
// child resource creation. It evaluates both RestrictedNamespaces and
// AllowedNamespaces with the following precedence:
//
//  1. RestrictedNamespaces — deny-list (always wins)
//  2. AllowedNamespaces    — allow-list (optional; empty = allow all)
//
// Called before onCreate, onReconcile, onDelete, and before registry dispatch.
func CheckNamespace(
	ctx context.Context,
	obj domain.Object,
	targetNamespace string,
	restricted orktypes.RestrictedNamespaces,
	allowed orktypes.AllowedNamespaces,
	crdName string,
) *NamespaceGuardResult {

	// 1. RestrictedNamespaces always win
	if restricted.IsRestricted(targetNamespace) {
		pattern := matchedRestrictedPattern(targetNamespace, restricted)

		logger.FromContext(ctx).Warn().
			Str("crd", crdName).
			Str("cr", fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())).
			Str("targetNamespace", targetNamespace).
			Str("pattern", pattern).
			Msg("namespace guard: child resource creation skipped — namespace is restricted")

		return &NamespaceGuardResult{
			Allowed:   false,
			Namespace: targetNamespace,
			Pattern:   pattern,
			Reason:    "restricted",
		}
	}

	// 2. AllowedNamespaces (optional allow-list)
	//    If empty → allow all (unless restricted)
	if len(allowed) > 0 && !allowed.IsAllowed(targetNamespace) {
		pattern := matchedAllowedPattern(targetNamespace, allowed)

		logger.FromContext(ctx).Warn().
			Str("crd", crdName).
			Str("cr", fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())).
			Str("targetNamespace", targetNamespace).
			Msg("namespace guard: child resource creation skipped — namespace not in allowed list")

		return &NamespaceGuardResult{
			Allowed:   false,
			Namespace: targetNamespace,
			Pattern:   pattern,
			Reason:    "not-allowed",
		}
	}

	// 3. Allowed
	return &NamespaceGuardResult{
		Allowed:   true,
		Namespace: targetNamespace,
	}
}

// EventMessage returns the Kubernetes event message for a blocked namespace.
func (r *NamespaceGuardResult) EventMessage(resourceKind, resourceName string) string {
	switch r.Reason {
	case "restricted":
		return fmt.Sprintf(
			"Skipped creating %s %q in namespace %q — namespace is restricted (matched pattern: %q)",
			resourceKind, resourceName, r.Namespace, r.Pattern,
		)
	case "not-allowed":
		return fmt.Sprintf(
			"Skipped creating %s %q in namespace %q — namespace is not in allowedNamespaces (closest match: %q)",
			resourceKind, resourceName, r.Namespace, r.Pattern,
		)
	default:
		return ""
	}
}

// matchedRestrictedPattern returns the first restricted pattern that matches.
func matchedRestrictedPattern(namespace string, restricted orktypes.RestrictedNamespaces) string {
	for _, pattern := range restricted {
		if orktypes.MatchesPattern(namespace, pattern) {
			return pattern
		}
	}
	return ""
}

// matchedAllowedPattern returns the first allowed pattern that matches.
// If none match, returns empty string.
func matchedAllowedPattern(namespace string, allowed orktypes.AllowedNamespaces) string {
	for _, pattern := range allowed {
		if orktypes.MatchesPattern(namespace, pattern) {
			return pattern
		}
	}
	return ""
}
