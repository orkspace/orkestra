package types

import (
	"fmt"
	"path/filepath"
)

// ExpandGatewayClustersInclude resolves gateway.clusters.include by reading the
// referenced file (which must contain a top-level "clusters:" map keyed by cluster
// name), and merging it with the inline gateway.clusters entries.
// Inline entries win by cluster name. Cleared after expansion.
// The include path is resolved relative to baseDir.
func ExpandGatewayClustersInclude(gw *GatewayConfig, baseDir string) error {
	if gw == nil || gw.Clusters == nil || gw.Clusters.Include == "" {
		return nil
	}

	path := gw.Clusters.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading gateway.clusters.include %q: %w", gw.Clusters.Include, err)
	}

	var f struct {
		Clusters map[string]GatewayClusterConfig `yaml:"clusters"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing gateway.clusters.include %q: %w", gw.Clusters.Include, err)
	}

	// Merge: included entries load first; inline overrides by name.
	if gw.Clusters.Entries == nil {
		gw.Clusters.Entries = make(map[string]GatewayClusterConfig, len(f.Clusters))
	}
	for name, cfg := range f.Clusters {
		if _, exists := gw.Clusters.Entries[name]; !exists {
			gw.Clusters.Entries[name] = cfg
		}
	}
	gw.Clusters.Include = ""
	return nil
}
