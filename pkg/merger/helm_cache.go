// pkg/merger/helm_cache.go
//
// Disk cache for Helm sources resolved by the merger.
//
// Two namespaces:
//
//	~/.orkestra/helm/git/<sha256>/   — git-sourced charts (repo + ref + chart path)
//	~/.orkestra/helm/repo/<sha256>/  — remote Helm repository charts (repo + chart + version)
//
// Cache key is the SHA256 of the tuple that uniquely identifies the artifact.
// Sentinel file: Chart.yaml — if it exists, the cache entry is complete.
// Callers pass refresh=true to bypass the cache and overwrite the stored copy.
//
// Authoring-time only, same as helm.go: this file's only entry point
// (WarmHelmSource) is called exclusively from the dev-only `ork pull`
// command and depends on helm.go's resolveChartPath, which doesn't exist
// in the runtime/gateway builds.

//go:build !runtime && !gateway

package merger

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// helmGitCacheKey returns a stable SHA256 key for a git helm source.
func helmGitCacheKey(src orktypes.HelmSource) string {
	ref := src.Version
	if ref == "" {
		ref = "HEAD"
	}
	subpath := src.Path
	if subpath == "" {
		subpath = src.Chart
	}
	h := sha256.Sum256([]byte(src.Repo + "\x00" + ref + "\x00" + subpath))
	return fmt.Sprintf("%x", h)
}

// helmRepoCacheKey returns a stable SHA256 key for a remote Helm repo source.
func helmRepoCacheKey(src orktypes.HelmSource) string {
	h := sha256.Sum256([]byte(src.Repo + "\x00" + src.Chart + "\x00" + src.Version))
	return fmt.Sprintf("%x", h)
}

func helmCacheRoot(sub string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	return filepath.Join(home, sub), nil
}

// helmGitCached returns the cached chart directory for a git helm source.
// Returns ("", false) on miss or any error.
func helmGitCached(src orktypes.HelmSource) (string, bool) {
	root, err := helmCacheRoot(".orkestra/helm/git")
	if err != nil {
		return "", false
	}
	dir := filepath.Join(root, helmGitCacheKey(src))
	if _, err := os.Stat(filepath.Join(dir, "Chart.yaml")); err == nil {
		return dir, true
	}
	return "", false
}

// helmGitCacheStore copies a resolved chart directory into the git helm cache
// and returns the cached path. Overwrites any existing entry.
func helmGitCacheStore(src orktypes.HelmSource, chartDir string) (string, error) {
	root, err := helmCacheRoot(".orkestra/helm/git")
	if err != nil {
		return "", err
	}
	dest := filepath.Join(root, helmGitCacheKey(src))
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := helmCopyDir(chartDir, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// helmRepoCached returns the cached chart directory for a remote Helm repo source.
// Returns ("", false) on miss or any error.
func helmRepoCached(src orktypes.HelmSource) (string, bool) {
	root, err := helmCacheRoot(".orkestra/helm/repo")
	if err != nil {
		return "", false
	}
	dir := filepath.Join(root, helmRepoCacheKey(src))
	if _, err := os.Stat(filepath.Join(dir, "Chart.yaml")); err == nil {
		return dir, true
	}
	return "", false
}

// helmRepoCacheStore copies a pulled chart directory into the remote Helm repo cache
// and returns the cached path. Overwrites any existing entry.
func helmRepoCacheStore(src orktypes.HelmSource, chartDir string) (string, error) {
	root, err := helmCacheRoot(".orkestra/helm/repo")
	if err != nil {
		return "", err
	}
	dest := filepath.Join(root, helmRepoCacheKey(src))
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := helmCopyDir(chartDir, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// WarmHelmSource pre-warms the local cache for a HelmSource.
// Equivalent to what the merger does on first use, but called explicitly by
// ork pull so subsequent commands are served from cache.
// When refresh is true the existing cache entry is discarded and re-fetched.
func WarmHelmSource(src orktypes.HelmSource, refresh bool) error {
	_, cleanup, err := resolveChartPath(src, refresh)
	if cleanup != nil {
		cleanup()
	}
	return err
}

// helmCopyDir recursively copies src into dst.
func helmCopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := readLocal(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
