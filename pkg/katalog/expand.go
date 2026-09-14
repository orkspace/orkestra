package katalog

import (
	"fmt"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// expandIncludes expands all include declarations at katalog and CRD level.
func (k *Katalog) expandIncludes() error {
	if k.Empty() {
		return fmt.Errorf("katalog is empty")
	}
	// ── Katalog Level ───────────────────────────────────────────────────────────────────

	if err := orktypes.ExpandNotesInclude(&k.Notes, k.katalogDir); err != nil {
		return fmt.Errorf("notes: %w", err)
	}
	if err := orktypes.ExpandProfileInclude(&k.Profiles, k.katalogDir); err != nil {
		return fmt.Errorf("profiles: %w", err)
	}
	if err := orktypes.ExpandGatewayAPIAuthInclude(k.Gateway, k.katalogDir); err != nil {
		return fmt.Errorf("gateway API auth: %w", err)
	}
	if err := orktypes.ExpandGatewayWebhookIncludes(k.Gateway, k.katalogDir); err != nil {
		return fmt.Errorf("gateway.webhooks: %w", err)
	}
	if err := orktypes.ExpandGatewayClustersInclude(k.Gateway, k.katalogDir); err != nil {
		return fmt.Errorf("gateway.clusters: %w", err)
	}

	// ── CRD Level ───────────────────────────────────────────────────────────────────

	for name, entry := range k.enabledCRDs {
		// Expand serve.include before enrichment so field hints are fully resolved.
		if err := populateAllServeFieldsFromInclude(&entry, k.katalogDir); err != nil {
			return fmt.Errorf("CRD %q: %w", name, err)
		}

		// Expand validation.include, mutation.include, conversion.include and status.include.
		if err := populateValidationRulesFromInclude(&entry, k.katalogDir); err != nil {
			return fmt.Errorf("CRD %q: %w", name, err)
		}
		if err := populateMutationRulesFromInclude(&entry, k.katalogDir); err != nil {
			return fmt.Errorf("CRD %q: %w", name, err)
		}
		if err := populateConversionPathsFromInclude(&entry, k.katalogDir); err != nil {
			return fmt.Errorf("CRD %q: %w", name, err)
		}
		if err := populateStatusFieldsFromInclude(&entry, k.katalogDir); err != nil {
			return fmt.Errorf("CRD %q: %w", name, err)
		}
		if err := orktypes.PopulateExternalCallsFromInclude(&entry, k.katalogDir); err != nil {
			return fmt.Errorf("CRD %q: %w", name, err)
		}
		if err := populateObserveInclude(&entry, k.katalogDir); err != nil {
			return fmt.Errorf("CRD %q: %w", name, err)
		}
		if err := populateReconcilerFromInclude(&entry, k.katalogDir); err != nil {
			return fmt.Errorf("CRD %q: %w", name, err)
		}
	}
	return nil
}
