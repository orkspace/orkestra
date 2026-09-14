package registry

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPersistMetadataVersion(t *testing.T) {
	t.Run("updates existing metadata.version", func(t *testing.T) {
		dir := t.TempDir()
		primary := "motif.yaml"
		path := filepath.Join(dir, primary)

		orig := `
kind: Motif
metadata:
  name: postgres
  version: v1
  description: "test motif"
`
		if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
			t.Fatalf("write primary file: %v", err)
		}

		newVersion := "v2"
		if err := PersistMetadataVersion(dir, primary, newVersion); err != nil {
			t.Fatalf("persistMetadataVersion failed: %v", err)
		}

		out, err := readLocal(path)
		if err != nil {
			t.Fatalf("read updated file: %v", err)
		}

		var doc map[string]interface{}
		if err := yaml.Unmarshal(out, &doc); err != nil {
			t.Fatalf("unmarshal updated file: %v", err)
		}

		meta, ok := doc["metadata"].(map[string]interface{})
		if !ok {
			t.Fatalf("metadata section missing after update")
		}
		if got, _ := meta["version"].(string); got != newVersion {
			t.Fatalf("version = %q; want %q", got, newVersion)
		}
	})

	t.Run("creates metadata and sets version when metadata missing", func(t *testing.T) {
		dir := t.TempDir()
		primary := "katalog.yaml"
		path := filepath.Join(dir, primary)

		orig := `
kind: Katalog
spec:
  something: true
`
		if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
			t.Fatalf("write primary file: %v", err)
		}

		newVersion := "v0.1.0"
		if err := PersistMetadataVersion(dir, primary, newVersion); err != nil {
			t.Fatalf("persistMetadataVersion failed: %v", err)
		}

		out, err := readLocal(path)
		if err != nil {
			t.Fatalf("read updated file: %v", err)
		}

		var doc map[string]interface{}
		if err := yaml.Unmarshal(out, &doc); err != nil {
			t.Fatalf("unmarshal updated file: %v", err)
		}

		meta, ok := doc["metadata"].(map[string]interface{})
		if !ok {
			t.Fatalf("metadata section missing after update")
		}
		if got, _ := meta["version"].(string); got != newVersion {
			t.Fatalf("version = %q; want %q", got, newVersion)
		}
	})
}
