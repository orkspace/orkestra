// pkg/types/registry.go
package types

import "strings"

// RegistrySource declares one pattern to pull from a registry.
//
// A registry source can be Git-based or OCI-based. The source type is
// determined by the oci field (false by default — git pull).
//
// After pulling, Orkestra validates the five required files exist and are
// non-empty. It then loads either katalog.yaml (default) or komposer.yaml
// based on useKomposer. Exactly one is loaded — not both.
//
// Plain string — URL is the entire entry, OCI scheme detected from prefix:
//
//   - oci://ghcr.io/orkspace/orkestra-registry/postgres@v14
//   - https://github.com/myorg/registry@main
//
// Struct with url field — same URL forms, required when auth or other fields are set:
//
//   - url: oci://ghcr.io/orkspace/orkestra-registry/postgres@v14
//
//   - url: ghcr.io/orkspace/orkestra-registry/postgres@v14
//     oci: true                                              # equivalent to oci:// prefix
//
// Git form:
//
//   - url: https://github.com/myorg/registry
//     version: main
//     oci: false      # default
//     useKomposer: true
//     auth:
//       type: github
//       fromEnv: GITHUB_TOKEN
//
// Private OCI with auth:
//
//   - url: registry.myorg.com/operators/postgres@v14-hardened
//     oci: true
//     auth:
//       type: basic
//       usernameFromEnv: REGISTRY_USER
//       passwordFromEnv: REGISTRY_PASSWORD

// RegistrySource is one entry in imports.registry.
type RegistrySource struct {
	// URL — the registry URL.
	//
	// Git:  https://github.com/myorg/orkestra-registry
	// OCI:  ghcr.io/orkspace/orkestra-registry/postgres
	//
	// Shorthand — embed version with @:
	//   ghcr.io/orkspace/orkestra-registry/postgres@v14
	//   https://github.com/myorg/registry@main
	//
	// When @ is present, Version field is ignored.
	URL string `yaml:"url" validate:"required" json:"url"`

	// Version — the version, tag, branch, or SHA to pull.
	//
	// For OCI:  semantic version tag    "v14", "v14.2.0"
	// For Git:  branch, tag, or SHA     "main", "v1.0.0", "abc123"
	//
	// Ignored when URL contains @.
	// Defaults to "main" for Git, "latest" for OCI when not set.
	Version string `yaml:"version,omitempty" json:"version,omitempty"`

	// OCI — when true, pull the pattern as an OCI artifact.
	// When false (default), pull via Git clone or raw file fetch.
	//
	// OCI artifacts are pulled using the ORAS protocol.
	// GitHub and GitLab URLs with oci: false use raw file HTTP — no clone needed.
	// Other Git URLs with oci: false use git clone.
	OCI bool `yaml:"oci,omitempty" json:"oci,omitempty"`

	// UseKomposer — when true, load komposer.yaml from the pulled pattern.
	// When false (default), load katalog.yaml.
	//
	// Exactly one file is loaded — not both.
	//
	// UseKomposer: false (default)
	//   Use this when you want the CRD definitions and will override them
	//   inline in your own Komposer. This is the common case.
	//
	// UseKomposer: true
	//   Use this when you want to accept the upstream operator's full
	//   source tree as-is — their sources, their defaults, everything.
	//   Useful for internal teams with a canonical registry where the
	//   upstream Komposer is exactly what you want to run.
	//
	// Warning: loading a Komposer from a registry source means that
	// Komposer's own sources are also resolved. A Komposer that sources
	// other Katalogs will pull those too. Understand the upstream
	// dependency tree before enabling this.
	UseKomposer bool `yaml:"useKomposer,omitempty" json:"useKomposer,omitempty"`

	// Auth — optional authentication for the registry.
	// When empty, requests are unauthenticated.
	// Auth credentials are resolved from environment variables — never literals.
	Auth *FileSourceAuth `yaml:"auth,omitempty" json:"auth,omitempty"`

	// Katalog — map of katalog names to their version references.
	// Key: katalog name (directory name under registry/katalogs/).
	// Value: version reference (branch, sha, or version tag).
	Katalog map[string]RegistryRef `yaml:"katalog,omitempty" json:"katalog,omitempty"`

	// Future source types (not yet implemented):
	// Hooks map[string]RegistryRef `yaml:"hooks,omitempty"`
	// Core  map[string]RegistryRef `yaml:"core,omitempty"`
}

// RegistryRef declares how to resolve a specific item from the registry.
// Exactly one of Branch, SHA, or Version should be set.
// When none are set, defaults to the default branch of the registry.
//
// Example:
//
//	website:
//	  branch: main        # track a branch
//
//	platform-namespace:
//	  version: v1.2.0     # pin to a release tag
//
//	application:
//	  sha: abc123def456   # pin to an exact commit
type RegistryRef struct {
	// Branch — track a branch (e.g. "main", "develop").
	// The latest commit on the branch is fetched.
	Branch string `yaml:"branch,omitempty" json:"branch,omitempty"`

	// Version — pin to a release tag (e.g. "v1.2.0", "v2").
	// Git tags are fetched — version is used as the tag name.
	Version string `yaml:"version,omitempty" json:"version,omitempty"`

	// SHA — pin to an exact commit hash.
	// The full or partial SHA works (Git resolves partial SHAs).
	SHA string `yaml:"sha,omitempty" json:"sha,omitempty"`
}

// Ref returns the effective git ref for this RegistryRef.
// Priority: SHA > Version > Branch > "main" (default).
func (r RegistryRef) Ref() string {
	if r.SHA != "" {
		return r.SHA
	}
	if r.Version != "" {
		return r.Version
	}
	if r.Branch != "" {
		return r.Branch
	}
	return "main"
}

// IsDefault reports whether no ref is explicitly set.
func (r RegistryRef) IsDefault() bool {
	return r.SHA == "" && r.Version == "" && r.Branch == ""
}

// UnmarshalYAML allows RegistrySource to be declared as a plain string or a struct.
//
// Plain string — URL only, no auth, no overrides:
//
//	registry:
//	  - oci://ghcr.io/myorg/postgres@v14
//	  - https://github.com/myorg/registry@main
//
// Struct — when auth, useKomposer, or other fields are needed:
//
//	registry:
//	  - url: registry.myorg.com/operators/postgres@v14
//	    auth:
//	      type: basic
//	      usernameFromEnv: REGISTRY_USER
//	      passwordFromEnv: REGISTRY_PASSWORD
func (r *RegistrySource) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var plain string
	if err := unmarshal(&plain); err == nil {
		r.URL = plain
		return nil
	}
	type registrySourceAlias RegistrySource
	var alias registrySourceAlias
	if err := unmarshal(&alias); err != nil {
		return err
	}
	*r = RegistrySource(alias)
	return nil
}

// IsOCI returns true when this source should be pulled as an OCI artifact.
// Either the oci field is explicitly set, or the URL uses the oci:// scheme:
//
//	oci: true  +  url: ghcr.io/myorg/postgres@v14
//	url: oci://ghcr.io/myorg/postgres@v14          (no oci: true needed)
func (r RegistrySource) IsOCI() bool {
	return r.OCI || strings.HasPrefix(strings.TrimSpace(r.URL), "oci://")
}

// ResolvedURL returns the effective URL with the version extracted.
// Handles both the @ shorthand and the explicit Version field.
// Strips the oci:// scheme when present — IsOCI() carries that signal instead.
//
// Returns (cleanURL, version).
// cleanURL is the URL without scheme or @ suffix.
// version is the resolved version string (never Empty().
func (r RegistrySource) ResolvedURL() (cleanURL, version string) {
	url := strings.TrimSpace(r.URL)
	url = strings.TrimPrefix(url, "oci://")

	if idx := strings.LastIndex(url, "@"); idx != -1 {
		// @ shorthand — split on last @
		// handles: ghcr.io/myorg/postgres@v14
		// handles: https://github.com/myorg/registry@main
		cleanURL = url[:idx]
		version = url[idx+1:]
		return
	}

	// Standard OCI colon tag — split on the last : that appears after the last /
	// handles: oci://ghcr.io/myorg/postgres:v1.0.0
	// avoids splitting on port numbers: localhost:5000/repo (no slash after the colon)
	if lastSlash := strings.LastIndex(url, "/"); lastSlash != -1 {
		afterSlash := url[lastSlash+1:]
		if colonIdx := strings.LastIndex(afterSlash, ":"); colonIdx != -1 {
			cleanURL = url[:lastSlash+1+colonIdx]
			version = afterSlash[colonIdx+1:]
			return
		}
	}

	cleanURL = url
	version = strings.TrimSpace(r.Version)
	if version == "" {
		version = r.defaultVersion()
	}
	return
}

// defaultVersion returns the default version when none is declared.
func (r RegistrySource) defaultVersion() string {
	if r.IsOCI() {
		return "latest"
	}
	return "main"
}

// SourceFile returns the filename Orkestra should load after pulling the pattern.
// Either "katalog.yaml" or "komposer.yaml" — never both.
func (r RegistrySource) SourceFile() string {
	if r.UseKomposer {
		return "komposer.yaml"
	}
	return "katalog.yaml"
}
