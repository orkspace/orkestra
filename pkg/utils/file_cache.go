// pkg/utils/file_cache.go
//
// SHA256-keyed disk cache for remote file fetches (https://).
// Cache layout: ~/.orkestra/files/<sha256hex>
// A hit is any file that exists at the expected path.
// Callers are responsible for bypassing the cache on --refresh.
package utils

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

const fileCacheBase = ".orkestra/files"

// fileCachePath returns the local path for a cached remote URL.
func fileCachePath(url string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	hash := sha256.Sum256([]byte(url))
	return filepath.Join(home, fileCacheBase, fmt.Sprintf("%x", hash)), nil
}

// CachedFileBytes returns the cached bytes for a remote URL, if present.
// Returns (nil, false) on any miss or error.
func CachedFileBytes(url string) ([]byte, bool) {
	path, err := fileCachePath(url)
	if err != nil {
		return nil, false
	}
	data, err := ReadLocal(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// CacheFileBytes writes bytes to the cache for a remote URL.
// Best-effort — callers should not fail on a cache write error.
func CacheFileBytes(url string, data []byte) error {
	path, err := fileCachePath(url)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// InvalidateFileCache removes the cached entry for a remote URL.
// Used by --refresh to force a re-fetch on the next access.
func InvalidateFileCache(url string) {
	path, err := fileCachePath(url)
	if err != nil {
		return
	}
	_ = os.Remove(path)
}
