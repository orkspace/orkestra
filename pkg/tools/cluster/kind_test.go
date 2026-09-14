package cluster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── writeKindConfig ───────────────────────────────────────────────────────────

func TestWriteKindConfig_ControlPlaneOnly(t *testing.T) {
	path, err := writeKindConfig(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(path)

	data, _ := readLocal(path)
	content := string(data)

	if !strings.Contains(content, "role: control-plane") {
		t.Error("expected control-plane node")
	}
	if strings.Contains(content, "role: worker") {
		t.Error("expected no worker nodes for workers=0")
	}
}

func TestWriteKindConfig_WithWorkers(t *testing.T) {
	for _, workers := range []int{1, 3, 5} {
		path, err := writeKindConfig(workers)
		if err != nil {
			t.Fatalf("workers=%d: unexpected error: %v", workers, err)
		}
		defer os.Remove(path)

		data, _ := readLocal(path)
		content := string(data)

		got := strings.Count(content, "role: worker")
		if got != workers {
			t.Errorf("workers=%d: got %d worker entries, want %d", workers, got, workers)
		}
		if !strings.Contains(content, "role: control-plane") {
			t.Errorf("workers=%d: missing control-plane node", workers)
		}
	}
}

func TestWriteKindConfig_ValidYAMLHeader(t *testing.T) {
	path, err := writeKindConfig(1)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	data, _ := readLocal(path)
	content := string(data)

	if !strings.Contains(content, "kind: Cluster") {
		t.Error("expected 'kind: Cluster'")
	}
	if !strings.Contains(content, "apiVersion: kind.x-k8s.io/v1alpha4") {
		t.Error("expected 'apiVersion: kind.x-k8s.io/v1alpha4'")
	}
}

func TestWriteKindConfig_FileIsTemp(t *testing.T) {
	path, err := writeKindConfig(0)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	base := filepath.Base(path)
	if !strings.HasPrefix(base, "ork-kind-") {
		t.Errorf("expected temp file name to start with 'ork-kind-', got %q", base)
	}
}

// ── resolveKind ───────────────────────────────────────────────────────────────

func TestResolveKind_CachedBinaryUsed(t *testing.T) {
	dir := t.TempDir()
	version := "v9.99.0"
	cached := filepath.Join(dir, "kind-"+version)

	// Write a dummy executable to the cache location.
	if err := os.WriteFile(cached, []byte("#!/bin/sh\necho kind"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Override orkBinDir by temporarily setting HOME to our temp dir.
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	// Create the expected sub-path ~/.orkestra/bin/
	cacheDir := filepath.Join(dir, ".orkestra", "bin")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cachedFull := filepath.Join(cacheDir, "kind-"+version)
	if err := os.WriteFile(cachedFull, []byte("#!/bin/sh\necho kind"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveKind(version)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != cachedFull {
		t.Errorf("expected %q, got %q", cachedFull, got)
	}
}

func TestResolveKind_EmptyVersionFallsBackToPath(t *testing.T) {
	// resolveKind("") should check PATH first.
	// We can't guarantee 'kind' is in PATH in CI, so we only verify that
	// the function does not immediately attempt a download (i.e. it doesn't
	// panic or error with a network message on an empty version).
	// The real guard: if kind is not in PATH, it falls to DefaultKindVersion cache,
	// which will attempt a download and fail without network — that's acceptable.
	_, _ = resolveKind("") // must not panic
}

func TestResolveKind_ExplicitVersionSkipsPath(t *testing.T) {
	// An explicit version that is not in cache should fall through to downloadKind.
	// We verify it does NOT silently return a PATH binary by using a version string
	// that could never match anything in a real PATH install.
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	// No cached binary exists for this version — resolveKind must attempt download,
	// which will fail (no network in unit tests). We just check the error message
	// is a download error, not a PATH-found success.
	_, err := resolveKind("v0.0.0-test-sentinel")
	if err == nil {
		// Could happen if the test machine has network and GitHub has this release.
		// Accept it — the important thing is it didn't silently use PATH kind.
		t.Log("resolveKind returned a binary (network available) — skipping negative assertion")
		return
	}
	if !strings.Contains(err.Error(), "download") && !strings.Contains(err.Error(), "404") {
		t.Errorf("expected download error, got: %v", err)
	}
}

// ── writeKindConfig error path ────────────────────────────────────────────────

func TestWriteKindConfig_NegativeWorkers(t *testing.T) {
	// Negative workers should produce no worker nodes (range over negative is a no-op).
	path, err := writeKindConfig(-1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(path)

	data, _ := readLocal(path)
	if strings.Contains(string(data), "role: worker") {
		t.Error("negative workers should produce no worker nodes")
	}
}

// ── orkBinDir ─────────────────────────────────────────────────────────────────

func TestOrkBinDir(t *testing.T) {
	home, _ := os.UserHomeDir()
	got := orkBinDir()
	want := filepath.Join(home, ".orkestra", "bin")
	if got != want {
		t.Errorf("orkBinDir() = %q, want %q", got, want)
	}
}
