//go:build !runtime && !gateway

package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/tools/plan"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Show what would change if this Katalog were applied",
	Long: `Compares a local katalog.yaml against a deployed Katalog.

A source for the deployed Katalog must be given explicitly:

  --bundle  read the deployed Katalog from a local bundle YAML (no cluster needed)
  --cm      read the deployed Katalog from a cluster ConfigMap (requires cluster access)

Examples:
  ork plan --bundle bundle.yaml
  ork plan -f katalog.yaml --bundle bundle.yaml
  ork plan -f katalog.yaml --cm orkestra-katalog --namespace orkestra-system`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rawFile, _ := cmd.Flags().GetString("file")
		file, err := resolveKatalogFile(rawFile)
		if err != nil {
			return err
		}
		bundle, _ := cmd.Flags().GetString("bundle")
		cm, _ := cmd.Flags().GetString("cm")
		ns, _ := cmd.Flags().GetString("namespace")
		if bundle != "" {
			return runPlanFromBundle(file, bundle)
		}
		if cm == "" {
			return fmt.Errorf(
				"a source is required: use --bundle <file> to compare against a local bundle " +
					"(no cluster needed), or --cm <name> to read from a cluster ConfigMap\n\n" +
					"  ork plan --bundle bundle.yaml\n" +
					"  ork plan --cm orkestra-katalog --namespace orkestra-system",
			)
		}
		return runPlan(cmd.Context(), file, cm, ns)
	},
}

func runPlan(ctx context.Context, localPath, cmName, namespace string) error {
	if namespace == "" {
		namespace = "orkestra-system"
	}

	// Read local Katalog
	localData, err := readLocal(localPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", localPath, err)
	}

	// If --cm ends with .yaml or .yml, treat it as a local file instead of a cluster name.
	var deployedData string
	if strings.HasSuffix(cmName, ".yaml") || strings.HasSuffix(cmName, ".yml") {
		raw, rerr := readLocal(cmName)
		if rerr != nil {
			return fmt.Errorf("reading ConfigMap file %s: %w", cmName, rerr)
		}
		deployedData, err = configMapDataFromBundle(raw)
		if err != nil {
			return fmt.Errorf("reading ConfigMap data from %s: %w", cmName, err)
		}
	} else {
		// Read deployed Katalog from the cluster ConfigMap
		kubeClient, kerr := buildKubeClient()
		if kerr != nil {
			return fmt.Errorf("connecting to cluster: %w", kerr)
		}
		cm, cerr := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
		if cerr != nil {
			if k8serrors.IsNotFound(cerr) {
				fmt.Printf("No deployed Katalog found (%s/%s).\n", namespace, cmName)
				fmt.Printf("This Katalog would be applied fresh:\n\n")
				printKatalogSummary(localData)
				return nil
			}
			return fmt.Errorf("reading ConfigMap %s/%s: %w", namespace, cmName, cerr)
		}
		var ok bool
		deployedData, ok = cm.Data["katalog"]
		if !ok {
			for _, v := range cm.Data {
				deployedData = v
				break
			}
		}
	}

	if deployedData == "" {
		return fmt.Errorf("no Katalog data found in %s", cmName)
	}

	// Parse both
	local, err := katalog.ParseBytes(localData, ".")
	if err != nil {
		return fmt.Errorf("parsing local Katalog: %w", err)
	}
	deployed, err := katalog.ParseBytes([]byte(deployedData), ".")
	if err != nil {
		return fmt.Errorf("parsing deployed Katalog: %w", err)
	}

	// Compute and print diff
	diff := plan.ComputeKatalogDiff(deployed, local)
	if diff.Empty() {
		fmt.Println("No changes. Local Katalog matches deployed.")
		return nil
	}

	fmt.Printf("Changes to apply:\n\n")
	diff.Print()
	return nil
}

func runPlanFromBundle(localPath, bundlePath string) error {
	localData, err := readLocal(localPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", localPath, err)
	}

	bundleData, err := readLocal(bundlePath)
	if err != nil {
		return fmt.Errorf("reading bundle %s: %w", bundlePath, err)
	}

	deployedData, err := configMapDataFromBundle(bundleData)
	if err != nil {
		return fmt.Errorf("reading ConfigMap from bundle: %w", err)
	}
	if deployedData == "" {
		fmt.Printf("No deployed Katalog found in bundle %s.\n", bundlePath)
		fmt.Printf("This Katalog would be applied fresh:\n\n")
		printKatalogSummary(localData)
		return nil
	}

	local, err := katalog.ParseBytes(localData, ".")
	if err != nil {
		return fmt.Errorf("parsing local Katalog: %w", err)
	}
	deployed, err := katalog.ParseBytes([]byte(deployedData), ".")
	if err != nil {
		return fmt.Errorf("parsing deployed Katalog: %w", err)
	}

	diff := plan.ComputeKatalogDiff(deployed, local)
	if diff.Empty() {
		fmt.Println("No changes. Local Katalog matches deployed.")
		return nil
	}
	fmt.Printf("Changes to apply:\n\n")
	diff.Print()
	return nil
}

// configMapDataFromBundle scans a multi-document YAML bundle for a ConfigMap
// and returns the first value from its .data map.
func configMapDataFromBundle(bundle []byte) (string, error) {
	type manifest struct {
		Kind string            `yaml:"kind"`
		Data map[string]string `yaml:"data"`
	}

	for _, doc := range splitYAMLDocs(bundle) {
		var m manifest
		if err := yaml.Unmarshal(doc, &m); err != nil {
			continue
		}
		if m.Kind != "ConfigMap" || len(m.Data) == 0 {
			continue
		}
		// prefer a key named "katalog", otherwise take the first value
		if v, ok := m.Data["katalog"]; ok {
			return v, nil
		}
		for _, v := range m.Data {
			return v, nil
		}
	}
	return "", nil
}

// splitYAMLDocs splits a YAML byte slice on "---" document separators.
func splitYAMLDocs(data []byte) [][]byte {
	var docs [][]byte
	for _, doc := range strings.Split(string(data), "\n---") {
		trimmed := strings.TrimSpace(doc)
		if trimmed != "" {
			docs = append(docs, []byte(trimmed))
		}
	}
	return docs
}

func buildKubeClient() (kubernetes.Interface, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kfg != nil && kfg.Cluster().KubekonfigPath() != "" {
		loadingRules.ExplicitPath = kfg.Cluster().KubekonfigPath()
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func printKatalogSummary(data []byte) {
	k, err := katalog.ParseBytes(data, ".")
	if err != nil {
		fmt.Printf("  (could not parse Katalog: %v)\n", err)
		return
	}
	for _, name := range k.CRDNames() {
		entry, _ := k.CRDEntry(name)
		fmt.Printf("  + %s  (%s/%s)\n", name, entry.APITypes.Group, entry.APITypes.Kind)
	}
}

func init() {
	rootCmd.AddCommand(planCmd)

	planCmd.Flags().StringP("file", "f", "", "Path to local katalog.yaml")
	planCmd.Flags().StringP("bundle", "b", "", "Path to a bundle YAML file — reads the ConfigMap from it instead of the cluster")
	planCmd.Flags().String("cm", "", "ConfigMap name (or path to a local ConfigMap YAML) holding the deployed Katalog — requires cluster access")
	planCmd.Flags().StringP("namespace", "n", "orkestra-system", "Namespace of the deployed Katalog ConfigMap")

	// Shadow global flags so they don't appear under `ork plan`
	shadowGlobalCommandFlags(planCmd)
}
