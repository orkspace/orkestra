// pkg/merger/helm.go
//
// Helm-chart Katalog imports (imports.helm:) — authoring-time only. Neither
// the runtime nor the gateway binary ever loads a Katalog with a helm:
// import: production Katalogs are pre-merged (ork generate bundle) into
// plain Kubernetes manifests before deployment — see helm_stub.go for what
// those two builds get instead. Excluding this file from them removes the
// entire Helm SDK (and its openpgp/containerd transitive dependencies) from
// the shipped binaries.

//go:build !runtime && !gateway

package merger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	helmvals "helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
)

// loadHelmSource renders a Helm chart and extracts Katalog CRD definitions.
// The chart must contain at least one template with kind: Katalog.
func (m *Merger) loadHelmSource(src orktypes.HelmSource) (map[string]orktypes.CRDEntry, error) {
	logger.Debug().
		Str("repo", src.Repo).
		Str("chart", src.Chart).
		Str("version", src.Version).
		Msg("merger: loading helm source")

	chartPath, cleanup, err := resolveChartPath(src, m.Refresh)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	return renderAndExtract(src, chartPath)
}

// resolveChartPath returns the local path to the chart directory or .tgz,
// downloading or cloning if necessary.
// cleanup is non-nil when a temp directory was created and must be removed.
// When refresh is true, the local cache is bypassed and the source is fetched fresh.
func resolveChartPath(src orktypes.HelmSource, refresh bool) (chartPath string, cleanup func(), err error) {
	switch {
	case isGitURL(src.Repo):
		// ── Git source ────────────────────────────────────────────────────
		// Clone the repo to a temp dir, then use the chart subdirectory.
		return resolveGitChart(src, refresh)

	case isLocalPath(src.Repo):
		// ── Local source ──────────────────────────────────────────────────
		// repo is a directory. chart is a subdirectory within it.
		// If chart is empty, use repo directly.
		if src.Chart != "" {
			return filepath.Join(src.Repo, src.Chart), nil, nil
		}
		return src.Repo, nil, nil

	default:
		// ── Remote Helm repo ──────────────────────────────────────────────
		return resolveRemoteChart(src, refresh)
	}
}

// isLocalPath returns true for file system paths.
// Handles absolute paths, relative paths, and ~ home directory.
func isLocalPath(s string) bool {
	return strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, "~") ||
		// no scheme at all — treat as relative path
		(!strings.Contains(s, "://") && !strings.HasPrefix(s, "http"))
}

// isGitURL returns true for git repository URLs.
func isGitURL(s string) bool {
	return strings.HasSuffix(s, ".git") ||
		strings.HasPrefix(s, "git://") ||
		strings.HasPrefix(s, "git@") ||
		// GitHub/GitLab URLs that point to a repo (not a file)
		(strings.Contains(s, "github.com") || strings.Contains(s, "gitlab.com")) &&
			!strings.Contains(s, "/raw/")
}

// resolveLocalChart validates and returns the local chart path.
func resolveLocalChart(src orktypes.HelmSource) (string, func(), error) {
	chartPath := src.Repo
	if src.Chart != "" {
		chartPath = filepath.Join(src.Repo, src.Chart)
	}

	// Resolve ~ to home directory
	if strings.HasPrefix(chartPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil, fmt.Errorf("resolving home dir: %w", err)
		}
		chartPath = filepath.Join(home, chartPath[1:])
	}

	if _, err := os.Stat(chartPath); err != nil {
		return "", nil, fmt.Errorf("local chart path %q not found: %w", chartPath, err)
	}

	return chartPath, nil, nil
}

// resolveGitChart clones the git repository and returns the chart directory.
// On a cache hit the clone is skipped entirely. When refresh is true the cache
// is bypassed and the repo is cloned fresh.
func resolveGitChart(src orktypes.HelmSource, refresh bool) (string, func(), error) {
	if !refresh {
		if cached, ok := helmGitCached(src); ok {
			logger.Debug().Str("repo", src.Repo).Msg("merger: git chart served from cache")
			return cached, func() {}, nil
		}
	}

	tmpDir, err := os.MkdirTemp("", "orkestra-git-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	ref := src.Version
	if ref == "" {
		ref = "HEAD"
	}

	logger.Debug().
		Str("repo", src.Repo).
		Str("ref", ref).
		Msg("merger: cloning git repository")

	if err := gitClone(src.Repo, tmpDir, ref); err != nil {
		cleanup()
		return "", nil, err
	}

	chartPath := tmpDir
	if src.Path != "" {
		chartPath = filepath.Join(tmpDir, src.Path)
	} else if src.Chart != "" {
		chartPath = filepath.Join(tmpDir, src.Chart)
	}

	if _, err := os.Stat(chartPath); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("chart path %q not found in repo %q", chartPath, src.Repo)
	}

	logger.Debug().
		Str("repo", src.Repo).
		Str("ref", ref).
		Str("chartPath", chartPath).
		Msg("merger: git repo cloned")

	cached, err := helmGitCacheStore(src, chartPath)
	if err != nil {
		// Cache write failure is non-fatal — use the temp dir directly.
		logger.Debug().Err(err).Msg("merger: failed to store git chart in cache")
		return chartPath, cleanup, nil
	}
	cleanup()
	return cached, func() {}, nil
}

// resolveRemoteChart pulls a chart from a remote Helm repository.
// On a cache hit the pull is skipped entirely. When refresh is true the cache
// is bypassed and the chart is pulled fresh.
func resolveRemoteChart(src orktypes.HelmSource, refresh bool) (string, func(), error) {
	if !refresh {
		if cached, ok := helmRepoCached(src); ok {
			logger.Debug().Str("repo", src.Repo).Str("chart", src.Chart).Msg("merger: helm chart served from cache")
			return cached, func() {}, nil
		}
	}

	settings := cli.New()
	cfg := &action.Configuration{}

	repoEntry := &repo.Entry{
		Name: "orkestra-tmp",
		URL:  src.Repo,
	}

	chartRepo, err := repo.NewChartRepository(repoEntry, getter.All(settings))
	if err != nil {
		return "", nil, fmt.Errorf("creating chart repo for %q: %w", src.Repo, err)
	}

	if _, err := chartRepo.DownloadIndexFile(); err != nil {
		return "", nil, fmt.Errorf("downloading repo index from %q: %w", src.Repo, err)
	}

	tmpDir, err := os.MkdirTemp("", "orkestra-helm-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}

	cleanup := func() { os.RemoveAll(tmpDir) }

	pull := action.NewPullWithOpts(action.WithConfig(cfg))
	pull.Settings = settings
	pull.RepoURL = src.Repo
	pull.Version = src.Version
	pull.Untar = true
	pull.DestDir = tmpDir

	if _, err := pull.Run(src.Chart); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("pulling chart %q@%s: %w", src.Chart, src.Version, err)
	}

	chartPath := filepath.Join(tmpDir, src.Chart)
	cached, err := helmRepoCacheStore(src, chartPath)
	if err != nil {
		logger.Debug().Err(err).Msg("merger: failed to store helm chart in cache")
		return chartPath, cleanup, nil
	}
	cleanup()
	return cached, func() {}, nil
}

// renderAndExtract renders a chart from a local path and extracts Katalog CRDs.
func renderAndExtract(src orktypes.HelmSource, chartPath string) (map[string]orktypes.CRDEntry, error) {
	settings := cli.New()

	// ── Load value files ──────────────────────────────────────────────────────
	valueOpts := &helmvals.Options{}
	for _, vf := range src.ValueFiles {
		resolved, err := resolveEnvVar(vf)
		if err != nil {
			return nil, fmt.Errorf("resolving value file %q: %w", vf, err)
		}

		if strings.HasPrefix(resolved, "http") {
			data, err := loadFile(resolved)
			if err != nil {
				return nil, fmt.Errorf("fetching remote values %q: %w", resolved, err)
			}
			tmp, err := writeTempFile(data, "orkestra-values-*.yaml")
			if err != nil {
				return nil, err
			}
			defer os.Remove(tmp)
			resolved = tmp
		}

		valueOpts.ValueFiles = append(valueOpts.ValueFiles, resolved)
	}

	vals, err := valueOpts.MergeValues(getter.All(settings))
	if err != nil {
		return nil, fmt.Errorf("merging helm values: %w", err)
	}

	for k, v := range src.Values {
		vals[k] = v
	}

	// ── Load and render ───────────────────────────────────────────────────────
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("loading chart from %q: %w", chartPath, err)
	}

	cfg := &action.Configuration{}
	install := action.NewInstall(cfg)
	install.DryRun = true
	install.ReleaseName = "orkestra-render"
	install.ClientOnly = true
	install.IncludeCRDs = true

	release, err := install.Run(chart, vals)
	if err != nil {
		return nil, fmt.Errorf("rendering chart %q: %w", src.Chart, err)
	}

	return extractKatalogCRDs(release.Manifest, src.Chart)
}

// renderAndExtract renders a Helm chart and extracts Katalog CRD definitions.

// extractKatalogCRDs parses rendered Helm output and extracts
// CRD definitions from any template with kind: Katalog.
func extractKatalogCRDs(manifest, chartName string) (map[string]orktypes.CRDEntry, error) {
	allCRDs := make(map[string]orktypes.CRDEntry)

	// Split on YAML document separator — one chart renders multiple templates
	docs := strings.Split(manifest, "\n---\n")

	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		katalog, err := parseKatalogDoc([]byte(doc), "helm:"+chartName)
		if err != nil {
			return nil, err // malformed Katalog — hard error
		}
		if katalog == nil {
			continue // not a Katalog — skip
		}

		for name, crd := range katalog.Spec.CRDs {
			crd.Name = name
			allCRDs[name] = crd
		}

		logger.Debug().
			Str("chart", chartName).
			Int("crds", len(katalog.Spec.CRDs)).
			Msg("merger: extracted CRDs from helm template")
	}

	if len(allCRDs) == 0 {
		return nil, fmt.Errorf(
			"chart %q produced no Katalog templates — "+
				"ensure at least one template has kind: %s",
			chartName, konfig.KatalogKind(),
		)
	}

	return allCRDs, nil
}
