// pkg/merger/parse.go
package merger

import (
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/konfig"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// parseKatalogDoc parses a single YAML document and returns a KatalogFile
// if it is a valid Katalog or Komposer. Returns (nil, nil) for any other
// document — not an error, just not a document we care about.
//
// Hard errors only when the document looks like a Katalog/Komposer
// (passes the fast string check) but fails to parse or has an unsupported
// apiVersion. Silently skipping malformed documents would hide user mistakes.
func parseKatalogDoc(doc []byte, source string) (*orktypes.KatalogFile, error) {
	// Fast path — skip documents that contain neither Katalog nor Komposer kind.
	// This avoids full YAML parsing for every non-relevant template in a Helm manifest.
	if !containsValidKind(doc) {
		return nil, nil
	}

	// Detect list-format CRDs before full parsing — gives a clear error instead of
	// a raw YAML unmarshal failure. spec.crds must be a map, not a sequence:
	//   spec.crds:             ← correct (map)
	//     myresource: {}
	//   spec.crds:             ← wrong (list)
	//     - name: myresource
	if looksLikeCRDList(doc) {
		return nil, fmt.Errorf(
			"%q: spec.crds must be a map (name: {}) not a list (- name:).\n"+
				"  See: https://orkestra.sh/reference/katalog#spec-crds",
			source,
		)
	}

	var katalog orktypes.KatalogFile
	if err := strictUnmarshal(doc, &katalog); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", source, err)
	}

	// Kind must be Katalog or Komposer — anything else is silently skipped.
	// This handles YAML files that happen to contain the word "Katalog" in a comment.
	if !konfig.IsValidPatternKind(katalog.Kind) {
		return nil, nil
	}

	// apiVersion must match a supported version — hard error with guidance.
	if !konfig.IsValidApiVersion(katalog.APIVersion) {
		return nil, fmt.Errorf(
			"%q: unsupported apiVersion %q\n"+
				"  Supported: %v\n"+
				"  This usually means the pattern was built for a different version of Orkestra.\n"+
				"  Check the upstream pattern's katalog.yaml or update Orkestra.",
			source, katalog.APIVersion, konfig.ApiVersions(),
		)
	}

	// Name is required irrespective of kind
	if katalog.Metadata.Name == "" {
		return nil, fmt.Errorf("%q: missing metadata.name", source)
	}

	return &katalog, nil
}

// looksLikeCRDList detects the common mistake of writing spec.crds as a YAML
// list instead of a map. Checks for "- name:" immediately after "crds:" with
// optional whitespace — good enough to catch the pattern without full parsing.
func looksLikeCRDList(doc []byte) bool {
	s := string(doc)
	idx := strings.Index(s, "crds:")
	if idx < 0 {
		return false
	}
	// Look at the content after "crds:" — if the next non-blank line starts
	// with "- " it is a list entry.
	after := strings.TrimLeft(s[idx+5:], " \t\r\n")
	return strings.HasPrefix(after, "- ")
}

// containsValidKind is a fast string check before committing to a full YAML parse.
// Returns true if the document contains either "kind: Katalog" or "kind: Komposer".
func containsValidKind(doc []byte) bool {
	s := string(doc)
	return strings.Contains(s, fmt.Sprintf("kind: %s", konfig.KatalogKind())) ||
		strings.Contains(s, fmt.Sprintf("kind: %s", konfig.KomposerKind()))
}

// sniffDocumentKind does a fast string scan for known Orkestra kinds that are
// NOT valid merger inputs (Motif, E2E). Used to produce a clear error instead
// of silently treating the file as an empty Katalog.
// Returns the detected kind string, or "" if none recognized.
func sniffDocumentKind(doc []byte) string {
	s := string(doc)
	for _, kind := range []string{konfig.MotifKind(), konfig.E2EKind()} {
		if strings.Contains(s, fmt.Sprintf("kind: %s", kind)) {
			return kind
		}
	}
	return ""
}

// Export
var ParseKatalogDoc = parseKatalogDoc
