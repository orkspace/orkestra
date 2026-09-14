//go:build !runtime && !gateway

package cli

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/pipeline"
	"github.com/orkspace/orkestra/pkg/merger"
	"github.com/orkspace/orkestra/pkg/tools/generate"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
)

// bundleOptsFromFor reads the --for flag and returns the corresponding BundleOptions.
// --for accepts a comma-separated list of component names: runtime (alias: run),
// gateway (alias: gw), cc (aliases: controlcenter, control-center).
// When --for is absent or empty, all three components are included (default).
func bundleOptsFromFor(cmd *cobra.Command) (generate.BundleOptions, error) {
	forVal, _ := cmd.Flags().GetString("for")
	if forVal == "" {
		return generate.DefaultBundleOptions(), nil
	}
	opts := generate.BundleOptions{}
	var unknown []string
	for _, part := range strings.Split(forVal, ",") {
		name := strings.TrimSpace(strings.ToLower(part))
		if name == "" {
			continue
		}
		switch name {
		case "run", "runtime":
			opts.IncludeRuntime = true
		case "gw", "gateway":
			opts.IncludeGateway = true
		case "cc", "controlcenter", "control-center":
			opts.IncludeControlCenter = true
		default:
			unknown = append(unknown, part)
		}
	}
	if len(unknown) > 0 {
		return generate.BundleOptions{}, fmt.Errorf(
			"orkestra: unknown --for value(s): %s\n\nValid values are:\n"+
				"  runtime   (alias: run)          — reconcilers, leader election\n"+
				"  gateway   (alias: gw)            — TLS, admission webhooks\n"+
				"  cc        (alias: controlcenter) — control-center\n\n"+
				"Example: --for gateway\n"+
				"         --for runtime,cc",
			strings.Join(unknown, ", "),
		)
	}
	if !opts.IncludeRuntime && !opts.IncludeGateway && !opts.IncludeControlCenter {
		return generate.BundleOptions{}, fmt.Errorf(
			"orkestra: --for produced an empty component list; nothing to generate\n\n" +
				"Valid values are: runtime (run), gateway (gw), cc (controlcenter, control-center)",
		)
	}
	return opts, nil
}

// defaultNamespace returns the namespace to use when --namespace is not supplied.
// Reads ORK_NAMESPACE from the environment so that CLI invocations inside
// an already-configured cluster automatically target the right namespace.
func defaultNamespace() string {
	if ns := os.Getenv("ORK_NAMESPACE"); ns != "" {
		return ns
	}
	return "orkestra-system"
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate Orkestra components",
}

// parseFilePaths handles comma-separated values and returns a slice of paths
func parseFilePaths(paths []string) []string {
	var expanded []string
	for _, p := range paths {
		// Split by comma and trim spaces
		parts := strings.Split(p, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				expanded = append(expanded, trimmed)
			}
		}
	}
	return expanded
}

// rejectCRDFile returns an error if any CRD used a local crdFile shortcut.
// Pass k.WithCRDFiles() as names and k.Enabled() as resolved — both available
// after pipeline.BuildExpanded, which records crdFile names before clearing the field.
func rejectCRDFile(names []string, resolved map[string]orktypes.CRDEntry) error {
	for _, name := range names {
		crd := resolved[name]
		g := orDefault(crd.APITypes.Group, "<group>")
		v := orDefault(crd.APITypes.Version, "<version>")
		k := orDefault(crd.APITypes.Kind, "<Kind>")
		p := orDefault(crd.APITypes.Plural, "<plural>")
		return fmt.Errorf(
			"CRD %q uses crdFile, which is a local development shortcut.\n\n"+
				"bundle, configmap, and rbac are production artifacts — the file will not\n"+
				"be available at runtime. Replace crdFile with explicit apiTypes:\n\n"+
				"  spec:\n"+
				"    crds:\n"+
				"      %s:\n"+
				"        apiTypes:\n"+
				"          group: %s\n"+
				"          version: %s\n"+
				"          kind: %s\n"+
				"          plural: %s",
			name, name, g, v, k, p,
		)
	}
	return nil
}

func orDefault(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

type mergerOut struct {
	m       *merger.Merger
	crds    []orktypes.CRDEntry
	kat     *katalog.Katalog
	paths   []string
	enabled map[string]orktypes.CRDEntry // m.Enabled()
}

func generateKatalog(cmd *cobra.Command) (*mergerOut, error) {
	katalogPaths, _ := cmd.Flags().GetStringSlice("file")

	expanded := parseFilePaths(katalogPaths)
	if len(expanded) == 0 {
		expanded = defaultFilePaths()
	}
	if len(expanded) == 0 {
		return nil, fmt.Errorf(errNoKatalog)
	}

	// Absolutize so merger.FirstEntryDir() is always absolute. Without this,
	// relative paths cause a double-join when populateAPITypesFromCRDFile
	// prepends katalogDir to a crdFile that was already joined once during load.
	for i, p := range expanded {
		if abs, err := filepath.Abs(p); err == nil {
			expanded[i] = abs
		}
	}

	m := merger.New(expanded...)
	if err := m.Merge(); err != nil {
		return nil, err
	}

	var kat katalog.Katalog
	kat.Spec = m.ToSpec()

	// Convert map to slice for generate functions
	crdMap := m.ToSpec().CRDs
	crds := make([]orktypes.CRDEntry, 0, len(crdMap))
	for _, c := range crdMap {
		crds = append(crds, c)
	}

	return &mergerOut{
		m:       m,
		crds:    crds,
		kat:     &kat,
		paths:   katalogPaths,
		enabled: m.Enabled(),
	}, nil
}

// buildKatalog builds the expanded Katalog using the --file flag from cmd.
func buildKatalog(cmd *cobra.Command) (*katalog.Katalog, error) {
	m, err := generateKatalog(cmd)
	if err != nil {
		return nil, fmt.Errorf("generating Katalog: %w", err)
	}
	return pipeline.BuildExpanded(kfg, m.m)
}

// buildKatalogFromPath builds an expanded Katalog from a file path without
// requiring a cobra command context. Useful when the path is already known.
func buildKatalogFromPath(path string) (*katalog.Katalog, error) {
	m := merger.New(path)
	if err := m.Merge(); err != nil {
		return nil, fmt.Errorf("merging katalog: %w", err)
	}
	return pipeline.BuildExpanded(kfg, m)
}

var generateDashboardsCmd = &cobra.Command{
	Use:   "dashboards",
	Short: "Generate Grafana dashboards for all CRDs",
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := generateKatalog(cmd)
		if err != nil {
			return err
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")

		log.Println("generating dashboards...")
		log.Printf("dry-run: %t\n", dryRun)

		if err := generate.Dashboards(out.crds, dryRun); err != nil {
			return fmt.Errorf("generate dashboards: %w", err)
		}

		log.Printf("dashboards generated successfully\n")
		log.Printf("out: %s\n", generate.DashDir)
		return nil
	},
}

var generateAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Generate runtime, docs, dashboards, examples, tests, and graphs",
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := generateKatalog(cmd)
		if err != nil {
			return fmt.Errorf("merge katalogs: %w", err)
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")

		log.Println("running all generators...")

		if _, err := generate.TypeRegistry(out.enabled, dryRun); err != nil {
			return fmt.Errorf("generate runtime: %w", err)
		}
		if err := generate.Dashboards(out.crds, dryRun); err != nil {
			return fmt.Errorf("generate dashboards: %w", err)
		}
		log.Println("all generators completed successfully")
		return nil
	},
}

var generateRbacCmd = &cobra.Command{
	Use:   "rbac",
	Short: "Generate RBAC ClusterRoles and ServiceAccounts for Orkestra components",
	Long: `Reads one or more katalog.yaml files, merges them, and generates minimal
ClusterRoles for the runtime and gateway processes, plus ServiceAccounts for
all three components (runtime, gateway, control center).

Use --for to limit the output to specific components. By default all three
are included. Multiple values are comma-separated.

Examples:
  ork generate rbac -f katalog.yaml
  ork generate rbac -f katalog.yaml --for gateway
  ork generate rbac -f katalog.yaml --for runtime,cc
  ork generate rbac -f a.yaml,b.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := generateKatalog(cmd)
		if err != nil {
			return err
		}
		namespace, _ := cmd.Flags().GetString("namespace")
		outputFile, _ := cmd.Flags().GetString("output")

		k, err := pipeline.BuildExpanded(kfg, out.m)
		if err != nil {
			return fmt.Errorf("build katalog: %w", err)
		}
		if err := rejectCRDFile(k.WithCRDFiles(), k.Enabled()); err != nil {
			return err
		}

		log.Println("generating rbac...")

		opts, err := bundleOptsFromFor(cmd)
		if err != nil {
			return err
		}

		if !k.IsGatewayEnabled() {
			opts.IncludeGateway = false
		}
		runtimeRules := k.GenerateRuntimeRBACRules()
		gatewayRules := k.GenerateGatewayRBACRules()

		output, err := generate.RBACWithOptions(runtimeRules, gatewayRules, opts, namespace, outputFile)
		if err != nil {
			return fmt.Errorf("generate rbac: %w", err)
		}

		if err := writeOutput(outputFile, "rbac.yaml", []byte(output)); err != nil {
			return err
		}

		return writeClusterRBACFiles(k, outputFile)
	},
}

var generateConfigMapCmd = &cobra.Command{
	Use:   "configmap",
	Short: "Generate a ConfigMap embedding a Katalog or Komposer",
	Long: `Reads a katalog.yaml or komposer.yaml file and produces a ConfigMap
that embeds the file under data:<filename>. Useful for injecting Katalogs
into the in-cluster Orkestra runtime.

Example:
  ork generate configmap -f katalog.yaml
  ork generate configmap -f komposer.yaml -n orkestra-system -o out.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := generateKatalog(cmd)
		if err != nil {
			return err
		}

		namespace, _ := cmd.Flags().GetString("namespace")
		outputFile, _ := cmd.Flags().GetString("output")

		k, err := pipeline.BuildExpanded(kfg, out.m)
		if err != nil {
			return fmt.Errorf("build katalog: %w", err)
		}
		if err := rejectCRDFile(k.WithCRDFiles(), k.Enabled()); err != nil {
			return err
		}

		log.Println("generating configmap...")

		expanded, err := k.SerializeExpanded()
		if err != nil {
			return fmt.Errorf("serialize katalog: %w", err)
		}

		cm, err := generate.ConfigMap(expanded, namespace)
		if err != nil {
			return fmt.Errorf("generate configmap: %w", err)
		}

		return writeOutput(outputFile, "config.yaml", cm)
	},
}

var generateBundleCmd = &cobra.Command{
	Use:   "bundle",
	Short: "Generate a complete installation bundle (RBAC + ConfigMap)",
	Long: `Generates a complete Orkestra installation bundle containing:
  • Namespace (default: 'orkestra-system')
  • ServiceAccounts for runtime, gateway, and control center
  • ClusterRoles and ClusterRoleBindings (one per process, minimal permissions)
  • ConfigMap embedding your Katalog

Use --for to limit the output to specific components. By default all three
are included. Multiple values are comma-separated.

Examples:
  ork generate bundle -f katalog.yaml
  ork generate bundle -f katalog.yaml --for gateway
  ork generate bundle -f katalog.yaml --for runtime,cc
  ork generate bundle -f katalog.yaml -o bundle.yaml -n orkestra-system`,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := generateKatalog(cmd)
		if err != nil {
			return err
		}

		namespace, _ := cmd.Flags().GetString("namespace")
		workloadNamespace, _ := cmd.Flags().GetString("workload-namespace")
		outputFile, _ := cmd.Flags().GetString("output")

		k, err := pipeline.BuildExpanded(kfg, out.m)
		if err != nil {
			return fmt.Errorf("build katalog: %w", err)
		}
		if err := rejectCRDFile(k.WithCRDFiles(), k.Enabled()); err != nil {
			return err
		}

		opts, err := bundleOptsFromFor(cmd)
		if err != nil {
			return err
		}

		log.Println("generating bundle...")

		if !k.IsGatewayEnabled() {
			opts.IncludeGateway = false
		}
		runtimeRules := k.GenerateRuntimeRBACRules()
		gatewayRules := k.GenerateGatewayRBACRules()

		expanded, err := k.SerializeExpanded()
		if err != nil {
			return fmt.Errorf("serialize katalog: %w", err)
		}

		bundle, err := generate.RenderBundle(runtimeRules, gatewayRules, expanded, namespace, workloadNamespace, opts)
		if err != nil {
			return fmt.Errorf("generate bundle: %w", err)
		}

		if err := writeOutput(outputFile, "bundle.yaml", []byte(bundle)); err != nil {
			return err
		}

		return writeClusterRBACFiles(k, outputFile)
	},
}

// writeClusterRBACFiles generates a gateway-<name>-rbac.yaml for each remote cluster
// that has serve-enabled CRDs routed to it. Files land in the same directory as
// outputFile (or the current directory when outputFile is empty or "-").
// Template-routed CRDs appear in every cluster file; a warning is printed for those.
func writeClusterRBACFiles(k *katalog.Katalog, outputFile string) error {
	clusterRules, templateKinds := k.GenerateGatewayClusterRBACRules()
	if len(clusterRules) == 0 {
		return nil
	}
	if outputFile == "-" {
		return nil // stdout mode — no path to place cluster files alongside
	}

	dir := clusterOutputDir(outputFile)
	clusters := k.GatewayClusters()

	if len(templateKinds) > 0 {
		crdTxt := "CRDs"
		if len(templateKinds) == 1 {
			crdTxt = "CRD"
		}

		fmt.Fprintf(os.Stderr,
			"\n%s warning: template-routed %s added to all cluster RBAC files: %s\n"+
				"  Remove rules for clusters that should not have access.\n",
			warningMark(), crdTxt, strings.Join(templateKinds, ", "),
		)
	}

	fmt.Println()
	for _, name := range sortedKeys(clusterRules) {
		b, err := generate.RBACForCluster(name, clusterRules[name], "kube-system")
		if err != nil {
			return fmt.Errorf("generate cluster rbac [%s]: %w", name, err)
		}
		filename := "gateway-" + name + "-rbac.yaml"
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, b, 0644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		log.Printf("%s generated successfully\n", filename)
		if cfg, ok := clusters[name]; ok {
			fmt.Printf("  Apply to cluster %q (%s):\n    kubectl apply -f %s\n\n", name, cfg.EndpointURL(), path)
		}
	}
	return nil
}

// clusterOutputDir returns the directory in which per-cluster RBAC files should
// be written, mirroring the logic in writeOutput.
func clusterOutputDir(outputFile string) string {
	if outputFile == "" || outputFile == "-" {
		return "."
	}
	info, err := os.Stat(outputFile)
	if err == nil && info.IsDir() {
		return outputFile
	}
	return filepath.Dir(outputFile)
}

func init() {
	rootCmd.AddCommand(generateCmd)

	generateCmd.AddCommand(generateKatalogCmd)
	generateCmd.AddCommand(generateCRDCmd)
	generateCmd.AddCommand(generateRegistryCmd)
	generateCmd.AddCommand(generateDashboardsCmd)
	generateCmd.AddCommand(generateAllCmd)
	generateCmd.AddCommand(generateRbacCmd)
	generateCmd.AddCommand(generateConfigMapCmd)
	generateCmd.AddCommand(generateBundleCmd)

	// All three commands use StringSliceP so generateKatalog can read them uniformly.
	for _, cmd := range []*cobra.Command{generateConfigMapCmd, generateBundleCmd, generateRbacCmd} {
		cmd.Flags().StringSliceP("file", "f", []string{}, "Path to katalog.yaml or komposer.yaml (repeatable or comma-separated)")
	}

	generateRegistryCmd.Flags().StringP("dirs", "d", "", "Comma-separated list of project directories to generate registries for")
	generateRegistryCmd.Flags().Duration("fetch-timeout", 2*time.Minute, "Timeout to fetch Go hook or constructor from 'location'")

	// Shared flags for all file-consuming generate commands.
	for _, cmd := range []*cobra.Command{
		generateRegistryCmd,
		generateDashboardsCmd,
		generateAllCmd,
		generateRbacCmd,
		generateConfigMapCmd,
		generateBundleCmd,
	} {
		cmd.Flags().Bool("dry-run", false, "Print generated output to stdout without writing files")
		cmd.Flags().StringP("output", "o", "", "Write generated output to file")
		cmd.Flags().StringP("namespace", "n", defaultNamespace(), "Namespace for the ServiceAccount")
	}

	// bundle-only flags
	generateBundleCmd.Flags().StringP("workload-namespace", "w", "", "Namespace for Deployment workloads (used by ork doctor deploy)")

	// component-selection flag (shared by rbac and bundle)
	// --for runtime          → runtime SA + ClusterRole only
	// --for gateway          → gateway SA + ClusterRole only
	// --for runtime,gateway  → both, no CC SA
	// --for runtime,cc       → runtime + CC SA, no gateway
	// (absent)               → all three (default)
	for _, cmd := range []*cobra.Command{generateRbacCmd, generateBundleCmd} {
		cmd.Flags().String("for", "", "Limit output to specific components: runtime, gateway, cc (comma-separated; default: all)")
	}

	// Shadow global flags so they don't appear under `ork generate`
	shadowGlobalCommandFlags(generateCmd)
	cobra.MarkFlagRequired(generateCmd.Flags(), "katalog")
}
