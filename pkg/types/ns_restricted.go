// pkg/types/ns_restricted.go
package types

import "strings"

// RestrictedNamespaces declares namespaces where Orkestra will not create
// child resources, regardless of what a CR spec requests.
//
// Applies before onCreate/onReconcile templates and before Go hooks run.
// The restriction is on child resources only — not on the CR itself.
//
// Supported at all three levels:
//   Komposer level — applies to all CRDs in the Komposer
//   Katalog level  — applies to all CRDs in the Katalog
//   CRD level      — applies to this specific CRD
//
// Rules from all three levels are merged — more specific levels add to,
// not replace, less specific levels. A namespace restricted at the Komposer
// level cannot be un-restricted at the CRD level. This is intentional:
// platform-wide restrictions are non-negotiable.
//
// Example:
//   restrictedNamespaces:
//     - kube-system
//     - cert-manager
//     - monitoring
//     - kube-*          # wildcard — all namespaces starting with kube-
//     - "*-system"      # wildcard — all namespaces ending in -system

// RestrictedNamespaces holds the set of restricted namespace patterns.
type RestrictedNamespaces []string

// IsRestricted reports whether a given namespace matches any restriction.
// Supports exact matches and simple glob patterns (* prefix or suffix).
func (r RestrictedNamespaces) IsRestricted(namespace string) bool {
	for _, pattern := range r {
		if MatchesPattern(namespace, pattern) {
			return true
		}
	}
	return false
}

// Merge combines two RestrictedNamespaces sets, deduplicating entries.
// Used when merging Komposer-level and CRD-level restrictions.
// Since restrictions are additive, a namespace restricted at any level
// is restricted at all levels — more specific levels cannot remove restrictions.
func (r RestrictedNamespaces) Merge(other RestrictedNamespaces) RestrictedNamespaces {
	seen := make(map[string]bool, len(r))
	merged := make(RestrictedNamespaces, 0, len(r)+len(other))

	for _, ns := range r {
		if !seen[ns] {
			seen[ns] = true
			merged = append(merged, ns)
		}
	}
	for _, ns := range other {
		if !seen[ns] {
			seen[ns] = true
			merged = append(merged, ns)
		}
	}

	return merged
}

// MatchesPattern reports whether a namespace matches a restriction pattern.
// Supports:
//
//	exact:   "kube-system"  → matches only "kube-system"
//	prefix:  "kube-*"       → matches any namespace starting with "kube-"
//	suffix:  "*-system"     → matches any namespace ending in "-system"
func MatchesPattern(namespace, pattern string) bool {
	if pattern == namespace {
		return true
	}

	// Prefix wildcard: "kube-*"
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(namespace, prefix)
	}

	// Suffix wildcard: "*-system"
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(namespace, suffix)
	}

	return false
}
