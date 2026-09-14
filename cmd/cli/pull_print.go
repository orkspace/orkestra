//go:build !runtime && !gateway

package cli

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"

	"github.com/orkspace/orkestra/pkg/registry"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// detectCacheType resolves the artifact kind by checking for sentinel files on
// disk — not by sniffing the path string. Returns (isMotif, isKatalog, patternFile).
func detectCacheType(cacheDir string) (bool, bool, string) {
	motifFile := filepath.Join(cacheDir, registry.FileMotif)
	if _, err := os.Stat(motifFile); err == nil {
		return true, false, motifFile
	}
	katalogFile := filepath.Join(cacheDir, registry.FileKatalog)
	if _, err := os.Stat(katalogFile); err == nil {
		return false, true, katalogFile
	}
	return false, false, ""
}

// readMotif reads and unmarshals a motif file. Returns nil and error on failure.
func readMotif(path string) (*orktypes.Motif, error) {
	data, err := readLocal(path)
	if err != nil {
		return nil, err
	}
	var m orktypes.Motif
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// inputsSummary returns derived values and a small sample of inputs to print.
type inputsSummary struct {
	TotalCount       int
	HasInputs        bool
	RequiredCount    int
	RequiredInputs   []orktypes.MotifInput
	FirstTwoDefaults []orktypes.MotifInput
}

func collectInputsSummary(inputs []orktypes.MotifInput) inputsSummary {
	s := inputsSummary{TotalCount: len(inputs)}
	if s.TotalCount == 0 {
		return s
	}
	s.HasInputs = true
	for _, in := range inputs {
		if in.Required {
			s.RequiredInputs = append(s.RequiredInputs, in)
		}
	}
	s.RequiredCount = len(s.RequiredInputs)
	// collect first two non-required inputs for display
	for _, in := range inputs {
		if !in.Required && len(s.FirstTwoDefaults) < 2 {
			s.FirstTwoDefaults = append(s.FirstTwoDefaults, in)
		}
	}
	return s
}

// printValidationHint prints the ork validate hint for the pattern file.
func printValidationHint(patternFile string) {
	if patternFile == "" {
		return
	}
	fmt.Print("\nValidate the pattern:\n")
	fmt.Printf("  ork validate -f %s\n", patternFile)
}

// printMotifReference prints motif usage and inputs sample.
func printMotifReference(ref *registry.Ref, motif *orktypes.Motif, sum inputsSummary) {
	fmt.Printf("\nReference in a Katalog:\n")
	fmt.Printf("  imports:\n")
	fmt.Printf("    - motif: %s\n", ref.String())

	if !sum.HasInputs {
		return
	}

	fmt.Printf("      with:\n")
	shown := 0
	if sum.RequiredCount > 0 {
		for i := 0; i < sum.RequiredCount && i < 2; i++ {
			fmt.Printf("        %-30s # required\n", sum.RequiredInputs[i].Name+": <value>")
			shown++
		}
	}
	for _, in := range sum.FirstTwoDefaults {
		if shown >= 2 {
			break
		}
		fmt.Printf("        %-30s # defaults to: %s\n", in.Name+": <value>", in.Default)
		shown++
	}
	remaining := sum.TotalCount - shown
	if remaining > 0 {
		fmt.Printf("        # ...and %d more inputs\n", remaining)
	}
}

// printKatalogReference prints katalog usage and run hint.
func printKatalogReference(ref *registry.Ref, cacheDir string) {
	fmt.Printf("\nRun this katalog pattern:\n")
	fmt.Printf("  ork run -f %s\n", filepath.Join(cacheDir, registry.FileKatalog))
	fmt.Printf("\nOr reference in a Komposer:\n")
	fmt.Printf("  imports:\n")
	fmt.Printf("    registry:\n")
	fmt.Printf("      - url: %s\n", ref.String())
}

// printCachedFiles lists the files in cacheDir so the user knows what was pulled.
func printCachedFiles(cacheDir string) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	fmt.Printf("\n  Files:\n")
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		fmt.Printf("    %-30s %s\n", e.Name(), formatSize(size))
	}
}

// printPullSuggestions orchestrates the helpers and handles errors gracefully.
func printPullSuggestions(ref *registry.Ref, cacheDir string) {
	isMotif, isKatalog, patternFile := detectCacheType(cacheDir)
	printCachedFiles(cacheDir)
	printValidationHint(patternFile)

	if isMotif {
		motif, err := readMotif(patternFile)
		if err != nil {
			// non-fatal: print a short message and return
			fmt.Fprintf(os.Stderr, "warning: failed to read motif %s: %v\n", patternFile, err)
			return
		}
		sum := collectInputsSummary(motif.Inputs)
		printMotifReference(ref, motif, sum)
		return
	}

	if isKatalog {
		printKatalogReference(ref, cacheDir)
		return
	}

	// fallback: nothing recognized
	fmt.Println("No motif or katalog pattern detected in cache.")
}
