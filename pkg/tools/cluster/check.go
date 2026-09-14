package cluster

import (
	"fmt"
	"path/filepath"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"gopkg.in/yaml.v3"
)

// clustersFile is the on-disk shape emitted by bootstrap --out and consumed
// by clusters check --config.
type clustersFile struct {
	Clusters map[string]orktypes.GatewayClusterConfig `yaml:"clusters"`
}

// LoadClustersFile reads a clusters.yaml (or .json) file and returns the
// map of cluster name → GatewayClusterConfig. The file format is identical
// to the gateway.clusters include file used by the katalog.
func LoadClustersFile(path string) (map[string]orktypes.GatewayClusterConfig, error) {
	data, err := readLocal(path)
	if err != nil {
		return nil, fmt.Errorf("reading clusters file: %w", err)
	}
	var f clustersFile
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		// yaml.Unmarshal handles JSON too, but be explicit for clarity.
		if err := yaml.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing clusters file: %w", err)
		}
	default:
		if err := yaml.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing clusters file: %w", err)
		}
	}
	if len(f.Clusters) == 0 {
		return nil, fmt.Errorf("clusters file %q: no clusters entries found", path)
	}
	return f.Clusters, nil
}
