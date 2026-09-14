// pkg/merger/helper.go
package merger

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
)

var (
	readLocal        = utils.ReadLocal
	strictUnmarshal  = utils.StrictUnmarshal
	loadFile         = utils.LoadFile
	loadFileWithAuth = utils.LoadFileWithAuth
	resolveEnvVar    = utils.ResolveEnvVar
)

// mergeKatalogSecurity merges two KatalogSecurity values.
// Fields that are non-nil/non-zero in override win; otherwise the base value is kept.
// This is the correct semantics for Komposer layering: source Katalog settings are
// inherited, and the Komposer only needs to declare what it explicitly wants to change.
func mergeKatalogSecurity(base, override orktypes.KatalogSecurity) orktypes.KatalogSecurity {
	result := base
	if override.DeletionProtection != nil {
		result.DeletionProtection = override.DeletionProtection
	}
	if override.Webhooks != nil {
		result.Webhooks = override.Webhooks
	}
	if override.Conversion != nil {
		result.Conversion = override.Conversion
	}
	if override.NamespaceProtection != nil {
		result.NamespaceProtection = override.NamespaceProtection
	}
	if override.ServiceName != nil {
		result.ServiceName = override.ServiceName
	}
	return result
}

// mergeKatalogNotification merges two KatalogNotification values.
// Source teams are inherited as the base; override teams win on name conflict.
// If override declares Defaults, those replace the base Defaults entirely.
// A nil override returns base unchanged; a nil base returns override.
func mergeKatalogNotification(base, override *orktypes.KatalogNotification) *orktypes.KatalogNotification {
	if override == nil {
		return base
	}
	if base == nil {
		return override
	}
	result := *base
	if len(override.Teams) > 0 {
		if result.Teams == nil {
			result.Teams = make(map[string]*orktypes.NotificationTeam, len(override.Teams))
		}
		for name, team := range override.Teams {
			result.Teams[name] = team
		}
	}
	if override.Defaults != nil {
		result.Defaults = override.Defaults
	}
	return &result
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (m *Merger) mustBeMerged() {
	if !m.merged {
		panic("merger: call Merge() before querying")
	}
}

// checkDuplicate returns an error if name is already in seen from a different source.
func checkDuplicate(seen map[string]string, name, source string) error {
	if existing, ok := seen[name]; ok && existing != source {
		return fmt.Errorf(
			"duplicate CRD %q: defined in %q and %q — names must be unique across all imports",
			name, existing, source,
		)
	}
	return nil
}

// writeTempFile writes data to a temp file and returns the path.
func writeTempFile(data []byte, pattern string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("writing temp file: %w", err)
	}
	return f.Name(), nil
}

// gitClone clones a git repository into dst at the given ref.
// ref may be a branch, tag, or commit hash.
func gitClone(repo, dst, ref string) error {
	if ref == "" {
		ref = "HEAD"
	}

	// First attempt: shallow clone at branch/tag
	cmd := exec.Command("git", "clone",
		"--depth", "1",
		"--branch", ref,
		repo,
		dst,
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err == nil {
		return nil
	}

	// Fallback: full clone + checkout (for commit hashes)
	if err := exec.Command("git", "clone", repo, dst).Run(); err != nil {
		return fmt.Errorf("git clone failed for %q: %w", repo, err)
	}

	if err := exec.Command("git", "-C", dst, "checkout", ref).Run(); err != nil {
		return fmt.Errorf("git checkout %q failed in %q: %w", ref, repo, err)
	}

	return nil
}

// isFileMotif returns true when a motif reference is a local file path.
// Matches ./, ../, absolute paths, and bare relative paths (e.g. "motifs/foo/motif.yaml").
// OCI references (ghcr.io/...) and URLs (https://...) do not match.
func isFileMotif(motif string) bool {
	return strings.HasPrefix(motif, "./") ||
		strings.HasPrefix(motif, "../") ||
		filepath.IsAbs(motif) ||
		(!strings.Contains(motif, "://") && !strings.Contains(motif, ":") && strings.Contains(motif, "/"))
}

// unused but kept for completeness
var _ = bytes.NewBuffer
