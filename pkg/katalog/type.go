package katalog

import (
	"os"
	"path/filepath"
	"reflect"

	"github.com/orkspace/orkestra/pkg/konfig"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/runtime"
)

// -----------------------------------------------------------------------------
// Variables
// -----------------------------------------------------------------------------
var (
	// For updating the CRD instance - needed for lookups
	resourceTypeMap = map[reflect.Type]string{}
)

// -----------------------------------------------------------------------------
// Structs
// -----------------------------------------------------------------------------
type Katalog struct {
	APIVersion   string                                `yaml:"apiVersion"`
	Kind         string                                `yaml:"kind"`
	Spec         orktypes.KatalogSpec                  `yaml:"spec"`
	Security     orktypes.KatalogSecurity              `yaml:"security"`
	Notes        orktypes.NoteRegistry                 `yaml:"notes,omitempty"`
	Profiles     orktypes.ProfileRegistry              `yaml:"profiles,omitempty"`
	Gateway      *orktypes.GatewayConfig               `yaml:"gateway,omitempty"`
	Publish      *orktypes.PublishConfig               `yaml:"publish,omitempty"`
	Notification *orktypes.KatalogNotification         `yaml:"notification,omitempty"`
	Providers    []orktypes.KatalogProviderRequirement `yaml:"providers,omitempty"`
	projectInfo  interface{}                           `yaml:"projectInfo,omitempty"`

	KomposerMetadata orktypes.KatalogMeta `yaml:"metadata"`

	// Internal — enabledCRDs is enriched and validated; Spec.CRDs holds all (including disabled)
	metadata           orktypes.KatalogMeta         `yaml:"-" json:"-"`
	lifecycle          *orktypes.KatalogLifecycle   `yaml:"-" json:"-"`
	policy             *orktypes.KatalogPolicy      `yaml:"-" json:"-"`
	enabledCRDs        map[string]orktypes.CRDEntry `yaml:"-" json:"-"`
	serveEnabledCRDs   []*orktypes.CRDEntry         `yaml:"-" json:"-"`
	withCRDFiles       []string                     `yaml:"-" json:"-"` // CRD names that declared crdFile, captured before the field is cleared
	katalogDir         string                       `yaml:"-" json:"-"`
	conversionRegistry *InMemoryConversionRegistry
	admissionRegistry  *InMemoryAdmissionRegistry

	// konfig for managing katalog-related user inputs from env
	konfig *konfig.Konfig

	// Warnings collects non‑fatal validation messages for this CRD.
	Warnings orktypes.Warnings `json:"-"` // not serialized

	// Indexes for O(1) lookups
	kindIndex       map[string]string `yaml:"-" json:"-"` // kind -> crd name
	nameIndex       map[string]string `yaml:"-" json:"-"` // lowercase(map key) -> crd name
	pluralIndex     map[string]string `yaml:"-" json:"-"` // plural resource name -> crd name
	apiVersionIndex map[string]string `yaml:"-" json:"-"` // apiVersion -> crd name
	gvkIndex        map[string]string `yaml:"-" json:"-"` // gvk.String() -> crd name
	gvrIndex        map[string]string `yaml:"-" json:"-"` // gvr.String() -> crd name
	targetIndex     map[string]string `yaml:"-" json:"-"` // target -> crd name

	webhookNameIndex map[string]string `yaml:"-" json:"-"` // lowercase(webhook entry name) -> source ("github"/"gitlab"/"slack"/"generic")
}

// GatewayClusters returns the gateway.clusters entries map, or nil when none are declared.
func (k *Katalog) GatewayClusters() map[string]orktypes.GatewayClusterConfig {
	if k == nil || k.Gateway == nil || k.Gateway.Clusters == nil {
		return nil
	}
	return k.Gateway.Clusters.Entries
}

// GatewayClusterCount returns the number of gateway clusters defined.
func (k *Katalog) GatewayClusterCount() int {
	return len(k.GatewayClusters())
}

// GatewayClustersEmpty returns true if no gateway clusters are defined.
func (k *Katalog) GatewayClustersEmpty() bool {
	return k.GatewayClusterCount() == 0
}

// EnabledCRDs returns a map of enabled CRDs keyed by their name.
func (k *Katalog) EnabledCRDs() map[string]orktypes.CRDEntry {
	return k.enabledCRDs
}

// ListEnabledCRDs returns a slice of all enabled CRD entries.
// Use this for iteration when the map key is not needed.
func (k *Katalog) ListEnabledCRDs() []orktypes.CRDEntry {
	entries := make([]orktypes.CRDEntry, 0, k.Len())
	for _, crd := range k.enabledCRDs {
		entries = append(entries, crd)
	}
	return entries
}

// ListEnabledCRDPointers returns a slice of pointers to all enabled CRD entries.
// Use this when you need to modify CRD entries or avoid copying.
func (k *Katalog) ListEnabledCRDPointers() []*orktypes.CRDEntry {
	entries := make([]*orktypes.CRDEntry, 0, k.Len())
	for _, crd := range k.enabledCRDs {
		crdCopy := crd
		entries = append(entries, &crdCopy)
	}
	return entries
}

// EnabledCRDsList returns a slice of all enabled CRD entries.
func (k *Katalog) EnabledCRDsList() []orktypes.CRDEntry {
	entries := make([]orktypes.CRDEntry, 0, k.Len())
	for _, crd := range k.enabledCRDs {
		entries = append(entries, crd)
	}
	return entries
}

// EnabledCRDMap returns the raw map of enabled CRDs.
func (k *Katalog) EnabledCRDMap() map[string]orktypes.CRDEntry {
	return k.enabledCRDs
}

// AllCRDs returns all CRDs including disabled ones (from Spec).
func (k *Katalog) AllCRDs() map[string]orktypes.CRDEntry {
	return k.Spec.CRDs
}

// UserNotes returns all user defined notes in the katalog
func (k *Katalog) UserNotes() orktypes.NoteRegistry {
	if k == nil {
		return orktypes.NoteRegistry{}
	}
	return k.Notes
}

// UserProfiles returns all user defined profiles in the katalog
func (k *Katalog) UserProfiles() orktypes.ProfileRegistry {
	if k == nil {
		return orktypes.ProfileRegistry{}
	}
	return k.Profiles
}

// Empty reports true when the katalog is nil.
func (k *Katalog) Empty() bool {
	return k == nil
}

// EnabledCRDsEmpty reports true when enabled crds is 0.
func (k *Katalog) EnabledCRDsEmpty() bool {
	return len(k.enabledCRDs) == 0
}

// Len returns the number of enabled CRDs in this katalog
func (k *Katalog) Len() int {
	return len(k.enabledCRDs)
}

// SetKonfig sets the konfig on the Katalog. Called by pipeline.NewKatalog
// before KomposeRuntimeKatalog so that konfig-dependent methods work correctly.
func (k *Katalog) SetKonfig(kfg *konfig.Konfig) {
	k.konfig = kfg
}

// WithCRDFiles returns the names of CRDs that declared a crdFile.
// Populated during KomposeRuntimeKatalog before the field is cleared,
// so callers can inspect which CRDs used the local-file shortcut even
// after apiTypes have been resolved and CRDFile wiped.
func (k *Katalog) WithCRDFiles() []string {
	return k.withCRDFiles
}

// HasIntentFiles reports whether intent.yaml or intent.json are present in the
// katalog directory. Used at validate time to enforce publish.tests.intent: true.
func (k *Katalog) HasIntentFiles() bool {
	if k.katalogDir == "" {
		return false
	}
	for _, name := range []string{"intent.yaml", "intent.json"} {
		if _, err := os.Stat(filepath.Join(k.katalogDir, name)); err == nil {
			return true
		}
	}
	return false
}

// Metadata returns the Katalog metadata.
func (k *Katalog) Metadata() orktypes.KatalogMeta {
	return k.metadata
}

// Lifecycle returns the lifecycle policy block, or nil if absent.
func (k *Katalog) Lifecycle() *orktypes.KatalogLifecycle {
	return k.lifecycle
}

// Policy returns the platform policy block, or nil if absent.
func (k *Katalog) Policy() *orktypes.KatalogPolicy {
	return k.policy
}

// Deprecation returns the raw deprecation block, or nil if absent.
func (k *Katalog) Deprecation() *orktypes.KatalogDeprecation {
	if k.lifecycle == nil {
		return nil
	}
	return k.lifecycle.Deprecation
}

// IsDeprecated returns true if the Katalog is deprecated.
func (k *Katalog) IsDeprecated() bool {
	if k.lifecycle == nil {
		return false
	}
	return k.lifecycle.IsDeprecated()
}

// IsMigrated returns true if the Katalog is deprecated and has a migration target.
func (k *Katalog) IsMigrated() bool {
	if k.lifecycle == nil || k.lifecycle.Deprecation == nil {
		return false
	}
	return k.lifecycle.Deprecation.MigrationTarget() != ""
}

// MigrationTarget returns the value of the MigratedTo field.
// If the lifecycle or deprecation block is nil, it returns an empty string.
func (k *Katalog) MigrationTarget() string {
	if k.lifecycle == nil || k.lifecycle.Deprecation == nil {
		return ""
	}
	return k.lifecycle.Deprecation.MigrationTarget()
}

// MigrationMessage returns the deprecation message.
func (k *Katalog) MigrationMessage() string {
	if k.lifecycle == nil || k.lifecycle.Deprecation == nil {
		return ""
	}
	return k.lifecycle.Deprecation.MigrationMessage()
}

// CRDEntry returns the enabled CRD entry for the given name.
func (k *Katalog) CRDEntry(name string) (orktypes.CRDEntry, bool) {
	entry, ok := k.enabledCRDs[name]
	return entry, ok
}

// Scheme builds and returns a runtime.Scheme with all Katalog types registered.
func (k *Katalog) Scheme() (*runtime.Scheme, error) {
	if k == nil {
		return nil, nil
	}
	return NewSchemeRegistry(k)
}

// Empty katalog for testing
func NewEmptyKatalog() *Katalog {
	return &Katalog{}
}
