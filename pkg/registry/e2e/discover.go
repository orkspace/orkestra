package e2e

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"gopkg.in/yaml.v3"
)

// DiscoverE2EFiles walks root recursively and returns paths to all *e2e.yaml
// files that are not pure aggregators (files with imports: but no spec:).
// Results are sorted for deterministic order. skip is a list of glob patterns
// (matched against the full path); any file whose path matches is excluded.
func DiscoverE2EFiles(root string, skip []string) ([]string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var found []string
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			for _, pattern := range skip {
				if matchesSkip(pattern, path) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "e2e.yaml") {
			return nil
		}
		for _, pattern := range skip {
			if matchesSkip(pattern, path) {
				return nil
			}
		}
		if isPureAggregatorFile(path) {
			return nil
		}
		found = append(found, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(found)
	return found, nil
}

// BuildDiscoveryE2E constructs an in-memory E2E aggregator from a list of
// discovered file paths. wait is injected on every import except the first.
func BuildDiscoveryE2E(paths []string, wait string) orktypes.E2E {
	imports := make([]orktypes.E2EImport, len(paths))
	for i, p := range paths {
		imp := orktypes.E2EImport{Path: p}
		if i > 0 && wait != "" {
			imp.Wait = wait
		}
		imports[i] = imp
	}
	return orktypes.E2E{
		APIVersion: "orkestra.orkspace.io/v1",
		Kind:       "E2E",
		Metadata:   orktypes.E2EMeta{Name: "discovered-suite"},
		Imports:    imports,
	}
}

// matchesSkip returns true when path contains pattern as a path component or
// matches it as a glob against the base name.
func matchesSkip(pattern, path string) bool {
	base := filepath.Base(path)
	if ok, _ := filepath.Match(pattern, base); ok {
		return true
	}
	// Also match if any path component equals the pattern exactly.
	for _, part := range strings.Split(path, string(os.PathSeparator)) {
		if part == pattern {
			return true
		}
	}
	return false
}

// DiscoverSimulateFiles walks root recursively and returns paths to all
// simulate.yaml leaf files (files with a spec:, not pure aggregators).
// Results are sorted for deterministic order.
func DiscoverSimulateFiles(root string, skip []string) ([]string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var found []string
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			for _, pattern := range skip {
				if matchesSkip(pattern, path) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "simulate.yaml") {
			return nil
		}
		for _, pattern := range skip {
			if matchesSkip(pattern, path) {
				return nil
			}
		}
		if isSimulatePureAggregator(path) {
			return nil
		}
		found = append(found, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(found)
	return found, nil
}

// isSimulatePureAggregator returns true when the file has imports but no spec.
func isSimulatePureAggregator(path string) bool {
	data, err := readLocal(path)
	if err != nil {
		return false
	}
	var doc struct {
		Imports []string `yaml:"imports"`
		Spec    *struct {
			Katalog string `yaml:"katalog"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	return len(doc.Imports) > 0 && doc.Spec == nil
}

// isPureAggregatorFile reads the file and returns true if it has imports but no spec.
func isPureAggregatorFile(path string) bool {
	data, err := readLocal(path)
	if err != nil {
		return false
	}
	var doc struct {
		Imports []interface{} `yaml:"imports"`
		Spec    struct {
			Katalog string   `yaml:"katalog"`
			CR      string   `yaml:"cr"`
			CRFiles []string `yaml:"crFiles"`
			Custom  *struct {
				Target string `yaml:"target"`
			} `yaml:"custom"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	hasSpec := doc.Spec.Katalog != "" || doc.Spec.CR != "" || len(doc.Spec.CRFiles) > 0 || (doc.Spec.Custom != nil && doc.Spec.Custom.Target != "")
	return len(doc.Imports) > 0 && !hasSpec
}
