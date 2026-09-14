package katalog

import (
	"sort"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// -----------------------------------------------------------------------------
// Index Building
// -----------------------------------------------------------------------------

// BuildLookupIndexes builds O(1) lookup maps indexes after CRDs are loaded.
// All indexes are stored in lowercase for case-insensitive lookups.
// After this runs, the Katalog supports:
//   - LookupByKind
//   - LookupByTarget
//   - LookupByGVKString
//   - LookupByGVRString
//
// Called last in setGroupVersionKind.
func (k *Katalog) BuildLookupIndexes() {
	k.apiVersionIndex = make(map[string]string)
	k.kindIndex = make(map[string]string)
	k.nameIndex = make(map[string]string)
	k.pluralIndex = make(map[string]string)
	k.gvkIndex = make(map[string]string)
	k.gvrIndex = make(map[string]string)
	k.targetIndex = make(map[string]string)
	k.webhookNameIndex = make(map[string]string)

	if k.Gateway != nil && k.Gateway.Webhooks != nil {
		w := k.Gateway.Webhooks
		for _, e := range w.GitHub {
			k.webhookNameIndex[strings.ToLower(e.Name)] = "github"
		}
		for _, e := range w.GitLab {
			k.webhookNameIndex[strings.ToLower(e.Name)] = "gitlab"
		}
		for _, e := range w.Slack {
			k.webhookNameIndex[strings.ToLower(e.Name)] = "slack"
		}
		for _, e := range w.Generic {
			k.webhookNameIndex[strings.ToLower(e.Name)] = "generic"
		}
	}

	for name, crd := range k.enabledCRDs {
		// Store all keys in lowercase for case-insensitive lookups.
		// apiVersionIndex is keyed by apiVersion+kind concatenated, matching
		// LookupByAPIVersionAndKind's query — apiVersion alone isn't unique
		// when multiple Kinds share a group/version.
		k.apiVersionIndex[strings.ToLower(crd.APIVersion()+crd.Kind())] = name
		k.kindIndex[strings.ToLower(crd.Kind())] = name
		k.nameIndex[strings.ToLower(name)] = name
		if crd.APITypes.Plural != "" {
			k.pluralIndex[strings.ToLower(crd.APITypes.Plural)] = name
		}
		k.gvkIndex[strings.ToLower(crd.GVKString())] = name
		k.gvrIndex[strings.ToLower(crd.GVRString())] = name
		if crd.HasServeTarget() {
			k.targetIndex[strings.ToLower(crd.ServeTarget())] = name
		}
	}
}

// BuildServeEnabledCRDs returns a slice of all serve-enabled CRDs.
// Use for iteration when the map key is not needed.
func (k *Katalog) BuildServeEnabledCRDs() []*orktypes.CRDEntry {
	if k == nil {
		return nil
	}
	serveEnabled := make([]*orktypes.CRDEntry, 0, k.Len())
	for _, crd := range k.enabledCRDs {
		if crd.ServeEnabled() {
			entry := crd
			serveEnabled = append(serveEnabled, &entry)
		}
	}
	return serveEnabled
}

// ServeEnabledCRDsForCluster returns the serve-enabled CRDs that should be
// present on the named remote cluster. A CRD is included when:
//   - serve.clusters contains clusterName (static match), OR
//   - serve.clusters contains a template expression (resolved at apply time —
//     the CRD could land on any cluster, so include conservatively), OR
//   - any target.clusters entry matches by the same rules above.
//
// CRDs with no serve.clusters (local-only) are never included.
func (k *Katalog) ServeEnabledCRDsForCluster(clusterName string) []*orktypes.CRDEntry {
	if k == nil {
		return nil
	}
	var out []*orktypes.CRDEntry
	for _, crd := range k.enabledCRDs {
		if !crd.ServeEnabled() || crd.Serve == nil {
			continue
		}
		if crdRoutesToCluster(crd.Serve, clusterName) {
			entry := crd
			out = append(out, &entry)
		}
	}
	return out
}

// crdRoutesToCluster reports whether a serve config routes to the named cluster.
func crdRoutesToCluster(s *orktypes.ServeConfig, clusterName string) bool {
	for _, c := range s.Clusters {
		if orktypes.IsTemplate(c) || c == clusterName {
			return true
		}
	}
	for _, target := range s.Target.Entries {
		for _, c := range target.TargetClusters() {
			if orktypes.IsTemplate(c) || c == clusterName {
				return true
			}
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// Lookup Methods (O(1) index-based)
// -----------------------------------------------------------------------------

// LookupByKind finds the CRD entry whose Kind matches the given kind string.
// O(1) lookup using the kind index. Case-insensitive. Nil-safe.
func (k *Katalog) LookupByKind(kind string) *CRDLookupResult {
	if k == nil {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(kind))
	if name, ok := k.kindIndex[key]; ok {
		if entry, ok := k.enabledCRDs[name]; ok {
			return &CRDLookupResult{CRD: &entry}
		}
	}
	return nil
}

// LookupByName finds the CRD entry whose name (the Katalog map key) matches
// the given name. O(1) — enabledCRDs is already keyed by name. Case-insensitive. Nil-safe.
func (k *Katalog) LookupByName(name string) *CRDLookupResult {
	if k == nil {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if original, ok := k.nameIndex[key]; ok {
		if entry, ok := k.enabledCRDs[original]; ok {
			return &CRDLookupResult{CRD: &entry}
		}
	}
	return nil
}

// LookupByLabel finds the CRD entry whose name (the Katalog map key) matches
// the given label. Case-sensitive. Nil-safe.
func (k *Katalog) LookupByLabel(labels map[string]string) *CRDLookupResult {
	if k == nil {
		return nil
	}
	for _, entry := range k.enabledCRDs {
		if entry.Labels.Contains(labels) {
			return &CRDLookupResult{CRD: &entry}
		}
	}

	return nil
}

// LookupByPlural finds the CRD entry whose plural resource name matches the given string.
// O(1) lookup using the plural index. Case-insensitive. Nil-safe.
// Useful for routing requests matched by URL path (e.g. /apis/group/version/pluralresources).
func (k *Katalog) LookupByPlural(plural string) *CRDLookupResult {
	if k == nil {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(plural))
	if original, ok := k.pluralIndex[key]; ok {
		if entry, ok := k.enabledCRDs[original]; ok {
			return &CRDLookupResult{CRD: &entry}
		}
	}
	return nil
}

// LookupByAPIVersionAndKind finds the CRD whose APIVersion and Kind matches the given strings.
// O(1) lookup using the apiVersionIndex.
func (k *Katalog) LookupByAPIVersionAndKind(apiVersion, kind string) *CRDLookupResult {
	if k == nil {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(apiVersion) + strings.TrimSpace(kind))
	if name, ok := k.apiVersionIndex[key]; ok {
		if entry, ok := k.enabledCRDs[name]; ok {
			return &CRDLookupResult{CRD: &entry}
		}
	}
	return nil
}

// LookupByGVKString finds the CRD entry whose GroupVersionKind matches the given GVK string.
// O(1) lookup using the GVK index. Case-insensitive.
func (k *Katalog) LookupByGVKString(gvkString string) *CRDLookupResult {
	if k == nil {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(gvkString))
	if name, ok := k.gvkIndex[key]; ok {
		if entry, ok := k.enabledCRDs[name]; ok {
			return &CRDLookupResult{CRD: &entry}
		}
	}
	return nil
}

// LookupByGVRString finds the CRD entry whose GroupVersionResource matches the given GVR string.
// O(1) lookup using the GVR index. Case-insensitive.
func (k *Katalog) LookupByGVRString(gvrString string) *CRDLookupResult {
	if k == nil {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(gvrString))
	if name, ok := k.gvrIndex[key]; ok {
		if entry, ok := k.enabledCRDs[name]; ok {
			return &CRDLookupResult{CRD: &entry}
		}
	}
	return nil
}

// LookupByTarget finds the CRD entry whose resolved target matches t.
// O(1) lookup using the target index. Case-insensitive.
func (k *Katalog) LookupByTarget(target string) *CRDLookupResult {
	if k == nil {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(target))
	if name, ok := k.targetIndex[key]; ok {
		if entry, ok := k.enabledCRDs[name]; ok {
			return &CRDLookupResult{CRD: &entry}
		}
	}
	return nil
}

// LookupWebhookSource finds which gateway.webhooks source ("github",
// "gitlab", "slack", "generic") declares an entry with the given name.
// O(1) lookup using the webhook name index. Case-insensitive.
func (k *Katalog) LookupWebhookSource(name string) (string, bool) {
	if k == nil {
		return "", false
	}
	key := strings.ToLower(strings.TrimSpace(name))
	source, ok := k.webhookNameIndex[key]
	return source, ok
}

// LookupByTargetOrKind attempts to find a CRD by target first, then by kind.
// Useful for handlers that accept either a target or a kind. Case-insensitive.
func (k *Katalog) LookupByTargetOrKind(identifier string) *CRDLookupResult {
	identifier = strings.TrimSpace(identifier)
	if r := k.LookupByTarget(identifier); r != nil {
		return r
	}
	return k.LookupByKind(identifier)
}

// MustLookupByTarget finds the CRD entry whose resolved target matches t.
// Panics if not found. Use when you know the target exists. Case-insensitive.
func (k *Katalog) MustLookupByTarget(target string) *CRDLookupResult {
	r := k.LookupByTarget(target)
	if r == nil {
		panic("CRD not found for target: " + target)
	}
	return r
}

// MustLookupByKind finds the CRD entry whose Kind matches the given kind string.
// Panics if not found. Use when you know the kind exists. Case-insensitive.
func (k *Katalog) MustLookupByKind(kind string) *CRDLookupResult {
	r := k.LookupByKind(kind)
	if r == nil {
		panic("CRD not found for kind: " + kind)
	}
	return r
}

// -----------------------------------------------------------------------------
// Existence Checks
// -----------------------------------------------------------------------------

// IsKindRegistered returns true if the kind exists in the index.
// Case-insensitive.
func (k *Katalog) IsKindRegistered(kind string) bool {
	if k == nil {
		return false
	}
	_, ok := k.kindIndex[strings.ToLower(strings.TrimSpace(kind))]
	return ok
}

// IsTargetRegistered returns true if the target exists in the index.
// Case-insensitive.
func (k *Katalog) IsTargetRegistered(target string) bool {
	if k == nil {
		return false
	}
	_, ok := k.targetIndex[strings.ToLower(strings.TrimSpace(target))]
	return ok
}

// IsGVKRegistered returns true if the GVK exists in the index.
// Case-insensitive.
func (k *Katalog) IsGVKRegistered(gvkString string) bool {
	if k == nil {
		return false
	}
	_, ok := k.gvkIndex[strings.ToLower(strings.TrimSpace(gvkString))]
	return ok
}

// IsGVRRegistered returns true if the GVR exists in the index.
// Case-insensitive.
func (k *Katalog) IsGVRRegistered(gvrString string) bool {
	if k == nil {
		return false
	}
	_, ok := k.gvrIndex[strings.ToLower(strings.TrimSpace(gvrString))]
	return ok
}

// -----------------------------------------------------------------------------
// List Methods
// -----------------------------------------------------------------------------

// ListTargets returns all registered targets (lowercase), sorted.
func (k *Katalog) ListTargets() []string {
	if k == nil {
		return nil
	}
	targets := make([]string, 0, len(k.targetIndex))
	for target := range k.targetIndex {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

// ListKinds returns all registered kinds (lowercase), sorted.
func (k *Katalog) ListKinds() []string {
	if k == nil {
		return nil
	}
	kinds := make([]string, 0, len(k.kindIndex))
	for kind := range k.kindIndex {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// ListGVKs returns all registered GVK strings (lowercase), sorted.
func (k *Katalog) ListGVKs() []string {
	if k == nil {
		return nil
	}
	gvks := make([]string, 0, len(k.gvkIndex))
	for gvk := range k.gvkIndex {
		gvks = append(gvks, gvk)
	}
	sort.Strings(gvks)
	return gvks
}

// ListGVRs returns all registered GVR strings (lowercase), sorted.
func (k *Katalog) ListGVRs() []string {
	if k == nil {
		return nil
	}
	gvrs := make([]string, 0, len(k.gvrIndex))
	for gvr := range k.gvrIndex {
		gvrs = append(gvrs, gvr)
	}
	sort.Strings(gvrs)
	return gvrs
}

// domain.Katalog Getters

// GetNameByGVKString delegates to LookupByGVKString.
// Used by domain.Katalog for callers who cannot import katalog
func (k *Katalog) GetNameByGVKString(gvkString string) string {
	return k.LookupByGVKString(gvkString).Entry().Name
}

// GetNameByGVRString delegates to LookupByGVRString.
// Used by domain.Katalog for callers who cannot import katalog
func (k *Katalog) GetNameByGVRString(gvrString string) string {
	return k.LookupByGVRString(gvrString).Entry().Name
}

// GetNameByKind delegates to LookupByKind.
// Used by domain.Katalog for callers who cannot import katalog
func (k *Katalog) GetNameByKind(kind string) string {
	return k.LookupByKind(kind).Entry().Name
}

// GetNameByTarget delegates to LookupByGVRString.
// Used by domain.Katalog for callers who cannot import katalog
func (k *Katalog) GetNameByTarget(target string) string {
	return k.LookupByTargetOrAlias(target).Entry().Name
}
