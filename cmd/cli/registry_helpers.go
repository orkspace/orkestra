//go:build !runtime && !gateway

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ── helpers ───────────────────────────────────────────────────────────────────
func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func wordWrap(s string, width int, indent string) string {
	if len(s) <= width {
		return s
	}
	return s[:width] + "\n" + indent + wordWrap(s[width:], width, indent)
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := readLocal(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

// validateCRDFile checks that path is a valid YAML file with the required
// CustomResourceDefinition fields: apiVersion, kind, spec.group, spec.names.kind.
func validateCRDFile(path string) error {
	data, err := readLocal(path)
	if err != nil {
		return err
	}
	var crd struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Spec       struct {
			Group string `yaml:"group"`
			Names struct {
				Kind string `yaml:"kind"`
			} `yaml:"names"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &crd); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	if crd.Kind != "CustomResourceDefinition" {
		return fmt.Errorf("kind must be CustomResourceDefinition, got %q", crd.Kind)
	}
	if crd.Spec.Group == "" {
		return fmt.Errorf("spec.group is required")
	}
	if crd.Spec.Names.Kind == "" {
		return fmt.Errorf("spec.names.kind is required")
	}
	return nil
}
