// pkg/registry/pattern.go
//
// Generic pattern layer for the Orkestra registry.
//
// Every Orkestra pattern file carries a kind: field. This package reads that
// field to determine the pattern's media type, required/optional files, and
// which registry to push to — without a separate code path per kind.
//
// To add a new pattern kind: add one entry to patternSpecs. Nothing else changes.
package registry

import (
	"fmt"
	"os"
	"path/filepath"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"gopkg.in/yaml.v3"
)

// patternSpecs is the registry of known pattern kinds.
// Add new kinds here — no other changes required.
var patternSpecs = map[PatternKind]*PatternSpec{
	KatalogKind: {
		Kind:          KatalogKind,
		MediaType:     "application/vnd.orkestra.pattern.v1+tar+gzip",
		PrimaryFile:   FileKatalog,
		RequiredFiles: []string{FileKatalog},
		OptionalFiles: []string{FileKomposer, FileCRD, FileReadme, FileCR, FileE2E, FileSimulate, FileIntentYAML, FileIntentJSON, FileGoMod, FileGoSum, FileMakefile},
	},
	MotifKind: {
		Kind:          MotifKind,
		MediaType:     "application/vnd.orkestra.motif.v1+tar+gzip",
		PrimaryFile:   FileMotif,
		RequiredFiles: []string{FileMotif},
		OptionalFiles: []string{FileReadme, "example/"},
	},
}

// DetectKind reads the primary YAML file in dir and returns the pattern kind.
// Tries katalog.yaml first, then motif.yaml.
func DetectKind(dir string) (PatternKind, *PatternSpec, error) {
	candidates := []string{FileKatalog, FileMotif}
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		data, err := readLocal(path)
		if err != nil {
			continue
		}
		var header struct {
			Kind string `yaml:"kind"`
		}
		if err := yaml.Unmarshal(data, &header); err != nil {
			return UnknownKind, nil, fmt.Errorf("reading %s: %w", name, err)
		}
		kind := PatternKind(header.Kind)
		if spec, ok := patternSpecs[kind]; ok {
			return kind, spec, nil
		}
	}
	return UnknownKind, nil, fmt.Errorf(
		"no recognized Orkestra pattern in %s (expected %s with kind: Katalog, or motif.yaml with kind: Motif)",
		dir, FileKatalog,
	)
}

// SpecFor returns the PatternSpec for a given kind.
func SpecFor(kind PatternKind) (*PatternSpec, error) {
	spec, ok := patternSpecs[kind]
	if !ok {
		return nil, fmt.Errorf("unknown pattern kind: %q", kind)
	}
	return spec, nil
}

// ValidatePatternDirectory validates that dir contains a well-formed pattern
// of the auto-detected kind. Returns the kind, spec, and the list of files to include.
func ValidatePatternDirectory(dir string) (PatternKind, *PatternSpec, []string, error) {
	kind, spec, err := DetectKind(dir)
	if err != nil {
		return UnknownKind, nil, nil, err
	}

	var files []string
	for _, f := range spec.RequiredFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); os.IsNotExist(err) {
			return kind, spec, nil, fmt.Errorf("%s pattern missing required file: %s", kind, f)
		}
		files = append(files, f)
	}
	for _, f := range spec.OptionalFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			files = append(files, f)
		}
	}

	return kind, spec, files, nil
}

// LoadPatternMeta reads name/version/description from the primary file.
// Works for both Katalog (katalog.yaml) and Motif (motif.yaml).
func LoadPatternMeta(dir string, spec *PatternSpec) (*PatternMeta, error) {
	path := filepath.Join(dir, spec.PrimaryFile)
	data, err := readLocal(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", spec.PrimaryFile, err)
	}
	var raw struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name        string   `yaml:"name"`
			Version     string   `yaml:"version"`
			Description string   `yaml:"description"`
			Author      string   `yaml:"author"`
			License     string   `yaml:"license"`
			Tags        []string `yaml:"tags"`
		} `yaml:"metadata"`
		Lifecycle *orktypes.KatalogLifecycle `yaml:"lifecycle"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", spec.PrimaryFile, err)
	}
	if raw.Metadata.Name == "" {
		return nil, fmt.Errorf("%s: metadata.name is required", spec.PrimaryFile)
	}
	meta := &PatternMeta{
		Kind:        PatternKind(raw.Kind),
		Name:        raw.Metadata.Name,
		Version:     raw.Metadata.Version,
		Description: raw.Metadata.Description,
		Author:      raw.Metadata.Author,
		License:     raw.Metadata.License,
		Tags:        raw.Metadata.Tags,
	}
	if raw.Lifecycle != nil {
		if d := raw.Lifecycle.Deprecation; d != nil {
			meta.Deprecated = &PatternDeprecated{
				MigratedTo:   d.MigratedTo,
				Message:      d.Message,
				TimelineFrom: d.TimelineFrom(),
				TimelineTo:   d.TimelineTo(),
			}
		}
	}
	if meta.Version == "" {
		meta.Version = "latest"
	}
	if meta.Description == "" {
		meta.Description = fmt.Sprintf("%s %s", kind(spec.Kind), meta.Name)
	}
	return meta, nil
}

// kind returns a display string for a PatternKind.
func kind(k PatternKind) string {
	switch k {
	case KatalogKind:
		return "Pattern"
	case MotifKind:
		return "Motif"
	default:
		return "Pattern"
	}
}

// mediaTypeForPatternFile returns the OCI layer media type for a file within
// a specific pattern kind.
func mediaTypeForPatternFile(name string, k PatternKind) string {
	switch name {
	case FileKatalog:
		return "application/vnd.orkestra.katalog.v1+yaml"
	case FileCRD:
		return "application/vnd.kubernetes.crd.v1+yaml"
	case FileCR:
		return "application/vnd.kubernetes.cr.v1+yaml"
	case FileReadme:
		return "text/markdown"
	case FileMotif:
		return "application/vnd.orkestra.motif.v1+yaml"
	case FileE2E:
		return "application/vnd.orkestra.e2e.v1+yaml"
	case FileSimulate:
		return "application/vnd.orkestra.simulate.v1+yaml"
	case FileGoMod, FileGoSum:
		return "text/plain"
	case FileMakefile:
		return "text/x-makefile"
	default:
		return "application/octet-stream"
	}
}
