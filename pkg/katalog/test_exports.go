package katalog

import (
	"github.com/orkspace/orkestra/pkg/konfig"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// NewKatalogWithLifecycleForTest builds a minimal Katalog with the lifecycle
// block set, for use in validate package tests that cannot set the unexported field.
func NewKatalogWithLifecycleForTest(kind string, lc *orktypes.KatalogLifecycle) *Katalog {
	return &Katalog{Kind: kind, lifecycle: lc}
}

// NewKatalogWithPolicyForTest builds a minimal Katalog with the policy block set.
func NewKatalogWithPolicyForTest(kind string, p *orktypes.KatalogPolicy) *Katalog {
	return &Katalog{Kind: kind, policy: p}
}

// NewKatalogWithMetadataForTest builds a Katalog with a specific metadata block
// and an optional enabledCRDs map. Nil crds is treated as empty.
func NewKatalogWithMetadataForTest(m orktypes.KatalogMeta, crds map[string]orktypes.CRDEntry) *Katalog {
	if crds == nil {
		crds = map[string]orktypes.CRDEntry{}
	}
	return &Katalog{metadata: m, enabledCRDs: crds}
}

// wireForTest runs the same setGroupVersionKind → setDefaults sequence
// Validate() runs in production (see validate.go), so a Katalog built by
// NewKatalogForTest/NewFromEntryPointers is immediately usable by
// LookupByKind/LookupByName/GVR/etc. without every test remembering to call
// BuildLookupIndexes (or hitting Kind()/GVR() silently returning zero values
// because GroupVersionKind/GroupVersionResource were never computed).
// Panics on error — these are hand-authored test fixtures, not untrusted
// input, so a malformed one should fail loudly and immediately.
func (k *Katalog) wireForTest() *Katalog {
	// setGroupVersionKind is best-effort: graph-only fixtures (e.g. cycle
	// detection tests) legitimately omit apiTypes, so we skip rather than panic.
	_ = k.SetGroupVersionKind()
	if err := k.SetDefaults(konfig.NewDefaultKonfig()); err != nil {
		panic("katalog test fixture: " + err.Error())
	}
	// Pre-build the serve-enabled cache so ServeEnabledCRDs() returns the
	// correct slice without requiring a full BuildExpanded/ParseFile call.
	k.serveEnabledCRDs = k.BuildServeEnabledCRDs()
	return k
}

// NewKatalogForTest creates a Katalog with pre-set enabledCRDs for testing.
// Bypasses YAML parsing and Validate()'s other steps (uniqueness,
// dependsOn, reconciler mode, …) but still wires GVK/GVR/lookup indexes and
// defaults exactly as Validate() would, so lookups and field defaults
// behave the same as a fully loaded Katalog.
func NewKatalogForTest(crds map[string]orktypes.CRDEntry) *Katalog {
	if crds == nil {
		crds = map[string]orktypes.CRDEntry{}
	}
	for key, entry := range crds {
		if entry.Name == "" {
			entry.Name = key
			crds[key] = entry
		}
	}
	return (&Katalog{enabledCRDs: crds}).wireForTest()
}

// SetKatalogDirForTest sets the katalogDir field on a Katalog for use in tests
// that exercise publish-path validation which reads files relative to that directory.
func (k *Katalog) SetKatalogDirForTest(dir string) { k.katalogDir = dir }

// SetEnabledCRDsForTest replaces the enabledCRDs map on an existing Katalog.
// Allows tests that build up a Katalog in stages to set CRDs after construction.
func (k *Katalog) SetEnabledCRDsForTest(crds map[string]orktypes.CRDEntry) {
	if crds == nil {
		crds = map[string]orktypes.CRDEntry{}
	}
	k.enabledCRDs = crds
}

// NewFromEntryPointers creates a Katalog from a map of CRD entry pointers.
// Useful when you have pointers and want to avoid copying. See
// NewKatalogForTest for what gets wired.
func NewFromEntryPointers(entries map[string]*orktypes.CRDEntry) *Katalog {
	if entries == nil {
		entries = map[string]*orktypes.CRDEntry{}
	}
	kat := &Katalog{
		enabledCRDs: make(map[string]orktypes.CRDEntry, len(entries)),
	}
	for k, v := range entries {
		if v != nil {
			entry := *v
			if entry.Name == "" {
				entry.Name = k
			}
			kat.enabledCRDs[k] = entry
		}
	}
	return kat.wireForTest()
}
