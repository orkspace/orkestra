//go:build !runtime && !gateway

package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"path/filepath"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/pipeline"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/registry/e2e"
	motifpkg "github.com/orkspace/orkestra/pkg/registry/motif"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	rbacv1 "k8s.io/api/rbac/v1"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate an Orkestra pattern (Katalog, Komposer, Motif, E2E, Simulate)",
	Long: `Validates any Orkestra pattern and reports errors.

The pattern kind is detected automatically from the 'kind' field:
  Katalog   — operator definition with CRD declarations
  Komposer  — multi-source katalog composer
  Motif     — reusable operator pattern
  E2E       — declarative end-to-end test spec
  Simulate  — declarative reconciler assertions

Reads katalog.yaml or komposer.yaml from the current directory by default.
Pass -f to validate a different file.

Examples:
  ork validate
  ork validate -f e2e.yaml
  ork validate -f simulate.yaml
  ork validate -f motif.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths, _ := cmd.Flags().GetStringSlice("file")
		expanded := parseFilePaths(paths)
		if len(expanded) == 0 {
			expanded = defaultFilePaths()
		}
		if len(expanded) == 0 {
			return fmt.Errorf(errNoKatalog)
		}

		var docKind string
		for _, path := range expanded {
			kind, err := detectKindFromFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}

			// Validate document type
			if !konfig.IsValidPatternKind(kind) {
				if kind == "" {
					return fmt.Errorf(
						"not an Orkestra pattern — expected a 'kind' field (allowed kinds: %s)",
						konfig.ValidKindsString(),
					)
				}
				return fmt.Errorf(
					"invalid Orkestra pattern kind %q (allowed kinds: %s)",
					kind, konfig.ValidKindsString(),
				)
			}

			if konfig.IsMotifKind(kind) {
				return validateMotifFile(path)
			}

			if konfig.IsE2EKind(kind) {
				return validateE2EFile(path)
			}

			if konfig.IsSimulateKind(kind) {
				playMode, _ := cmd.Flags().GetBool("play")
				return validateSimulateFilePlay(path, playMode)
			}

			docKind = kind
		}

		// Default path: Katalog / Komposer validation
		spin := StartSpinner("Resolving imports...")
		m, err := generateKatalog(cmd)
		if err != nil {
			spin.Failure()
			return err
		}
		spin.Stop()

		k, err := pipeline.BuildExpanded(kfg, m.m)
		if err != nil {
			var typedErr *katalog.TypedOperatorError
			if errors.As(err, &typedErr) {
				printTypedOperatorHint(typedErr, "ork validate")
			}
			return err
		}
		entries := k.EnabledCRDs()

		kindLabel := "Katalog"
		if konfig.IsKomposerKind(docKind) {
			kindLabel = "Komposer"
		}

		if k.IsStandaloneGateway() {
			kindLabel = "Gateway Standalone"
		}

		full, _ := cmd.Flags().GetBool("full")
		showNotes, _ := cmd.Flags().GetBool("notes")
		showProfiles, _ := cmd.Flags().GetBool("profiles")

		// --notes / --profiles: quiet mode — skip full validate output, print only that registry.
		if showNotes || showProfiles {
			if showNotes {
				printValidateNotes(k.Notes)
			}
			if showProfiles {
				printValidateProfiles(k.Profiles)
			}
			return nil
		}

		var perCRDPerms map[string][]rbacv1.PolicyRule
		if full {
			perCRDPerms = k.GeneratePerCRDRBACRules()
		}

		// Sort entries by name for stable output across runs.
		sortedEntries := make([]orktypes.CRDEntry, 0, len(entries))
		for _, e := range entries {
			sortedEntries = append(sortedEntries, e)
		}
		sort.Slice(sortedEntries, func(i, j int) bool {
			return sortedEntries[i].Name < sortedEntries[j].Name
		})

		fmt.Println()
		fmt.Println(bold("Validating " + kindLabel + "..."))
		fmt.Println()

		builtIn := 0
		custom := 0

		var deprecationHint string
		if konfig.IsKomposerKind(docKind) {
			deprecationHint = "To acknowledge this import, add it to lifecycle.accept.patterns in your Komposer."
		}
		printKatalogDeprecationWithHint(k.Deprecation(), deprecationHint)

		// Print each CRD entry with enrichment info
		for _, entry := range sortedEntries {
			printCRDValidationLine(k, entry)
			if full {
				printCRDPermissions(perCRDPerms[entry.Name])
				printCRDProfiles(entry)
			}
			fmt.Println()

			if entry.IsBuiltIn {
				builtIn++
			} else {
				custom++
			}
		}

		// Summary
		crdText := "CRDs"
		if (builtIn + custom) == 1 {
			crdText = "CRD"
		}

		fmt.Println(strings.Repeat("─", 60))
		fmt.Printf("%d %s valid (%d built-in, %d custom)\n", len(entries), crdText, builtIn, custom)

		for _, w := range k.CronConditionWarnings() {
			fmt.Printf("\n%s  %s\n", yellow("⚠"), w)
		}

		if full {
			if dd := k.DependencyDisplayData(); dd != nil {
				printValidateDependencyGraph(dd)
			}
			printRuntimePermissionsSection(k.GenerateRuntimeRBACRules())
			printGatewayPermissionsSection(k.GenerateGatewayRBACRules())
			fmt.Println()
		}

		// Always print non-empty registries inline.
		printValidateNotes(k.Notes)
		printValidateProfiles(k.Profiles)

		return nil
	},
}

// detectKindFromFile peeks at a YAML file to read its kind field.
func detectKindFromFile(path string) (string, error) {
	data, err := readLocal(path)
	if err != nil {
		return "", err
	}
	var doc struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	return doc.Kind, nil
}

// validateE2EFile validates an E2E spec file and prints a summary.
func validateE2EFile(path string) error {
	fmt.Println()
	fmt.Println(bold("Validating E2E..."))
	fmt.Println()

	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var doc orktypes.E2E
	if err := strictUnmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	baseDir := filepath.Dir(path)
	isAggregator := len(doc.Imports) > 0 && doc.Spec.Katalog == "" && doc.Spec.Init == nil

	var errs []string

	if doc.Metadata.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	isCustom := doc.Spec.Custom != nil && doc.Spec.Custom.Target != ""
	if isCustom {
		switch doc.Spec.Custom.Target {
		case orktypes.CustomTargetKubernetes:
			// valid and supported
		case orktypes.CustomTargetContainer:
			return fmt.Errorf("spec.custom.target \"container\" is coming soon — not yet supported in this version")
		default:
			errs = append(errs, fmt.Sprintf(
				"spec.custom.target %q is not supported — valid values: kubernetes, container",
				doc.Spec.Custom.Target,
			))
		}
	}
	expanded := doc.Spec.Expect
	if !isAggregator {
		if doc.Spec.Katalog == "" && doc.Spec.Init == nil && !isCustom {
			errs = append(errs, "spec.katalog is required (or spec.init for example packs, spec.custom.target for custom targets, or imports)")
		}
		if doc.Spec.CRD == "" && doc.Spec.Init == nil && !isCustom {
			errs = append(errs, "spec.crd is required (or spec.init for example packs, spec.custom.target for custom targets, or imports)")
		}
		if doc.Spec.CR == "" && doc.Spec.Init == nil && !isCustom {
			errs = append(errs, "spec.cr is required (or spec.init for example packs, spec.custom.target for custom targets, or imports)")
		}
		if len(doc.Spec.Expect) == 0 {
			errs = append(errs, "spec.expect must contain at least one expectation")
		}
		var expandErr error
		expanded, expandErr = e2e.ExpandExpectIncludes(doc.Spec.Expect, baseDir)
		if expandErr != nil {
			errs = append(errs, expandErr.Error())
			expanded = doc.Spec.Expect
		}
		for i, exp := range expanded {
			if exp.Include != "" {
				continue
			}
			if exp.Name == "" {
				errs = append(errs, fmt.Sprintf("spec.expect[%d].name is required", i))
			}
			after := exp.After
			if after == "" {
				after = orktypes.AfterSetupComplete
			}
			validAfter := false
			for _, v := range orktypes.ValidAfterValues {
				if after == v {
					validAfter = true
					break
				}
			}
			if !validAfter {
				errs = append(errs, fmt.Sprintf("spec.expect[%d].after must be one of %v (got %q)", i, orktypes.ValidAfterValues, exp.After))
			}
			var kubectlCount int
			if k := exp.Kubectl; k != nil {
				kubectlCount = len(k.Get) + len(k.Logs) + len(k.Describe) + len(k.Exec) +
					len(k.PortForward) + len(k.Apply) + len(k.Delete) + len(k.Patch) +
					len(k.Events) + len(k.Auth) + len(k.Cp) + len(k.Top) +
					len(k.Restart) + len(k.Scale)
			}
			if len(exp.Resources) == 0 && len(exp.Commands) == 0 && kubectlCount == 0 {
				errs = append(errs, fmt.Sprintf("spec.expect[%d] (%q): must have at least one resource, command, or kubectl check", i, exp.Name))
			}
		}
	}

	// Validate imports (collect per-import errors for display).
	importErrs := e2e.ValidateImports(baseDir, doc.Imports)

	// Validate kubectl blocks.
	kubectlErrs := e2e.ValidateKubectl(expanded)

	if len(errs) > 0 || len(importErrs) > 0 || len(kubectlErrs) > 0 {
		for _, e := range errs {
			fmt.Printf("  %s %s\n", failureMark(), e)
		}
		for _, ie := range importErrs {
			fmt.Printf("  %s import: %s\n", failureMark(), ie)
		}
		for _, ke := range kubectlErrs {
			fmt.Printf("  %s %s\n", failureMark(), ke)
		}
		fmt.Println()
		return fmt.Errorf("%d validation error(s) in %s", len(errs)+len(importErrs)+len(kubectlErrs), path)
	}

	icon := healthIcon("ready")
	fmt.Printf("%s %s\n", icon, bold(doc.Metadata.Name))
	if doc.Metadata.Description != "" {
		fmt.Printf("    %s\n", gray(doc.Metadata.Description))
	}
	if !isAggregator {
		if isCustom {
			fmt.Printf("    %s\n", gray(fmt.Sprintf("mode    : custom target (%s — Orkestra install skipped)", doc.Spec.Custom.Target)))
		}
		if doc.Spec.Katalog != "" {
			fmt.Printf("    %s\n", gray("katalog : "+doc.Spec.Katalog))
		}
		if doc.Spec.CRD != "" {
			fmt.Printf("    %s\n", gray("crd     : "+doc.Spec.CRD))
		}
		if doc.Spec.CR != "" {
			fmt.Printf("    %s\n", gray("cr      : "+doc.Spec.CR))
		}
		if s := doc.Spec.Setup; s != nil {
			if len(s.Apply) > 0 {
				paths := make([]string, len(s.Apply))
				for i, e := range s.Apply {
					paths[i] = e.Path
				}
				fmt.Printf("    %s\n", gray("setup.apply : "+strings.Join(paths, ", ")))
			}
			if len(s.Helm) > 0 {
				fmt.Printf("    %s\n", gray(fmt.Sprintf("setup.helm  : %d chart(s)", len(s.Helm))))
			}
			if len(s.Wait) > 0 {
				fmt.Printf("    %s\n", gray(fmt.Sprintf("setup.wait  : %d resource(s)", len(s.Wait))))
			}
		}
	}
	if len(doc.Imports) > 0 {
		fmt.Printf("    %s\n", gray(fmt.Sprintf("imports : %d file(s)", len(doc.Imports))))
		for _, imp := range doc.Imports {
			label := imp.Path
			if imp.FreshCluster {
				label += " (fresh cluster)"
			}
			if imp.Wait != "" {
				label += " (wait: " + imp.Wait + ")"
			}
			fmt.Printf("      %s %s\n", healthIcon("ready"), gray(label))
		}
	}
	fmt.Println()
	for _, exp := range expanded {
		to := exp.Timeout
		if to == "" {
			to = "60s"
		}
		after := exp.After
		if after == "" {
			after = orktypes.AfterSetupComplete
		}
		fmt.Printf("    %s\n",
			gray(fmt.Sprintf("%-40s after: %-12s timeout: %s", exp.Name, after, to)))
	}
	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))
	if isAggregator {
		fmt.Printf("%d import(s) valid\n", len(doc.Imports))
	} else {
		fmt.Printf("%d expectation(s) valid", len(expanded))
		if len(doc.Imports) > 0 {
			fmt.Printf(", %d import(s) valid", len(doc.Imports))
		}
		fmt.Println()
	}

	return nil
}

// validateSimulateFile validates a Simulate spec file and prints a summary.
func validateSimulateFile(path string) error {
	return validateSimulateFileOpts(path, false, false)
}

func validateSimulateFilePlay(path string, play bool) error {
	return validateSimulateFileOpts(path, false, play)
}

func validateSimulateFileQuiet(path string) error {
	return validateSimulateFileOpts(path, true, false)
}

func validateSimulateFileOpts(path string, quiet, playMode bool) error {
	if !quiet {
		fmt.Println()
		fmt.Println(bold("Validating Simulate..."))
		fmt.Println()
	}

	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var doc orktypes.Simulate
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	baseDir := filepath.Dir(path)
	isAggregator := len(doc.Imports) > 0 && doc.Spec == nil

	var errs []string

	if doc.Metadata.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if !isAggregator && doc.Spec == nil {
		errs = append(errs, "spec or imports is required")
	}
	if isAggregator {
		for _, f := range doc.Imports {
			p := f
			if !filepath.IsAbs(p) {
				p = filepath.Join(baseDir, p)
			}
			if !fileExists(p) {
				errs = append(errs, "import not found: "+f)
			}
		}
	}
	if doc.Spec != nil {
		if doc.Spec.Katalog == "" {
			errs = append(errs, "spec.katalog is required")
		} else if !fileExists(filepath.Join(baseDir, doc.Spec.Katalog)) {
			errs = append(errs, "spec.katalog not found: "+doc.Spec.Katalog)
		}
		if len(doc.Spec.AllCRPaths()) == 0 {
			if !playMode {
				errs = append(errs, "spec.cr or spec.crFiles is required (or use --play if ork serve play will supply the CR)")
			}
		} else {
			for _, p := range doc.Spec.AllCRPaths() {
				if !fileExists(filepath.Join(baseDir, p)) {
					errs = append(errs, "cr file not found: "+p)
				}
			}
		}
		for _, p := range doc.Spec.AllCRDPaths() {
			if !fileExists(filepath.Join(baseDir, p)) {
				errs = append(errs, "crd file not found: "+p)
			}
		}
		if doc.Spec.Expect != nil {
			if expandErr := orktypes.ExpandSimulateOpsIncludes(doc.Spec.Expect, baseDir); expandErr != nil {
				errs = append(errs, "expanding ops includes: "+expandErr.Error())
			}
			validVerbs := map[string]bool{
				"create": true,
				"apply":  true,
				"update": true,
				"delete": true,
				"patch":  true,
			}
			validateOpRules := func(rules []orktypes.SimulateOpRule, prefix string) {
				for i, rule := range rules {
					switch {
					case rule.Verb == "" || rule.Resource == "":
						errs = append(errs, fmt.Sprintf("%s[%d]: verb and resource are required", prefix, i))
					case !validVerbs[rule.Verb]:
						errs = append(errs, fmt.Sprintf("%s[%d]: invalid verb %q (must be create, apply, update, delete, or patch)", prefix, i, rule.Verb))
					}
				}
			}
			validateAbsentRules := func(rules []orktypes.SimulateOpRule, prefix string) {
				for i, rule := range rules {
					switch {
					case rule.Resource == "":
						errs = append(errs, fmt.Sprintf("%s[%d]: resource is required", prefix, i))
					case rule.Verb != "" && !validVerbs[rule.Verb]:
						errs = append(errs, fmt.Sprintf("%s[%d]: invalid verb %q (must be create, apply, update, delete, or patch)", prefix, i, rule.Verb))
					}
				}
			}
			validateOpRules(doc.Spec.Expect.Ops, "expect.ops")
			validateAbsentRules(doc.Spec.Expect.Absent, "expect.absent")
			for crdName, sub := range doc.Spec.Expect.CRDs {
				if sub == nil {
					continue
				}
				validateOpRules(sub.Ops, fmt.Sprintf("expect.crds[%s].ops", crdName))
				validateAbsentRules(sub.Absent, fmt.Sprintf("expect.crds[%s].absent", crdName))
			}
		}
	}

	if len(errs) > 0 {
		if !quiet {
			for _, e := range errs {
				fmt.Printf("  %s %s\n", failureMark(), e)
			}
			fmt.Println()
			fmt.Println(strings.Repeat("─", 60))
		}
		return fmt.Errorf("%d validation error(s) in %s", len(errs), path)
	}

	if quiet {
		return nil
	}

	// Success — print structured summary matching the Katalog/E2E style.
	fmt.Printf("%s %s\n", healthIcon("ready"), bold(doc.Metadata.Name))
	if doc.Metadata.Description != "" {
		fmt.Printf("    %s\n", gray(doc.Metadata.Description))
	}
	fmt.Println()

	if isAggregator {
		fmt.Printf("    %s\n", gray(fmt.Sprintf("imports : %d file(s)", len(doc.Imports))))
		for _, f := range doc.Imports {
			fmt.Printf("      %s %s\n", healthIcon("ready"), gray(f))
		}
	} else {
		cycles := doc.Spec.Cycles
		if cycles <= 0 {
			cycles = 10
		}
		fmt.Printf("    %s\n", gray(fmt.Sprintf("katalog : %s", doc.Spec.Katalog)))
		fmt.Printf("    %s\n", gray(fmt.Sprintf("cr      : %s", doc.Spec.CR)))
		fmt.Printf("    %s\n", gray(fmt.Sprintf("cycles  : %d", cycles)))
		if doc.Spec.Expect != nil {
			totalOps := len(doc.Spec.Expect.Ops)
			totalAbsent := len(doc.Spec.Expect.Absent)
			for _, sub := range doc.Spec.Expect.CRDs {
				if sub == nil {
					continue
				}
				totalOps += len(sub.Ops)
				totalAbsent += len(sub.Absent)
			}
			fmt.Printf("    %s\n", gray(fmt.Sprintf("ops     : %d rule(s)", totalOps)))
			if totalAbsent > 0 {
				fmt.Printf("    %s\n", gray(fmt.Sprintf("absent  : %d rule(s)", totalAbsent)))
			}
			if len(doc.Spec.Expect.CRDs) > 0 {
				fmt.Printf("    %s\n", gray(fmt.Sprintf("crds    : %d scoped", len(doc.Spec.Expect.CRDs))))
			}
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))
	if isAggregator {
		fmt.Printf("%d import(s) valid\n", len(doc.Imports))
	} else {
		ops := 0
		if doc.Spec.Expect != nil {
			ops = len(doc.Spec.Expect.Ops)
			for _, sub := range doc.Spec.Expect.CRDs {
				if sub != nil {
					ops += len(sub.Ops)
				}
			}
		}
		fmt.Printf("Simulate is valid (%d op rule(s))\n", ops)
	}
	return nil
}

// validateMotifFile runs Motif-specific validation and prints results.
func validateMotifFile(path string) error {
	fmt.Println()
	fmt.Println(bold("Validating Motif..."))
	fmt.Println()

	errs := katalog.ValidateMotif(path)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Printf("  %s %s\n", failureMark(), e.Error())
		}
		fmt.Println()
		fmt.Println(strings.Repeat("─", 60))
		return fmt.Errorf("%d validation error(s) in %s", len(errs), path)
	}

	// Load the motif to build the structured summary.
	m, err := motifpkg.Load(path)
	if err != nil {
		// ValidateMotif already passed, so this is unexpected — degrade gracefully.
		fmt.Printf("%s %s\n", healthIcon("ready"), bold(path))
	} else {
		fmt.Printf("%s %s\n", healthIcon("ready"), bold(m.Metadata.Name))
		if m.Metadata.Description != "" {
			fmt.Printf("    %s\n", gray(m.Metadata.Description))
		}
		if m.Metadata.Version != "" {
			fmt.Println()
			fmt.Printf("    %s\n", gray("version : "+m.Metadata.Version))
		}
		if len(m.Inputs) > 0 {
			if m.Metadata.Version == "" {
				fmt.Println()
			}
			fmt.Printf("    %s\n", gray(fmt.Sprintf("inputs  : %d", len(m.Inputs))))
		}
		if summary := motifResourceSummary(m); summary != "" {
			fmt.Printf("    %s\n", gray("resources: "+summary))
		}
		if summary := motifProfileSummary(m); summary != "" {
			fmt.Printf("    %s\n", gray("profiles : "+summary))
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println("Motif is valid")
	return nil
}

// motifProfileSummary returns a compact string listing non-empty profile classes
// and their counts, e.g. "networkPolicies(2) resourceQuotas(1)".
func motifProfileSummary(m *orktypes.Motif) string {
	reg := m.Profiles
	if reg.Empty() {
		return ""
	}
	var parts []string
	add := func(kind string, n int) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%s(%d)", kind, n))
		}
	}
	add("networkPolicies", len(reg.NetworkPolicies))
	add("resourceQuotas", len(reg.ResourceQuotas))
	add("limitRanges", len(reg.LimitRanges))
	add("hpa", len(reg.HPA))
	add("pdb", len(reg.PDB))
	add("rollingUpdate", len(reg.RollingUpdate))
	return strings.Join(parts, " ")
}

// motifResourceSummary returns a compact string listing non-empty resource types
// and their counts, e.g. "deployments(1) services(1) networkPolicies(2)".
func motifResourceSummary(m *orktypes.Motif) string {
	if m.Resources == nil {
		return ""
	}
	ht := m.Resources.HookTemplates
	var parts []string
	add := func(kind string, n int) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%s(%d)", kind, n))
		}
	}
	add("deployments", len(ht.Deployments))
	add("statefulSets", len(ht.StatefulSets))
	add("daemonSets", len(ht.DaemonSets))
	add("services", len(ht.Services))
	add("ingresses", len(ht.Ingresses))
	add("networkPolicies", len(ht.NetworkPolicies))
	add("jobs", len(ht.Jobs))
	add("cronJobs", len(ht.CronJobs))
	add("secrets", len(ht.Secrets))
	add("configMaps", len(ht.ConfigMaps))
	add("serviceAccounts", len(ht.ServiceAccounts))
	add("roles", len(ht.Roles))
	add("roleBindings", len(ht.RoleBindings))
	add("clusterRoles", len(ht.ClusterRoles))
	add("clusterRoleBindings", len(ht.ClusterRoleBindings))
	add("resourceQuotas", len(ht.ResourceQuotas))
	add("limitRanges", len(ht.LimitRanges))
	add("namespaces", len(ht.Namespaces))
	add("persistentVolumeClaims", len(ht.PersistentVolumeClaims))
	add("horizontalPodAutoscalers", len(ht.HorizontalPodAutoscalers))
	add("podDisruptionBudgets", len(ht.PodDisruptionBudgets))
	return strings.Join(parts, " ")
}

func printValidateProfiles(reg orktypes.ProfileRegistry) {
	if reg.Empty() {
		return
	}
	type entry struct{ kind, name string }
	var entries []entry
	for _, p := range reg.Resources {
		entries = append(entries, entry{"resources", p.Name})
	}
	for _, p := range reg.ContainerSecurity {
		entries = append(entries, entry{"containerSecurity", p.Name})
	}
	for _, p := range reg.PodSecurity {
		entries = append(entries, entry{"podSecurity", p.Name})
	}
	for _, p := range reg.HPA {
		entries = append(entries, entry{"hpa", p.Name})
	}
	for _, p := range reg.PDB {
		entries = append(entries, entry{"pdb", p.Name})
	}
	for _, p := range reg.RollingUpdate {
		entries = append(entries, entry{"rollingUpdate", p.Name})
	}
	for _, p := range reg.Reconciler {
		entries = append(entries, entry{"reconciler", p.Name})
	}
	for _, p := range reg.NetworkPolicies {
		entries = append(entries, entry{"networkPolicies", p.Name})
	}
	for _, p := range reg.ResourceQuotas {
		entries = append(entries, entry{"resourceQuotas", p.Name})
	}
	for _, p := range reg.LimitRanges {
		entries = append(entries, entry{"limitRanges", p.Name})
	}
	for _, p := range reg.Probes {
		entries = append(entries, entry{"probes", p.Name})
	}
	maxLen := 0
	for _, e := range entries {
		if l := len(e.kind); l > maxLen {
			maxLen = l
		}
	}
	fmt.Println()
	fmt.Printf("%s\n", bold(fmt.Sprintf("Profiles (%d)", len(entries))))
	for _, e := range entries {
		fmt.Printf("  %s   %s\n", padRight(cyan(e.kind), maxLen), gray(e.name))
	}
}

func printValidateNotes(reg orktypes.NoteRegistry) {
	if reg.Empty() {
		return
	}
	maxLen := 0
	for _, n := range reg.Functions {
		if l := len(n.Name); l > maxLen {
			maxLen = l
		}
	}
	fmt.Println()
	fmt.Printf("%s\n", bold(fmt.Sprintf("Notes (%d)", len(reg.Functions))))
	for _, n := range reg.Functions {
		fmt.Printf("  %s   %s\n", padRight(cyan(n.Name), maxLen), gray(n.Expression))
	}
}

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().StringSliceP("file", "f", nil, "Path to an Orkestra pattern (repeatable or comma-separated)")
	validateCmd.Flags().Bool("full", false, "Show per-CRD permissions, dependency graph, and system-level RBAC")
	validateCmd.Flags().Bool("notes", false, "Quiet mode: print only the merged note registry, skip full validate output")
	validateCmd.Flags().Bool("profiles", false, "Quiet mode: print only the merged profile registry, skip full validate output")
	validateCmd.Flags().Bool("play", false, "For Simulate specs: skip spec.cr requirement (ork serve play will supply the CR)")

	// Shadow global flags so they don't appear under `ork validate`
	shadowGlobalCommandFlags(validateCmd)
}
