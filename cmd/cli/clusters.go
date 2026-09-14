//go:build !runtime && !gateway

package cli

import (
	"context"
	"fmt"
	"strings"

	apigateway "github.com/orkspace/orkestra/pkg/gateway/api"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/validate"
	"github.com/orkspace/orkestra/pkg/tools/cluster"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
)

// ── ork clusters ──────────────────────────────────────────────────────────────

var clustersCmd = &cobra.Command{
	Use:   "clusters",
	Short: "List registered gateway clusters",
	Long: `List all clusters registered in gateway.clusters.

Shows the name, endpoint, and credential form for each cluster.
Reads the katalog from the current directory or --file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}
		return runClustersList(k)
	},
}

func runClustersList(k *katalog.Katalog) error {
	fmt.Println()

	if !k.IsGatewayEnabled() {
		fmt.Printf("  %s gateway is not enabled\n", gray("○"))
		fmt.Println()
		return nil
	}

	clusters := k.GatewayClusters()
	if len(clusters) == 0 {
		fmt.Printf("  %s gateway.clusters is empty — no remote clusters registered\n", gray("○"))
		fmt.Println()
		return nil
	}

	fmt.Printf("  gateway.clusters (%d registered)\n", len(clusters))
	fmt.Println()

	for _, name := range sortedKeys(clusters) {
		cfg := clusters[name]
		fmt.Printf("  %s  %s\n", cyan("→"), bold(name))
		fmt.Printf("     %s\n", gray(cfg.EndpointURL()))
		fmt.Printf("     %s\n", gray(credentialSummary(cfg)))
		fmt.Println()
	}
	return nil
}

// ── ork clusters validate ─────────────────────────────────────────────────────

var clustersValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate gateway.clusters configuration offline",
	Long: `Validate the gateway.clusters block in the katalog.

Reads the katalog and checks each cluster entry for structural validity:
endpoint required, exactly one credential form, required secret refs present.

Also verifies that every static serve.cluster and target.cluster reference
resolves to a registered cluster name. Template expressions are validated
against the full user-defined funcMap.

With --full, also reports which CRDs route to each cluster.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		full, _ := cmd.Flags().GetBool("full")

		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		return runClustersValidate(k, full)
	},
}

func runClustersValidate(k *katalog.Katalog, full bool) error {
	fmt.Println()
	fmt.Printf("%s  ork clusters validate\n", bold("⎈"))
	fmt.Println()

	if !k.IsGatewayEnabled() {
		fmt.Printf("  %s gateway is not enabled — no clusters to validate\n", gray("○"))
		fmt.Println()
		return nil
	}

	clusters := k.GatewayClusters()
	if len(clusters) == 0 {
		fmt.Printf("  %s gateway.clusters is empty — no remote clusters registered\n", gray("○"))
		fmt.Println()
		return nil
	}

	clusterRefs := buildClusterRefIndex(k)

	fmt.Printf("  gateway.clusters (%d registered)\n", len(clusters))
	fmt.Println()

	allOK := true
	for _, name := range sortedKeys(clusters) {
		cfg := clusters[name]
		if !printClusterValidation(name, cfg, clusterRefs[name], full) {
			allOK = false
		}
	}

	if err := validate.ValidateGatewayClusters(k); err != nil {
		fmt.Printf("  %s %s\n", failureMark(), red(err.Error()))
		allOK = false
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))
	if allOK {
		fmt.Printf("%s %d cluster(s) valid\n", successMark(), len(clusters))
	} else {
		fmt.Printf("%s cluster validation failed\n", failureMark())
		return fmt.Errorf("cluster validation failed")
	}
	fmt.Println()
	return nil
}

// ── ork clusters check ────────────────────────────────────────────────────────

var clustersCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Connect to each registered cluster and verify CRD presence",
	Long: `Go online: read each cluster's credential secret from the management
cluster, connect to the remote cluster, verify the katalog's CRDs are installed,
and report pass / unreachable / missing-CRD per cluster.

Uses the current kubectl context to reach the management cluster.
Pass --context <ctx> to use a specific context instead.

With --config <file>, skips the katalog entirely and checks connectivity only
for each cluster listed in the bootstrap config file. Use this to verify
contexts are reachable before bootstrapping them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		clusterCtx, _ := cmd.Flags().GetString("context")
		clustersRaw, _ := cmd.Flags().GetString("clusters")
		configPath, _ := cmd.Flags().GetString("config")

		if configPath != "" {
			clusters, err := cluster.LoadClustersFile(configPath)
			if err != nil {
				return err
			}
			return runClustersCheck(cmd.Context(), nil, clusters, clusterCtx)
		}

		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		all := k.GatewayClusters()
		if len(all) == 0 {
			fmt.Printf("  %s gateway.clusters is empty — nothing to check\n", gray("○"))
			return nil
		}

		// Filter to requested subset when --clusters is set.
		clusters := all
		if clustersRaw != "" {
			selected := map[string]orktypes.GatewayClusterConfig{}
			for _, name := range strings.Split(clustersRaw, ",") {
				name = strings.TrimSpace(name)
				cfg, ok := all[name]
				if !ok {
					return fmt.Errorf("%s cluster %q is not registered in gateway.clusters", failureMark(), name)
				}
				selected[name] = cfg
			}
			clusters = selected
		}

		return runClustersCheck(cmd.Context(), k, clusters, clusterCtx)
	},
}

func runClustersCheck(ctx context.Context, k *katalog.Katalog, clusters map[string]orktypes.GatewayClusterConfig, clusterCtx string) error {
	fmt.Println()
	fmt.Printf("%s  ork clusters check\n", bold("⎈"))
	fmt.Println()

	localKube, err := cluster.LocalClient(ctx, clusterCtx)
	if err != nil {
		return fmt.Errorf("%s management cluster: %w", failureMark(), err)
	}

	results := apigateway.CheckClusters(ctx, k, clusters, localKube, "default")

	anyFailed := false
	for _, r := range results {
		fmt.Printf("  %s  %s  %s\n", cyan("→"), bold(r.Name), gray(r.Endpoint))

		if r.CredErr != nil {
			fmt.Printf("     %s credentials: %s\n", failureMark(), red(r.CredErr.Error()))
			anyFailed = true
			fmt.Println()
			continue
		}
		fmt.Printf("     %s credentials: read ok\n", successMark())

		if r.ConnErr != nil {
			fmt.Printf("     %s connect: %s\n", failureMark(), red(r.ConnErr.Error()))
			anyFailed = true
			fmt.Println()
			continue
		}
		fmt.Printf("     %s connect: reachable\n", successMark())

		for _, crdName := range sortedKeys(r.CRDs) {
			if err := r.CRDs[crdName]; err != nil {
				fmt.Printf("     %s crd %s: %s\n", failureMark(), red(crdName), gray(err.Error()))
				anyFailed = true
			} else {
				fmt.Printf("     %s crd %s: installed\n", successMark(), crdName)
			}
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("─", 60))
	if anyFailed {
		fmt.Printf("%s one or more clusters failed\n", failureMark())
		return fmt.Errorf("one or more clusters failed checks")
	}
	fmt.Printf("%s all clusters reachable\n", successMark())
	fmt.Println()
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func printClusterValidation(name string, cfg orktypes.GatewayClusterConfig, refs []string, full bool) bool {
	fmt.Printf("  %s  %s\n", cyan("→"), bold(name))
	ok := true

	if cfg.EndpointURL() == "" {
		fmt.Printf("     %s endpoint: missing\n", failureMark())
		ok = false
	} else {
		fmt.Printf("     %s endpoint: %s\n", successMark(), gray(cfg.EndpointURL()))
	}

	switch cfg.CredentialForm() {
	case "kubeconfig":
		fmt.Printf("     %s credential: kubeconfig %s\n", successMark(),
			gray(fmt.Sprintf("(secretRef: %s[%s])", cfg.SecretRef.SecretName(), cfg.SecretRef.SecretKey())))
	case "token":
		if cfg.IsInsecure() {
			fmt.Printf("     %s credential: bearer token %s\n", successMark(),
				gray(fmt.Sprintf("(tokenRef: %s[%s], insecure)", cfg.TokenRef.SecretName(), cfg.TokenRef.SecretKey())))
		} else {
			fmt.Printf("     %s credential: bearer token + CA %s\n", successMark(),
				gray(fmt.Sprintf("(tokenRef: %s[%s], caRef: %s[%s])",
					cfg.TokenRef.SecretName(), cfg.TokenRef.SecretKey(),
					cfg.CARef.SecretName(), cfg.CARef.SecretKey())))
		}
	default:
		fmt.Printf("     %s credential: no credential form declared\n", failureMark())
		ok = false
	}

	if full {
		if len(refs) == 0 {
			fmt.Printf("     %s routes: %s\n", gray("○"), gray("not referenced by any CRD"))
		} else {
			fmt.Printf("     %s routes: %s\n", gray("○"), gray(strings.Join(refs, ", ")))
		}
	}

	fmt.Println()
	return ok
}

func credentialSummary(cfg orktypes.GatewayClusterConfig) string {
	switch cfg.CredentialForm() {
	case "kubeconfig":
		return fmt.Sprintf("kubeconfig  secretRef: %s[%s]", cfg.SecretRef.SecretName(), cfg.SecretRef.SecretKey())
	case "token":
		if cfg.IsInsecure() {
			return fmt.Sprintf("token (insecure)  tokenRef: %s[%s]", cfg.TokenRef.SecretName(), cfg.TokenRef.SecretKey())
		}
		return fmt.Sprintf("token + CA  tokenRef: %s[%s]", cfg.TokenRef.SecretName(), cfg.TokenRef.SecretKey())
	default:
		return "no credential declared"
	}
}

func buildClusterRefIndex(k *katalog.Katalog) map[string][]string {
	idx := map[string][]string{}
	for crdName, entry := range k.EnabledCRDs() {
		if entry.Serve == nil {
			continue
		}
		for _, v := range entry.Serve.Clusters {
			if !orktypes.IsTemplate(v) {
				idx[v] = append(idx[v], crdName+".serve.clusters")
			}
		}
		for targetName, target := range entry.Serve.Target.Entries {
			for _, v := range target.TargetClusters() {
				if !orktypes.IsTemplate(v) {
					idx[v] = append(idx[v], fmt.Sprintf("%s.serve.target.%s.clusters", crdName, targetName))
				}
			}
		}
	}
	return idx
}

func init() {
	clustersValidateCmd.Flags().Bool("full", false, "Show CRD routing references per cluster")
	clustersCheckCmd.Flags().String("context", "", "kubectl context for reading credential secrets (defaults to current context)")
	clustersCheckCmd.Flags().String("clusters", "", "comma-separated list of cluster names to check (default: all registered clusters)")
	clustersCheckCmd.Flags().String("config", "", "path to a cluster-config.yaml to check connectivity without a katalog")

	clustersCmd.AddCommand(clustersValidateCmd)
	clustersCmd.AddCommand(clustersCheckCmd)
	rootCmd.AddCommand(clustersCmd)
	shadowGlobalCommandFlags(clustersCmd)
}
