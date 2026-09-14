package registry

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/orkspace/orkestra/pkg/utils"
	"gopkg.in/yaml.v3"
)

// Helpers
var readLocal = utils.ReadLocal

// ToString returns the underlying string value.
func (k PatternKind) ToString() string {
	return string(k)
}

// String implements fmt.Stringer so PatternKind prints nicely with fmt.
func (k PatternKind) String() string {
	return k.ToString()
}

// ExtractTagVersion extracts the tag portion from a reference like:
//
//	"name:version" -> "version"
//	"ghcr.io/org/repo/name:version" -> "version"
//	"oci://ghcr.io/org/repo/name:version" -> "version"
//
// If no tag is present (e.g., "name" or "ghcr.io/org/repo/name@sha256:..."), returns "".
func ExtractTagVersion(ref string) string {
	// strip any digest part first (after '@')
	if at := strings.Index(ref, "@"); at != -1 {
		ref = ref[:at]
	}
	// find last ':' and last '/'
	lastColon := strings.LastIndex(ref, ":")
	lastSlash := strings.LastIndex(ref, "/")
	if lastColon == -1 || lastColon < lastSlash {
		return ""
	}
	return ref[lastColon+1:]
}

// helper to persist metadata.version into the primary file
// dir: directory containing the primary file
// primaryFile: filename (e.g., "motif.yaml" or "katalog.yaml")
// newVersion: version string to write into metadata.version
// uses WriteFileAndFormat so the file is formatted after the change.
func PersistMetadataVersion(dir, primaryFile, newVersion string) error {
	path := filepath.Join(dir, primaryFile)
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", primaryFile, err)
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing %s: %w", primaryFile, err)
	}

	meta, ok := doc["metadata"].(map[string]interface{})
	if !ok || meta == nil {
		meta = make(map[string]interface{})
		doc["metadata"] = meta
	}
	meta["version"] = newVersion

	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", primaryFile, err)
	}

	// Write and then format the file for consistent layout
	if err := utils.WriteFileAndFormat(path, out, 0o644); err != nil {
		return fmt.Errorf("writing/formatting %s: %w", primaryFile, err)
	}
	return nil
}

// IsFilePath reports whether ref is a local file reference.
func IsFilePath(ref string) bool {
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "../") {
		return true
	}
	return strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml")
}

// CleanOCIRef returns a clean raw OCI
func CleanOCIRef(input string) string {
	if !IsOCIRef(input) {
		return ""
	}
	input = strings.TrimPrefix(input, "oci://")
	return strings.TrimSuffix(input, "/")
}
