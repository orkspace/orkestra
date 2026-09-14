// pkg/katalog/parsek.go
package katalog

import (
	"fmt"

	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/merger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// -----------------------------------------------------------------------------
//
//	YAML Builder
//
// -----------------------------------------------------------------------------

// KomposeRuntimeKatalog composes the runtime Katalog for Orkestra from merged configuration sources.
//
// This function is the central entry point for transforming the declarative Katalog
// (YAML files + overrides) into a fully resolved, validated, and defaulted runtime
// representation (map of CRDEntry). It is called once at Orkestra startup.
//
// The runtime Katalog is used for:
//   - Reconciliation (controller loops, worker counts, resync periods)
//   - Admission webhooks (validation, mutation, deletion protection)
//   - Child resource materialisation (operatorBox onCreate/onReconcile)
//   - Status field resolution (Layer 2 status)
//   - Enrichment (owner, replicasets, pods, HPA)
//
// Processing steps (in order):
//
//  1. Extract merged spec, security, gateway, providers, and metadata from the merger.
//
//  2. Retrieve the list of enabled CRDs (the union of all declarations).
//
//  3. For each CRD entry:
//
//     - If a CRD file path is provided, populate APITypes from the actual CRD
//     (group, version, plural, scope) – this overrides any inline apiTypes.
//
//     - Run enrichment (EnrichCRDEntry) to resolve kind-only built-in declarations
//     (e.g., "Namespace" → group: "", version: "v1", plural: "namespaces").
//
//  4. Expand motif imports declared in operatorBox blocks (motif re-use across CRDs).
//
//  5. Initialize conversion and admission registries (for webhook generation).
//
//  6. Register validation, mutation, and conversion rules from each CRD entry.
//
//  7. Apply defaults (workers, max queue depth, resync, etc.) from the global config.
//
// Returns the final map of CRDEntry keyed by CRD name, ready for the reconciler and
// webhook servers.
//
// Errors are returned for:
//   - Missing or invalid CRD files
//   - Unknown built-in kinds during enrichment
//   - Partially specified apiTypes (e.g., kind+group but no version)
//   - Motif import resolution failures
//   - Default assignment errors
//
// Example:
//
//	runtimeCRDs, err := katalog.KomposeRuntimeKatalog(kfg, merger, "path/to/katalog")
func (k *Katalog) KomposeRuntimeKatalog(
	kfg *konfig.Konfig,
	m *merger.Merger,
	paths ...string,
) (map[string]orktypes.CRDEntry, error) {

	k.Spec = m.ToSpec()
	k.Spec.Imports = m.ToSpecImports()
	k.Security = m.ToSecurity()
	k.Gateway = m.ToGateway()
	k.Publish = m.ToPublish()
	k.Notification = m.ToNotification()
	k.Providers = m.ToProviders()
	k.Profiles = m.ToProfiles()
	k.Notes = m.ToNotes()
	k.projectInfo = m.ToProjectInfo()
	k.enabledCRDs = m.Enabled()           // Enabled CRDs for all operations
	k.metadata = m.APIMetadata().Metadata // Metadata for CLI and health endpoints
	k.lifecycle = m.ToLifecycle()
	k.policy = m.ToPolicy()
	k.APIVersion = m.APIMetadata().APIVersion
	k.Kind = m.APIMetadata().Kind
	k.konfig = kfg
	k.katalogDir = m.FirstEntryDir()

	// Expand all includes
	if err := k.expandIncludes(); err != nil {
		return nil, err
	}

	for name, entry := range k.enabledCRDs {
		// Populate APITypes from crdFile before enrichment so isFullySpecified sees
		// the correct values. crdFile is the source of truth — overwrites any apiTypes.
		// Clear CRDFile afterwards: the runtime (and any serialized bundle) must not
		// reference local filesystem paths that don't exist inside a container.
		if entry.CRDFile != "" {
			k.withCRDFiles = append(k.withCRDFiles, name)
			if err := populateAPITypesFromCRDFile(&entry, k.katalogDir); err != nil {
				return nil, fmt.Errorf("CRD %q: %w", name, err)
			}
			entry.CRDFile = ""
		}

		// Enrich enabled CRDs
		outcome, err := EnrichCRDEntry(&entry)
		if err != nil {
			return nil, err
		}
		entry.EnrichmentOutcome = outcome
		k.enabledCRDs[name] = entry
	}

	// Expand spec.imports — merge profiles into the Katalog-wide ProfileRegistry
	if err := k.expandKatalogImports(); err != nil {
		return nil, err
	}

	// Expand Motif imports declared in each operatorBox (resources, status, admission only)
	if err := k.expandMotifImports(); err != nil {
		return nil, err
	}

	// Apply any sythesized admission rules for:
	// required: true, default and override
	k.applyServeAdmissionSynthesis()

	// initialize conversion registry and admission registry
	k.conversionRegistry = NewInMemoryConversionRegistry()
	k.admissionRegistry = NewInMemoryAdmissionRegistry()

	// now safe to register rules
	for _, entry := range k.enabledCRDs {
		k.admissionRegistry.registerAdmissionRulesFromEntry(entry)
		k.conversionRegistry.registerConversionRulesFromSpec(entry)
	}

	// Apply defaults/GVK so CLI tools (simulate, validate, plan) get the same
	// field values as the runtime without needing to call Validate.
	if err := k.SetDefaults(kfg); err != nil {
		return nil, err
	}
	if err := k.SetGroupVersionKind(); err != nil {
		return nil, err
	}

	// Build all serve enabled CRDs
	k.serveEnabledCRDs = k.BuildServeEnabledCRDs()

	return k.enabledCRDs, nil
}
