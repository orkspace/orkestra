// pkg/merger/registry.go
//
// Registry Katalog imports (imports.registry:) — authoring-time only, same
// reasoning as helm.go: the runtime and gateway only ever read the
// katalog.yaml key from a ConfigMap, already fully merged by
// `ork generate bundle` with no imports left to resolve. See registry_stub.go
// for what those two builds get instead.

//go:build !runtime && !gateway

package merger

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/logger"
	pkgregistry "github.com/orkspace/orkestra/pkg/registry"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	orasauth "oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

const orasPullTimeout = 2 * time.Minute

// knownPatternFiles is the complete set of files a pattern may contain.
// All are attempted during Git pulls; presence is validated after pull
// by validatePatternStructure using the kind-specific required/optional rules.
var knownPatternFiles = []string{
	pkgregistry.FileKatalog,
	pkgregistry.FileMotif,
	pkgregistry.FileCRD,
	pkgregistry.FileReadme,
	pkgregistry.FileCR,
	pkgregistry.FileE2E,
	pkgregistry.FileSimulate,
	pkgregistry.FileGoMod,
	pkgregistry.FileGoSum,
	pkgregistry.FileMakefile,
}

// loadRegistrySource loads a single registry pattern entry.
//
// Resolution sequence:
//  1. Parse url@version shorthand or url + version fields
//  2. Determine pull method: OCI or Git
//  3. Pull pattern to a temp directory
//  4. Validate pattern structure based on detected kind
//  5. Load katalog.yaml or komposer.yaml based on UseKomposer
//  6. Parse and return the CRD entries
func (m *Merger) loadRegistrySource(src orktypes.RegistrySource) (map[string]orktypes.CRDEntry, error) {
	cleanURL, version := src.ResolvedURL()

	logger.Debug().
		Str("url", cleanURL).
		Str("version", version).
		Bool("oci", src.IsOCI()).
		Str("loads", src.SourceFile()).
		Msg("merger: pulling registry source")

	auth, err := resolveRegistryAuth(src.Auth)
	if err != nil {
		return nil, fmt.Errorf("registry %q: auth: %w", cleanURL, err)
	}

	tmpDir, cleanup, err := m.pullPattern(cleanURL, version, src.IsOCI(), auth)
	if err != nil {
		return nil, fmt.Errorf("registry %q@%s: pull failed: %w", cleanURL, version, err)
	}
	defer cleanup()

	if err := validatePatternStructure(tmpDir, cleanURL, version); err != nil {
		return nil, err
	}

	sourceFile := filepath.Join(tmpDir, src.SourceFile())
	sourcePath := fmt.Sprintf("registry:%s@%s/%s", cleanURL, version, src.SourceFile())

	data, err := readLocal(sourceFile)
	if err != nil {
		return nil, fmt.Errorf("registry %q@%s: reading %s: %w",
			cleanURL, version, src.SourceFile(), err)
	}

	doc, err := parseKatalogDoc(data, sourcePath)
	if err != nil {
		return nil, fmt.Errorf("registry %q@%s: parsing %s: %w",
			cleanURL, version, src.SourceFile(), err)
	}
	if doc == nil {
		return nil, fmt.Errorf(
			"registry %q@%s: %s is not a valid Katalog or Komposer document",
			cleanURL, version, src.SourceFile(),
		)
	}

	if lc := doc.Lifecycle; lc != nil {
		if dep := lc.Deprecation; dep != nil {
			msg := fmt.Sprintf("warning: registry pattern %q@%s is deprecated", cleanURL, version)
			if dep.MigratedTo != "" {
				msg += fmt.Sprintf(" — migrate to: %s", dep.MigratedTo)
			}
			if dep.Message != "" {
				msg += fmt.Sprintf(" (%s)", dep.Message)
			}
			fmt.Fprintln(os.Stderr, msg)
		}
	}

	registryRef := fmt.Sprintf("%s@%s", cleanURL, version)

	switch doc.Kind {
	case konfig.KatalogKind():
		if src.UseKomposer {
			return nil, fmt.Errorf(
				"registry %q@%s: useKomposer is true but komposer.yaml contains kind %q — "+
					"check the upstream pattern's komposer.yaml",
				cleanURL, version, doc.Kind,
			)
		}
		entries, err := m.loadKatalog(sourcePath, doc)
		if err != nil {
			return nil, err
		}
		stampRegistryRef(entries, registryRef)
		return entries, nil

	case konfig.KomposerKind():
		if !src.UseKomposer {
			return nil, fmt.Errorf(
				"registry %q@%s: useKomposer is false but katalog.yaml contains kind %q — "+
					"set useKomposer: true to load the upstream Komposer, or check the pattern structure",
				cleanURL, version, doc.Kind,
			)
		}
		entries, err := m.loadKomposer(sourcePath, doc)
		if err != nil {
			return nil, err
		}
		stampRegistryRef(entries, registryRef)
		return entries, nil

	default:
		return nil, fmt.Errorf(
			"registry %q@%s: %s has unexpected kind %q — expected %q or %q",
			cleanURL, version, src.SourceFile(), doc.Kind,
			konfig.KatalogKind(), konfig.KomposerKind(),
		)
	}
}

// stampRegistryRef sets RegistryRef on every entry in the map.
func stampRegistryRef(entries map[string]orktypes.CRDEntry, ref string) {
	for name, entry := range entries {
		entry.RegistryRef = ref
		entries[name] = entry
	}
}

// pullPattern fetches a registry pattern to a temp directory.
// Returns the temp dir path and a cleanup function.
func (m *Merger) pullPattern(
	url, version string,
	oci bool,
	auth *utils.FileAuth,
) (tmpDir string, cleanup func(), err error) {
	tmpDir, err = os.MkdirTemp("", "orkestra-registry-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}

	cleanup = func() { os.RemoveAll(tmpDir) }

	if oci {
		err = m.pullOCIPattern(url, version, tmpDir, auth)
	} else {
		err = m.pullGitPattern(url, version, tmpDir, auth)
	}

	if err != nil {
		cleanup()
		return "", nil, err
	}

	return tmpDir, cleanup, nil
}

// ── OCI pull ──────────────────────────────────────────────────────────────────

func (m *Merger) pullOCIPattern(url, version, tmpDir string, auth *utils.FileAuth) error {
	ociRef := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	ociRef = strings.TrimSuffix(ociRef, "/")
	ociRef = fmt.Sprintf("%s:%s", ociRef, version)

	// Serve from local cache when available — avoids a network round-trip on
	// every ork validate/template/simulate after ork pull.
	if pkgRef, err := pkgregistry.Resolve(ociRef); err == nil {
		if cacheDir, err := pkgRef.CachePath(); err == nil && pkgRef.IsCached() {
			logger.Debug().
				Str("ref", ociRef).
				Str("cache", cacheDir).
				Msg("registry: serving OCI artifact from local cache")
			return copyPatternFilesFromCache(cacheDir, tmpDir)
		}
	}

	logger.Debug().
		Str("ref", ociRef).
		Str("dst", tmpDir).
		Msg("registry: pulling OCI artifact with ORAS Go library")

	if err := orasPull(ociRef, tmpDir, auth); err != nil {
		return fmt.Errorf("OCI pull %q: %w", ociRef, err)
	}

	logger.Debug().
		Str("ref", ociRef).
		Str("dst", tmpDir).
		Msg("registry: OCI artifact pulled successfully")

	return nil
}

// copyPatternFilesFromCache copies the known pattern files from a cache
// directory into a temp directory for the merger to process.
func copyPatternFilesFromCache(cacheDir, tmpDir string) error {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return fmt.Errorf("reading cache dir %s: %w", cacheDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := readLocal(filepath.Join(cacheDir, e.Name()))
		if err != nil {
			return fmt.Errorf("reading cached file %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, e.Name()), data, 0644); err != nil {
			return fmt.Errorf("writing %s to temp dir: %w", e.Name(), err)
		}
	}
	return nil
}

func orasPull(ref, dst string, auth *utils.FileAuth) error {
	ctx, cancel := context.WithTimeout(context.Background(), orasPullTimeout)
	defer cancel()

	repoName, reference, ok := strings.Cut(ref, ":")
	if !ok || reference == "" {
		return fmt.Errorf("invalid OCI reference %q: missing tag or digest", ref)
	}

	repo, err := remote.NewRepository(repoName)
	if err != nil {
		return fmt.Errorf("creating repository for %q: %w", repoName, err)
	}

	if auth != nil {
		repo.Client = &orasauth.Client{
			ClientID: "orkestra",
			Credential: func(ctx context.Context, registry string) (orasauth.Credential, error) {
				switch strings.ToLower(auth.Type) {
				case "basic":
					if auth.Username != "" && auth.Password != "" {
						return orasauth.Credential{
							Username: auth.Username,
							Password: auth.Password,
						}, nil
					}
				case "bearer", "github":
					if auth.BearerToken != "" {
						return orasauth.Credential{
							RefreshToken: auth.BearerToken,
						}, nil
					}
				}
				return orasauth.EmptyCredential, nil
			},
		}
	} else {
		// No explicit auth — fall back to Docker credential store (~/.docker/config.json).
		// This mirrors pkg/registry.Client.remoteRepo so `ork pull -f`
		// and `ork pull <url>` use the same credential source.
		if store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{}); err == nil {
			repo.Client = &orasauth.Client{
				ClientID:   "orkestra",
				Cache:      orasauth.DefaultCache,
				Credential: credentials.Credential(store),
			}
		}
	}

	store, err := file.New(dst)
	if err != nil {
		return fmt.Errorf("creating file store: %w", err)
	}
	defer store.Close()

	_, err = oras.Copy(ctx,
		repo, reference,
		store, "",
		oras.DefaultCopyOptions,
	)
	if err != nil {
		return fmt.Errorf("pulling OCI artifact %q: %w", ref, err)
	}

	return nil
}

// ── Git pull ──────────────────────────────────────────────────────────────────

func (m *Merger) pullGitPattern(url, version, tmpDir string, auth *utils.FileAuth) error {
	switch {
	case isGitHubURL(url):
		return m.pullGitHubPattern(url, version, tmpDir, auth)
	case isGitLabURL(url):
		return m.pullGitLabPattern(url, version, tmpDir, auth)
	default:
		return pullGenericGitPattern(url, version, tmpDir, auth)
	}
}

// pullGitHubPattern fetches all known pattern files from a GitHub repository.
// Files that don't exist are silently skipped; validatePatternStructure enforces
// the kind-specific required set after the pull.
func (m *Merger) pullGitHubPattern(url, version, tmpDir string, auth *utils.FileAuth) error {
	for _, filename := range knownPatternFiles {
		rawURL := githubRawURL(url, version, filename)
		data, err := utils.LoadFileWithAuth(rawURL, auth)
		if err != nil {
			continue // file not present in this pattern — validated after pull
		}
		if err := os.WriteFile(filepath.Join(tmpDir, filename), data, 0644); err != nil {
			return fmt.Errorf("writing %q: %w", filename, err)
		}
	}
	return nil
}

// pullGitLabPattern fetches all known pattern files from a GitLab repository.
func (m *Merger) pullGitLabPattern(url, version, tmpDir string, auth *utils.FileAuth) error {
	for _, filename := range knownPatternFiles {
		rawURL := gitlabRawURL(url, version, filename)
		data, err := utils.LoadFileWithAuth(rawURL, auth)
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(tmpDir, filename), data, 0644); err != nil {
			return fmt.Errorf("writing %q: %w", filename, err)
		}
	}
	return nil
}

// pullGenericGitPattern clones the repository and copies all known pattern files.
func pullGenericGitPattern(url, version, tmpDir string, auth *utils.FileAuth) error {
	cloneDir, err := os.MkdirTemp("", "orkestra-clone-*")
	if err != nil {
		return fmt.Errorf("creating clone dir: %w", err)
	}
	defer os.RemoveAll(cloneDir)

	cloneURL := injectAuthIntoURL(url, auth)
	if err := gitClone(cloneURL, cloneDir, version); err != nil {
		return err
	}

	for _, filename := range knownPatternFiles {
		data, err := readLocal(filepath.Join(cloneDir, filename))
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(tmpDir, filename), data, 0644); err != nil {
			return fmt.Errorf("copying %q: %w", filename, err)
		}
	}

	return nil
}

// pullMotifFromGit fetches only motif.yaml from a Git host.
// Used for standalone Motif repos (not full patterns).
func (m *Merger) pullMotifFromGit(url, version, tmpDir string, auth *utils.FileAuth) error {
	var rawURL string
	switch {
	case isGitHubURL(url):
		rawURL = githubRawURL(url, version, "motif.yaml")
	case isGitLabURL(url):
		rawURL = gitlabRawURL(url, version, "motif.yaml")
	default:
		return m.fetchMotifFromGenericGit(url, version, tmpDir, auth)
	}

	data, err := utils.LoadFileWithAuth(rawURL, auth)
	if err != nil {
		return fmt.Errorf("fetching motif.yaml from %s@%s: %w", url, version, err)
	}
	return os.WriteFile(filepath.Join(tmpDir, "motif.yaml"), data, 0644)
}

func (m *Merger) fetchMotifFromGenericGit(url, version, tmpDir string, auth *utils.FileAuth) error {
	cloneDir, err := os.MkdirTemp("", "orkestra-motif-clone-*")
	if err != nil {
		return fmt.Errorf("creating clone dir: %w", err)
	}
	defer os.RemoveAll(cloneDir)

	cloneURL := injectAuthIntoURL(url, auth)
	if err := gitClone(cloneURL, cloneDir, version); err != nil {
		return err
	}

	data, err := readLocal(filepath.Join(cloneDir, "motif.yaml"))
	if err != nil {
		return fmt.Errorf("motif.yaml not found in repository %s@%s", url, version)
	}
	return os.WriteFile(filepath.Join(tmpDir, "motif.yaml"), data, 0644)
}

// ── Pattern validation ────────────────────────────────────────────────────────

// validatePatternStructure validates that dir contains a well-formed pattern.
// Kind is auto-detected; required files are determined by the pattern kind.
//
// Katalog patterns require only katalog.yaml (crd.yaml, README.md, cr.yaml are optional).
// Motif patterns require only motif.yaml.
func validatePatternStructure(dir, url, version string) error {
	_, _, _, err := pkgregistry.ValidatePatternDirectory(dir)
	if err != nil {
		return fmt.Errorf("registry pattern %q@%s failed structure validation: %w", url, version, err)
	}
	return nil
}

// ── Registry URL resolution ───────────────────────────────────────────────────

func (m *Merger) resolveRegistryURL(srcURL string) (string, error) {
	if srcURL != "" {
		return srcURL, nil
	}

	env := os.Getenv("ORK_REGISTRY")
	if env != "" {
		return env, nil
	}

	if m.registryURL != "" {
		return m.registryURL, nil
	}

	return "", fmt.Errorf(
		"no registry URL configured for this source.\n\n" +
			"Set the registry URL using one of:\n" +
			"  1. ORK_REGISTRY environment variable:\n" +
			"       export ORK_REGISTRY=https://github.com/myorg/orkestra-registry\n\n" +
			"  2. Explicit url in the source block:\n" +
			"       imports:\n" +
			"         registry:\n" +
			"           - url: https://github.com/myorg/orkestra-registry\n" +
			"             katalog:\n" +
			"               website:\n" +
			"                 branch: main",
	)
}

// ── URL construction ──────────────────────────────────────────────────────────

func isGitHubURL(u string) bool {
	return strings.Contains(u, "github.com") && !strings.HasSuffix(u, ".git")
}

func isGitLabURL(u string) bool {
	return strings.Contains(u, "gitlab.com") && !strings.HasSuffix(u, ".git")
}

func githubRawURL(repoURL, ref, filePath string) string {
	u := strings.TrimSuffix(strings.TrimSuffix(repoURL, ".git"), "/")
	u = strings.Replace(u, "https://github.com/", "https://raw.githubusercontent.com/", 1)
	u = strings.Replace(u, "http://github.com/", "https://raw.githubusercontent.com/", 1)
	return fmt.Sprintf("%s/%s/%s", u, ref, filePath)
}

func gitlabRawURL(repoURL, ref, filePath string) string {
	u := strings.TrimSuffix(strings.TrimSuffix(repoURL, ".git"), "/")
	return fmt.Sprintf("%s/-/raw/%s/%s", u, ref, filePath)
}

func injectAuthIntoURL(rawURL string, auth *utils.FileAuth) string {
	if auth == nil || strings.ToLower(auth.Type) != "basic" || auth.Username == "" {
		return rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	parsed.User = url.UserPassword(auth.Username, auth.Password)
	return parsed.String()
}

// ── Auth resolution ───────────────────────────────────────────────────────────

func resolveRegistryAuth(auth *orktypes.FileSourceAuth) (*utils.FileAuth, error) {
	if auth == nil {
		return nil, nil
	}
	return auth.Resolve()
}
