// pkg/katalog/serialize.go
package katalog

import (
	"fmt"

	"github.com/orkspace/orkestra/pkg/konfig"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"gopkg.in/yaml.v3"
)

// SerializeExpanded serializes the post-KomposeRuntimeKatalog state to YAML.
//
// The output is a valid Katalog YAML with fully expanded CRDs (all motif
// imports already inlined, imports: field absent). This is what the bundle's
// ConfigMap embeds — the runtime can load it without any OCI pulls.
//
// Must be called after KomposeRuntimeKatalog; returns an error if no CRDs
// are present (indicates expansion hasn't been run yet).
func (k *Katalog) SerializeExpanded() ([]byte, error) {
	if k.EnabledCRDsEmpty() && !k.IsStandaloneGateway() {
		return nil, fmt.Errorf("Katalog has no enabled CRDs")
	}

	// Reconstruct a KatalogFile from the fully expanded state.
	// Always kind: Katalog — the bundle has no imports; Komposer sources become
	// plain Katalogs after expansion so the runtime always takes the loadKatalog path.
	kf := orktypes.KatalogFile{
		APIVersion:   k.APIVersion,
		Kind:         konfig.KatalogKind(),
		Metadata:     k.metadata,
		Spec:         orktypes.KatalogSpec{CRDs: k.enabledCRDs},
		Security:     k.Security,
		Gateway:      k.Gateway,
		Notification: k.Notification,
		Providers:    k.Providers,
		Profiles:     k.Profiles,
		Notes:        k.Notes,
	}

	out, err := yaml.Marshal(kf)
	if err != nil {
		return nil, fmt.Errorf("serializing expanded katalog: %w", err)
	}
	return out, nil
}
