package katalog

import (
	"sort"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// CRDLookupResult carries the resolved CRD entry and the alias name that was
// used to find it. Alias is empty when a primary target or a non-alias lookup matched.
type CRDLookupResult struct {
	CRD   *orktypes.CRDEntry
	Alias string
}

// Entry returns the resolved CRD entry. Nil-safe — returns nil when the
// lookup result itself is nil, so callers can write:
//
//	crd := kat.LookupByKind("App").Entry()
//	if crd == nil { ... }
func (r *CRDLookupResult) Entry() *orktypes.CRDEntry {
	if r == nil || r.CRD == nil {
		return nil
	}
	return r.CRD
}

// LookupByTargetOrAlias finds a serve-enabled CRD by primary target or alias name.
// Returns nil when no match is found or when the matching entry is disabled.
//
// Primary targets are checked before aliases. When the same name is both a
// primary target on one CRD and an alias on another, the primary target wins —
// ork validate catches this collision at load time.
func (k *Katalog) LookupByTargetOrAlias(target string) *CRDLookupResult {
	if k == nil || target == "" {
		return nil
	}
	// 1. Primary targets first — only when the primary surface is enabled.
	for i := range k.enabledCRDs {
		crd := k.enabledCRDs[i]
		if crd.HasServeTarget() && crd.ServeTarget() == target {
			return &CRDLookupResult{CRD: &crd, Alias: ""}
		}
	}
	// 2. Alias entries (non-primary enabled entries in Target.Entries).
	for i := range k.enabledCRDs {
		crd := k.enabledCRDs[i]
		if !crd.ServeEnabled() || crd.Serve == nil {
			continue
		}
		cfg, ok := crd.Serve.Target.Entries[target]
		if !ok || cfg == nil || cfg.Primary || !cfg.IsEnabled() {
			continue
		}
		return &CRDLookupResult{CRD: &crd, Alias: target}
	}
	return nil
}

// LookupByKindOrAlias finds a CRD by Kubernetes Kind first, then falls back to
// LookupByTargetOrAlias (primary target, then alias). Use this when the caller
// may send any form of identifier — Kubernetes Kind, serve target name, or alias.
func (k *Katalog) LookupByKindOrAlias(identifier string) *CRDLookupResult {
	if r := k.LookupByKind(identifier); r != nil {
		return r
	}
	return k.LookupByTargetOrAlias(identifier)
}

// AvailableTargets returns the primary targets and alias names of all
// serve-enabled CRDs, sorted alphabetically.
// Used in error messages ("available: app, internal, public, smartapp, v2").
func (k *Katalog) AvailableTargets() []string {
	if k == nil {
		return nil
	}
	seen := make(map[string]struct{}, k.Len()*2)
	for _, crd := range k.enabledCRDs {
		if crd.HasServeTarget() {
			seen[crd.ServeTarget()] = struct{}{}
		}
		for name, cfg := range crd.AllServeTargets() {
			if cfg != nil && !cfg.Primary && cfg.IsEnabled() {
				seen[name] = struct{}{}
			}
		}
	}
	targets := make([]string, 0, len(seen))
	for t := range seen {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	return targets
}

// ServeCatalog returns all serve-enabled CRD entries, sorted by target.
// Used by the catalog endpoint to list available services.
func (k *Katalog) ServeCatalog() []*orktypes.CRDEntry {
	if k == nil {
		return nil
	}
	catalog := make([]*orktypes.CRDEntry, 0, k.Len())
	for _, crd := range k.enabledCRDs {
		if crd.HasServeTarget() {
			catalog = append(catalog, &crd)
		}
	}
	sort.Slice(catalog, func(i, j int) bool {
		return catalog[i].ServeTarget() < catalog[j].ServeTarget()
	})
	return catalog
}

// ServeEnabledCRDs returns all serve-enabled CRD entries as a slice.
// Uses cached serveEnabledCRDs if available, otherwise builds it.
func (k *Katalog) ServeEnabledCRDs() []*orktypes.CRDEntry {
	if k == nil {
		return nil
	}
	if k.serveEnabledCRDs == nil {
		return k.BuildServeEnabledCRDs()
	}
	return k.serveEnabledCRDs
}
