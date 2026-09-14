// pkg/motif/loader.go
//
// Loads a Motif from a file path or registry reference.
//
// Resolution mirrors RegistrySource in a Komposer exactly — the same four
// reference forms are supported:
//
//	motif: postgres                                            # bare name → default motif registry (OCI)
//	motif: oci://ghcr.io/orkspace/orkestra-motifs/postgres:v0.1.0   # oci:// prefix → OCI
//	motif: ghcr.io/orkspace/orkestra-motifs/postgres@v0.1.0          # full OCI ref with oci: true
//	motif: https://github.com/myorg/postgres-motif@main              # git URL
//	motif: ./motifs/postgres/motif.yaml                              # file path
//
// Bare names and oci:// prefixes are auto-detected so oci: true is not required
// for those forms. For full OCI refs (host with dots), oci: true is still required
// — same as RegistrySource.
package motif

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/merger"
	"github.com/orkspace/orkestra/pkg/registry"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
)

var (
	readLocal       = utils.ReadLocal
	strictUnmarshal = utils.StrictUnmarshal
)

// Load loads a Motif from a local file path.
// For full import resolution (registry, OCI, auth), use LoadImport.
func Load(path string) (*orktypes.Motif, error) {
	data, err := readLocal(path)
	if err != nil {
		return nil, fmt.Errorf("reading motif %s: %w", path, err)
	}
	m, err := parse(data)
	if err != nil {
		return nil, err
	}
	if err := expandIncludes(m, filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("motif %s: %w", path, err)
	}
	return m, nil
}

// LoadImport resolves and loads a Motif from a MotifImport declaration.
//
// Resolution order:
//  1. File path (starts with ./, ../, /, or ends with .yaml/.yml)
//  2. oci:// prefix → OCI pull (auto-detected, oci: field not required)
//  3. Bare name (no scheme, no dots in registry host) → resolved against the
//     default motif registry (ORK_MOTIFS_REGISTRY or ghcr.io/orkspace/orkestra-motifs)
//  4. Full OCI ref + oci: true → OCI pull (komposer-compatible form)
//  5. Git URL (https://, http://, git@) → git pull
func LoadImport(imp *orktypes.MotifImport) (*orktypes.Motif, error) {
	ref := strings.TrimSpace(imp.Motif)

	// File path — relative, absolute, or ends with .yaml/.yml
	if registry.IsFilePath(ref) {
		return Load(ref)
	}

	oci := imp.OCI

	// oci:// prefix → always OCI, strip prefix before further parsing.
	if registry.IsOCIRef(ref) {
		oci = true
		ref = registry.CleanOCIRef(ref)
	}

	// Bare name — no scheme, no dots in the host segment → resolve against
	// the default motif registry and pull via OCI.
	// e.g. "postgres" or "postgres:v0.1.0"
	if !oci && !isGitURL(ref) && !registry.LooksLikeFullRef(ref) {
		resolved, err := registry.ResolveForKind(ref, registry.MotifKind)
		if err != nil {
			return nil, fmt.Errorf("motif %q: resolving reference: %w", imp.Motif, err)
		}
		ref = resolved.Full // e.g. "ghcr.io/orkspace/orkestra-motifs/postgres:v0.1.0"
		oci = true
	}

	// Parse cleanURL and version.
	// Supports both OCI's :tag syntax and the @version shorthand used by RegistrySource.
	cleanURL, version := resolveMotifRef(ref, imp.Version, oci)

	auth, err := imp.Auth.Resolve()
	if err != nil {
		return nil, fmt.Errorf("motif %q: auth: %w", imp.Motif, err)
	}

	tmpDir, cleanup, err := merger.PullMotifToDir(cleanURL, version, oci, auth)
	if err != nil {
		return nil, fmt.Errorf("motif %q@%s: pull failed: %w", cleanURL, version, err)
	}
	defer cleanup()

	fileName := registry.FileMotif
	data, err := readLocal(filepath.Join(tmpDir, fileName))
	if err != nil {
		return nil, fmt.Errorf("motif %q@%s: %s not found in artifact: %w", cleanURL, version, fileName, err)
	}

	return parse(data)
}

// isGitURL reports whether ref is a Git remote URL.
func isGitURL(ref string) bool {
	return strings.HasPrefix(ref, "https://") ||
		strings.HasPrefix(ref, "http://") ||
		strings.HasPrefix(ref, "git@")
}

// resolveMotifRef returns the (cleanURL, version) pair ready for PullMotifToDir.
//
// Precedence:
//  1. @version shorthand in ref  — "ghcr.io/.../postgres@v14"
//  2. :tag at the end of an OCI ref — "ghcr.io/.../postgres:v14"
//  3. Explicit imp.Version field
//  4. Default: "latest" for OCI, "main" for Git
func resolveMotifRef(ref, version string, oci bool) (cleanURL, resolvedVersion string) {
	// @ shorthand (komposer style) — takes precedence over everything.
	if idx := strings.LastIndex(ref, "@"); idx != -1 {
		return ref[:idx], ref[idx+1:]
	}

	// For OCI refs, a colon after the last slash is the image tag.
	// e.g. "ghcr.io/orkspace/orkestra-motifs/postgres:v0.1.0"
	if oci {
		if colonIdx := strings.LastIndex(ref, ":"); colonIdx > strings.LastIndex(ref, "/") {
			return ref[:colonIdx], ref[colonIdx+1:]
		}
	}

	// Explicit version field or defaults.
	cleanURL = ref
	if version != "" {
		return cleanURL, version
	}
	if oci {
		return cleanURL, "latest"
	}
	return cleanURL, "main"
}

func parse(data []byte) (*orktypes.Motif, error) {
	var m orktypes.Motif
	if err := strictUnmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing motif: %w", err)
	}
	if !konfig.IsMotifKind(m.Kind) {
		return nil, fmt.Errorf("expected kind: Motif, got: %s", m.Kind)
	}
	if m.Metadata.Name == "" {
		return nil, fmt.Errorf("motif metadata.name is required")
	}
	return &m, nil
}
