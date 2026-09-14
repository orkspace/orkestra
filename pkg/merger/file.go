// pkg/merger/file.go
package merger

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ── Merge rules ───────────────────────────────────────────────────────────────
//
// Katalog  (kind: Katalog)
//   Declares CRDs directly in spec.crds.
//   Must NOT declare imports — imports are a Komposer concern.
//   Error if imports block is present.
//
// Komposer (kind: Komposer)
//   Composes Katalogs from multiple imports (files, registry, helm).
//   May declare inline spec.crds as overrides — merged last, win on conflict.
//   Imports are resolved recursively. Each import must be a Katalog.
//   A Komposer cannot import another Komposer.
//
// Within one file's import tree:
//   localSeen catches duplicates across imports and within inline block.
//
// Across entry point files:
//   seen in Merge() catches duplicates.
//
// Inline overrides import:
//   valid — map key collision triggers mergeCRDEntry.
//
// Inline duplicates inline:
//   always an error.

// loadKatalogFile parses one file and dispatches to the correct loader
// based on its kind. Returns the deduplicated CRD map for this file tree.
func (m *Merger) loadKatalogFile(path string) (map[string]orktypes.CRDEntry, error) {
	data, err := loadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}

	doc, err := parseKatalogDoc(data, path)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		if kind := sniffDocumentKind(data); kind != "" {
			return nil, fmt.Errorf("%q: kind %q cannot be used here — expected kind: Katalog or Komposer", path, kind)
		}
		logger.Debug().
			Str("path", path).
			Msg("merger: skipping — not a valid Katalog or Komposer document")
		return nil, nil
	}

	// Dispatch on Kind — Katalog and Komposer are handled differently
	switch doc.Kind {
	case konfig.KatalogKind():
		return m.loadKatalog(path, doc)
	case konfig.KomposerKind():
		return m.loadKomposer(path, doc)
	default:
		// Should not reach here — parseKatalogDoc already validates Kind
		return nil, fmt.Errorf("%q: unexpected kind %q", path, doc.Kind)
	}
}

// loadKatalog reads CRD definitions from a Katalog file.
// Map keys are the CRD names; Name is injected from the key.
func (m *Merger) loadKatalog(path string, doc *orktypes.KatalogFile) (map[string]orktypes.CRDEntry, error) {
	// Guard — imports in a Katalog is a mistake
	if doc.LooksLikeKomposer() {
		return nil, fmt.Errorf(
			"%q: kind Katalog cannot declare imports — "+
				"use kind: Komposer to compose multiple Katalogs",
			path,
		)
	}

	result := make(map[string]orktypes.CRDEntry, len(doc.Spec.CRDs))

	// Resolve the katalog namespace — "default" when not declared.
	katalogNamespace := doc.Metadata.Namespace
	if katalogNamespace == "" {
		katalogNamespace = "default"
	}

	// katalogDir is used to resolve relative crdFile and crFiles paths.
	// We resolve them here — while we still have the katalog file's path —
	// so they become absolute before being merged into the top-level map.
	// This allows ork run/validate -f /any/path/katalog.yaml to work from
	// any working directory, even when the katalog is imported by a Komposer.
	//
	// katalogDir must be made genuinely absolute (not just joined once) —
	// downstream resolution (e.g. populateAPITypesFromCRDFile in
	// pkg/katalog/crdfile.go) re-checks filepath.IsAbs() on the already-
	// joined path and re-joins it against katalogDir again if it's still
	// relative, doubling the directory. A relative -f path with a real
	// subdirectory (e.g. -f a/b/katalog.yaml run from elsewhere) used to
	// silently double into a/b/a/b/crd.yaml — invisible when katalogDir
	// happened to be "." (running from inside the katalog's own directory).
	katalogDir := filepath.Dir(path)
	if abs, err := filepath.Abs(katalogDir); err == nil {
		katalogDir = abs
	}

	for name, crd := range doc.Spec.CRDs {
		if name == "" {
			return nil, fmt.Errorf("%q spec.crds: CRD with empty key", path)
		}
		// Duplicate within the same file is impossible — map keys are unique.
		crd.Name = name

		// Resolve crdFile and crFiles to absolute paths relative to this katalog.
		if crd.CRDFile != "" && !filepath.IsAbs(crd.CRDFile) && !strings.HasPrefix(crd.CRDFile, "http") {
			crd.CRDFile = filepath.Join(katalogDir, crd.CRDFile)
		}
		for i, cf := range crd.CRFiles {
			if !filepath.IsAbs(cf) && !strings.HasPrefix(cf, "http") {
				crd.CRFiles[i] = filepath.Join(katalogDir, cf)
			}
		}
		if crd.Setup != nil {
			for i, entry := range crd.Setup.Apply {
				if !filepath.IsAbs(entry.Path) && !strings.HasPrefix(entry.Path, "http") {
					crd.Setup.Apply[i].Path = filepath.Join(katalogDir, entry.Path)
				}
			}
		}
		// Resolve motif file paths in imports to absolute so they work regardless
		// of the working directory when expandMotifImports runs.
		for i, imp := range crd.Imports {
			if isFileMotif(imp.Motif) && !filepath.IsAbs(imp.Motif) {
				crd.Imports[i].Motif = filepath.Join(katalogDir, imp.Motif)
			}
		}

		// Stamp katalog metadata — only when not already set so that values
		// deserialized from an expanded ConfigMap YAML are preserved.
		if crd.KatalogNamespace == "" {
			crd.KatalogNamespace = katalogNamespace
		}
		if crd.KatalogDescription == "" {
			crd.KatalogDescription = doc.Metadata.Description
		}
		if crd.KatalogVersion == "" {
			crd.KatalogVersion = doc.Metadata.Version
		}

		// Apply katalog-level CrossAccess as the default for every CRD that
		// does not declare its own crossAccess field.
		if crd.CrossAccess == nil && doc.CrossAccess != nil {
			v := *doc.CrossAccess
			crd.CrossAccess = &v
		}

		// Merge spec-level restrictions into each CRD (additive).
		protect := doc.Security.NamespaceProtection
		if protect != nil {
			if len(protect.RestrictedNamespaces) > 0 {
				crd.RestrictedNamespaces = protect.RestrictedNamespaces.Merge(crd.RestrictedNamespaces)
			}
			if len(protect.AllowedNamespaces) > 0 {
				crd.AllowedNamespaces = protect.AllowedNamespaces.Merge(crd.AllowedNamespaces)
			}
		}

		result[name] = crd

		logger.Debug().
			Str("crd", name).
			Str("source", path).
			Msg("merger: CRD loaded from Katalog")
	}

	logger.Debug().
		Str("path", path).
		Int("crds", len(result)).
		Msg("merger: Katalog loaded")

	// This is a katalog
	apiMetadata := apiMetadata{
		APIVersion: doc.APIVersion,
		Kind:       doc.Kind,
		Metadata:   doc.Metadata,
	}
	m.apiMetadata = apiMetadata
	m.lifecycle = doc.Lifecycle
	m.policy = doc.Policy
	m.security = doc.Security
	m.notification = doc.Notification
	m.providers = doc.Providers
	m.gateway = doc.Gateway
	m.publish = doc.Publish
	m.profiles = doc.Profiles
	m.notes = doc.Notes
	// Resolve spec.imports motif file paths to absolute, same as CRD-level imports above.
	specImports := make([]orktypes.MotifImport, len(doc.Spec.Imports))
	copy(specImports, doc.Spec.Imports)
	for i, imp := range specImports {
		if isFileMotif(imp.Motif) && !filepath.IsAbs(imp.Motif) {
			specImports[i].Motif = filepath.Join(katalogDir, imp.Motif)
		}
	}
	m.specImports = specImports

	return result, nil
}

// loadKomposer resolves imports from a Komposer file and merges all CRDs.
func (m *Merger) loadKomposer(path string, doc *orktypes.KatalogFile) (map[string]orktypes.CRDEntry, error) {
	if !doc.WithImportsOrSpecOverrides() {
		logger.Warn().
			Str("path", path).
			Msg("merger: Komposer has no imports and no inline CRDs — nothing to load")
		return nil, nil
	}

	localSeen := map[string]string{}
	allCRDs := make(map[string]orktypes.CRDEntry)

	// accSecurity, accNotification, accProviders, accNotes, and accProfiles accumulate top-level
	// settings from all imported Katalogs. Each import that calls loadKatalog sets these
	// as side-effects on m; we capture and merge here so they are not discarded
	// when the Komposer's own (possibly Empty() block is applied at the end.
	var accSecurity orktypes.KatalogSecurity
	var accNotification *orktypes.KatalogNotification
	var accProviders []orktypes.KatalogProviderRequirement
	var accProfiles orktypes.ProfileRegistry
	var accSpecImports []orktypes.MotifImport
	var accNotes orktypes.NoteRegistry
	notesSeen := make(map[string]string) // note name → import label, for cross-Katalog conflict detection

	// ── Step 1: registry imports ─────────────────────────────────────────────
	if doc.Imports != nil {
		for i, regSrc := range doc.Imports.Registry {
			crds, err := m.loadRegistrySource(regSrc)
			if err != nil {
				return nil, fmt.Errorf("%q imports.registry[%d]: %w", path, i, err)
			}

			for name, crd := range crds {
				srcName := fmt.Sprintf("registry:%d", i)
				if regSrc.URL != "" {
					srcName = "registry:" + regSrc.URL
				}
				if err := checkDuplicate(localSeen, name, srcName); err != nil {
					return nil, fmt.Errorf("%q: %w", path, err)
				}
				localSeen[name] = srcName
				allCRDs[name] = crd
			}

			// Accumulate security, notification, providers, and profiles from registry source Katalog.
			accSecurity = mergeKatalogSecurity(accSecurity, m.security)
			accNotification = mergeKatalogNotification(accNotification, m.notification)
			accProviders = append(accProviders, m.providers...)
			merged, err := accProfiles.Merge(m.profiles, fmt.Sprintf("registry:%d", i))
			if err != nil {
				return nil, fmt.Errorf("%q imports.registry[%d]: profiles: %w", path, i, err)
			}
			accProfiles = merged
			accSpecImports = append(accSpecImports, m.specImports...)
			mergedNotes, err := accNotes.MergeImport(m.notes, fmt.Sprintf("registry:%d", i), notesSeen)
			if err != nil {
				return nil, fmt.Errorf("%q imports.registry[%d]: notes: %w", path, i, err)
			}
			accNotes = mergedNotes
			logger.Debug().
				Str("import", fmt.Sprintf("registry:%d", i)).
				Msg("merger: accumulated security, notification, and providers from registry import")
		}
	}

	// ── Step 2: file imports ──────────────────────────────────────────────────
	if doc.Imports != nil {
		for _, fileSrc := range doc.Imports.Files {

			// Resolve environment variable in the URL if needed
			resolved, err := resolveEnvVar(fileSrc.URL)
			if err != nil {
				return nil, fmt.Errorf("%q imports.files: %w", path, err)
			}

			// Resolve relative paths against the Komposer's directory so
			// ork run -f /any/path/komposer.yaml works from any working directory.
			if !filepath.IsAbs(resolved) && !strings.HasPrefix(resolved, "http") {
				resolved = filepath.Join(filepath.Dir(path), resolved)
			}

			// Resolve authentication credentials from environment variables
			auth, err := fileSrc.Auth.Resolve()
			if err != nil {
				return nil, fmt.Errorf("%q imports.files[%q]: auth: %w", path, resolved, err)
			}

			// Load the file — must be a Katalog, not another Komposer
			crds, err := m.loadImportFileWithAuth(path, resolved, auth)
			if err != nil {
				return nil, fmt.Errorf("%q imports.files[%q]: %w", path, resolved, err)
			}

			for name, crd := range crds {
				if err := checkDuplicate(localSeen, name, "file:"+resolved); err != nil {
					return nil, fmt.Errorf("%q: %w", path, err)
				}
				localSeen[name] = "file:" + resolved
				allCRDs[name] = crd
			}

			// Accumulate security, notification, providers, and profiles from this Katalog file import.
			accSecurity = mergeKatalogSecurity(accSecurity, m.security)
			accNotification = mergeKatalogNotification(accNotification, m.notification)
			accProviders = append(accProviders, m.providers...)
			merged, err := accProfiles.Merge(m.profiles, "file:"+resolved)
			if err != nil {
				return nil, fmt.Errorf("%q imports.files[%q]: profiles: %w", path, resolved, err)
			}
			accProfiles = merged
			accSpecImports = append(accSpecImports, m.specImports...)
			mergedNotes, err := accNotes.MergeImport(m.notes, "file:"+resolved, notesSeen)
			if err != nil {
				return nil, fmt.Errorf("%q imports.files[%q]: notes: %w", path, resolved, err)
			}
			accNotes = mergedNotes
			logger.Debug().
				Str("import", "file:"+resolved).
				Msg("merger: accumulated security, notification, and providers from file import")
		}
		// ── Step 3: helm imports ──────────────────────────────────────────────
		for i, helmSrc := range doc.Imports.Helm {
			crds, err := m.loadHelmSource(helmSrc)
			if err != nil {
				return nil, fmt.Errorf("%q imports.helm[%d]: %w", path, i, err)
			}

			srcName := fmt.Sprintf("helm:%s/%s@%s", helmSrc.Repo, helmSrc.Chart, helmSrc.Version)
			for name, crd := range crds {
				if err := checkDuplicate(localSeen, name, srcName); err != nil {
					return nil, fmt.Errorf("%q: %w", path, err)
				}
				localSeen[name] = srcName
				allCRDs[name] = crd
			}

			// Accumulate security, notification, providers, and profiles from this Helm import.
			accSecurity = mergeKatalogSecurity(accSecurity, m.security)
			accNotification = mergeKatalogNotification(accNotification, m.notification)
			accProviders = append(accProviders, m.providers...)
			merged, err := accProfiles.Merge(m.profiles, srcName)
			if err != nil {
				return nil, fmt.Errorf("%q imports.helm[%d]: profiles: %w", path, i, err)
			}
			accProfiles = merged
			accSpecImports = append(accSpecImports, m.specImports...)
			mergedNotes, err := accNotes.MergeImport(m.notes, srcName, notesSeen)
			if err != nil {
				return nil, fmt.Errorf("%q imports.helm[%d]: notes: %w", path, i, err)
			}
			accNotes = mergedNotes
			logger.Debug().
				Str("import", srcName).
				Msg("merger: accumulated security, notification, and providers from helm import")
		}
	}

	// ── Step 4: inline spec.crds — override any source CRD with same name ────
	inlineKey := "inline:" + path
	for name, crd := range doc.Spec.CRDs {
		if name == "" {
			return nil, fmt.Errorf("%q spec.crds: CRD with empty key", path)
		}

		// Duplicate within the same inline block is impossible (map keys).
		// But if same inline key was already recorded, it's a bug.
		if existing, ok := localSeen[name]; ok && existing == inlineKey {
			return nil, fmt.Errorf(
				"%q spec.crds: duplicate CRD %q — each CRD name must be unique",
				path, name,
			)
		}

		crd.Name = name

		// Merge onto source, don't replace
		if base, found := allCRDs[name]; found {
			allCRDs[name] = mergeCRDEntry(base, crd)
			logger.Debug().
				Str("crd", name).
				Str("source", inlineKey).
				Msg("merger: inline override merged onto source entry")
		} else {
			allCRDs[name] = crd
			logger.Debug().
				Str("crd", name).
				Str("source", inlineKey).
				Msg("merger: new CRD from inline spec.crds")
		}

		localSeen[name] = inlineKey
	}
	// Fill KatalogDescription and KatalogVersion fallbacks — if the sub-Katalog had none, use the Komposer's.
	for name, crd := range allCRDs {
		changed := false
		if crd.KatalogDescription == "" && doc.Metadata.Description != "" {
			crd.KatalogDescription = doc.Metadata.Description
			changed = true
		}
		if crd.KatalogVersion == "" && doc.Metadata.Version != "" {
			crd.KatalogVersion = doc.Metadata.Version
			changed = true
		}
		if changed {
			allCRDs[name] = crd
		}
	}

	// Merge Komposer-level restrictions into every CRD (additive).
	protect := doc.Security.NamespaceProtection
	if protect != nil {
		if len(protect.RestrictedNamespaces) > 0 || len(protect.AllowedNamespaces) > 0 {
			for name, crd := range allCRDs {
				crd.RestrictedNamespaces = protect.RestrictedNamespaces.Merge(crd.RestrictedNamespaces)
				crd.AllowedNamespaces = protect.AllowedNamespaces.Merge(crd.AllowedNamespaces)
				allCRDs[name] = crd
			}
		}
	}

	logger.Debug().
		Str("path", path).
		Int("crds", len(allCRDs)).
		Msg("merger: Komposer loaded")

	// This is a komposer
	apiMetadata := apiMetadata{
		APIVersion: doc.APIVersion,
		Kind:       doc.Kind,
		Metadata:   doc.Metadata,
	}

	m.apiMetadata = apiMetadata

	// Merge accumulated source fields with the Komposer's own top-level blocks.
	// Komposer-declared fields win on conflict (non-nil / non-empty override semantics).
	// This ensures all top-level Katalog fields — security, notification, providers —
	// are visible when running `ork generate rbac` or `ork generate configmap`
	// against a Komposer, identical to running against the source Katalogs directly.
	m.security = mergeKatalogSecurity(accSecurity, doc.Security)
	m.notification = mergeKatalogNotification(accNotification, doc.Notification)
	if len(doc.Providers) > 0 {
		m.providers = doc.Providers
	} else {
		m.providers = accProviders
	}
	if doc.Gateway != nil {
		m.gateway = doc.Gateway
	}
	if doc.Publish != nil {
		m.publish = doc.Publish
	}
	if doc.Lifecycle != nil {
		m.lifecycle = doc.Lifecycle
	}
	if doc.Policy != nil {
		m.policy = doc.Policy
	}

	mergedProfiles, err := accProfiles.Merge(doc.Profiles, path)
	if err != nil {
		return nil, fmt.Errorf("%q: profiles: %w", path, err)
	}
	m.profiles = mergedProfiles

	if len(doc.Spec.Imports) > 0 {
		return nil, fmt.Errorf("%q: Komposer does not support spec.imports — declare notes: and profiles: inline to override Katalog-wide settings", path)
	}
	m.specImports = accSpecImports
	mergedNotes, err := doc.Notes.Merge(accNotes, "katalog")
	if err != nil {
		return nil, fmt.Errorf("%q: notes: %w", path, err)
	}
	m.notes = mergedNotes

	logger.Debug().
		Str("path", path).
		Msg("merger: Komposer security, notification, and providers merged from imports and inline")

	return allCRDs, nil
}
