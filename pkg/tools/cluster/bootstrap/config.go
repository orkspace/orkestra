package bootstrap

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	rbacv1 "k8s.io/api/rbac/v1"
)

// ClusterEntry describes a single cluster to bootstrap.
// It maps directly to one entry in the clusters: list of a config file.
type ClusterEntry struct {
	// Name is the logical cluster name used in gateway.clusters and Secret names (required).
	Name string `yaml:"name" json:"name"`
	// Context is the kubeconfig context for the target cluster (required).
	Context string `yaml:"context" json:"context"`
	// SANamespace is the namespace on the target cluster for the SA and token Secret.
	// Defaults to DefaultSANamespace ("kube-system") when empty.
	SANamespace string `yaml:"sa-namespace" json:"sa-namespace"`
	// SAName overrides the ServiceAccount name on the target cluster.
	// Defaults to DefaultSAName ("orkestra-gateway") when empty.
	SAName string `yaml:"sa-name" json:"sa-name"`
	// Rules are the ClusterRole rules to apply on the target cluster.
	// Used in the generic (non-Orkestra) path. When absent, ClusterRole and
	// ClusterRoleBinding are not created — only the SA and token are provisioned.
	Rules []rbacv1.PolicyRule `yaml:"rules,omitempty" json:"rules,omitempty"`
}

// ConfigFile is the structure of a cluster bootstrap config file.
//
//	clusters:
//	  - name: staging
//	    context: kind-ork-multi-2
//	  - name: prod
//	    context: kind-ork-multi-3
//	    sa-namespace: restricted-ns
type ConfigFile struct {
	Clusters []ClusterEntry `yaml:"clusters" json:"clusters"`
}

// LoadConfig reads a YAML or JSON cluster config file and applies defaults.
// It does not validate required fields — call ValidateConfig for that.
func LoadConfig(path string) (*ConfigFile, error) {
	data, err := readLocal(path)
	if err != nil {
		return nil, fmt.Errorf("reading bootstrap config: %w", err)
	}
	var cfg ConfigFile
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing bootstrap config: %w", err)
		}
	default:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing bootstrap config: %w", err)
		}
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

// ValidateConfig checks that all required fields are present and names are unique.
// It also applies defaults so callers always receive a fully-populated config.
func ValidateConfig(cfg *ConfigFile) error {
	if len(cfg.Clusters) == 0 {
		return fmt.Errorf("bootstrap config: clusters list is empty")
	}
	seen := make(map[string]bool, len(cfg.Clusters))
	var errs []string
	for i, e := range cfg.Clusters {
		prefix := fmt.Sprintf("clusters[%d]", i)
		if strings.TrimSpace(e.Name) == "" {
			errs = append(errs, prefix+": name is required")
		} else if seen[e.Name] {
			errs = append(errs, prefix+fmt.Sprintf(": duplicate name %q", e.Name))
		} else {
			seen[e.Name] = true
		}
		if strings.TrimSpace(e.Context) == "" {
			errs = append(errs, prefix+": context is required")
		}
		for j, rule := range e.Rules {
			for _, verb := range rule.Verbs {
				if !KnownVerbs[verb] {
					errs = append(errs, fmt.Sprintf("%s.rules[%d]: unknown verb %q", prefix, j, verb))
				}
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("bootstrap config invalid:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// applyDefaults sets default values for any unspecified fields in the configuration.
// For each cluster, if SANamespace is empty, it assigns the default "kube-system" namespace.
func applyDefaults(cfg *ConfigFile) {
	for i := range cfg.Clusters {
		if cfg.Clusters[i].SANamespace == "" {
			cfg.Clusters[i].SANamespace = DefaultSANamespace
		}
	}
}
