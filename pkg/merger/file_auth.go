// pkg/merger/file_auth.go
package merger

import (
	"fmt"

	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
)

// loadImportFileWithAuth loads a Katalog import file with optional authentication.
// Imports must be Katalogs — a Komposer cannot import another Komposer.
func (m *Merger) loadImportFileWithAuth(komposerPath, importPath string, auth *utils.FileAuth) (map[string]orktypes.CRDEntry, error) {
	data, err := loadFileWithAuth(importPath, auth)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", importPath, err)
	}

	doc, err := parseKatalogDoc(data, importPath)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		logger.Debug().
			Str("path", importPath).
			Msg("merger: skipping import — not a valid Katalog document")
		return nil, nil
	}

	if doc.Kind == konfig.KomposerKind() {
		return nil, fmt.Errorf(
			"%q imports.files[%q]: a Komposer cannot import another Komposer — "+
				"only Katalog files are valid imports",
			komposerPath, importPath,
		)
	}

	if doc.Kind != konfig.KatalogKind() {
		return nil, fmt.Errorf(
			"%q imports.files[%q]: expected kind %q, got %q",
			komposerPath, importPath, konfig.KatalogKind(), doc.Kind,
		)
	}

	return m.loadKatalog(importPath, doc)
}
