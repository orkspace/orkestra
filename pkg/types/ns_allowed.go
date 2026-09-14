// pkg/types/ns_allowed.go
package types

// AllowedNamespaces declares namespaces where Orkestra *is permitted*
// to create child resources. If the list is empty, all namespaces are allowed.
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
// not replace, less specific levels. A namespace allowed at the Komposer
// level cannot be "disallowed" at the CRD level. This is intentional:
// platform-wide allowances are non-negotiable.
//
// Example:
//   allowedNamespaces:
//     - apps
//     - workloads
//     - team-*        # wildcard — any namespace starting with team-
//     - "*-sandbox"   # wildcard — any namespace ending with -sandbox

// AllowedNamespaces holds the set of allowed namespace patterns.
type AllowedNamespaces []string

// IsAllowed reports whether a given namespace matches any allowed pattern.
// If the list is empty, all namespaces are allowed.
// Supports exact matches and simple glob patterns (* prefix or suffix).
func (a AllowedNamespaces) IsAllowed(namespace string) bool {
	// Empty list → allow everything
	if len(a) == 0 {
		return true
	}

	for _, pattern := range a {
		if MatchesPattern(namespace, pattern) {
			return true
		}
	}
	return false
}

// Merge combines two AllowedNamespaces sets, deduplicating entries.
// Since allowances are additive, a namespace allowed at any level
// is allowed at all levels — more specific levels cannot remove allowances.
func (a AllowedNamespaces) Merge(other AllowedNamespaces) AllowedNamespaces {
	seen := make(map[string]bool, len(a))
	merged := make(AllowedNamespaces, 0, len(a)+len(other))

	for _, ns := range a {
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

// IsSingleNamespace reports whether this CRD's namespace rules pin it to
// exactly one namespace — the same single-namespace fast path the runtime
// informer uses to scope its ListerWatcher (pkg/runtime/informer.NamespaceFilter
// mirrors this exact check; this is the canonical definition both that type
// and any validator should call instead of re-deriving it from len(...)==1).
func (c *CRDEntry) IsSingleNamespace() bool {
	return len(c.AllowedNamespaces) == 1 && len(c.RestrictedNamespaces) == 0
}

// SingleNamespace returns the one namespace IsSingleNamespace confirmed, or
// "" when there isn't exactly one.
func (c *CRDEntry) SingleNamespace() string {
	if !c.IsSingleNamespace() {
		return ""
	}
	return c.AllowedNamespaces[0]
}

// PinnedToNamespace reports whether this CRD's informer is scoped to watch
// only one fixed namespace — either via IsSingleNamespace (AllowedNamespaces
// with exactly one entry) or the legacy per-CRD Namespace field
// (cmd/internal/runtime_konstructor.go's dynamic-CRD fallback). A CRD whose
// serve.namespace resolves differently per submission (e.g. by team) can never
// be watched this way — whatever it creates outside the pinned namespace
// would sit unreconciled forever. See Katalog.validateServeNamespace.
func (c *CRDEntry) PinnedToNamespace() bool {
	return c.IsSingleNamespace() || c.Namespace != ""
}
