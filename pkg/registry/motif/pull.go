// pkg/motif/pull.go
//
// PullImport fetches a motif OCI artifact to the local registry cache.
// Used as the pre-pull step before validate, generate, or run commands.
//
// Authoring-time only: every caller (ork pull, ork e2e) is already
// !runtime && !gateway tagged. LoadImport in loader.go (not gated) is the
// path pkg/katalog's motif-import expansion actually uses, and it goes
// through merger.PullMotifToDir, not this file.

//go:build !runtime && !gateway

package motif

import (
	"context"
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/registry"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// PullImport fetches a motif artifact into the registry cache
// (~/.orkestra/registry/...) so subsequent loads are served from disk.
// Non-OCI refs (file paths, git URLs) are silently skipped.
// Resolution mirrors LoadImport exactly, including bare-name → default registry expansion.
func PullImport(imp *orktypes.MotifImport) error {
	ref := strings.TrimSpace(imp.Motif)

	if registry.IsFilePath(ref) || isGitURL(ref) {
		return nil
	}

	oci := imp.OCI
	if registry.IsOCIRef(ref) {
		oci = true
		ref = registry.CleanOCIRef(ref)
	}

	// Bare name → resolve against default motif registry.
	var resolved *registry.Ref
	if !oci && !registry.LooksLikeFullRef(ref) {
		r, err := registry.ResolveForKind(ref, registry.MotifKind)
		if err != nil {
			return fmt.Errorf("motif %q: resolving reference: %w", imp.Motif, err)
		}
		resolved = r
	} else if oci {
		cleanURL, version := resolveMotifRef(ref, imp.Version, true)
		r, err := registry.Resolve(fmt.Sprintf("%s:%s", cleanURL, version))
		if err != nil {
			return fmt.Errorf("motif %q: resolving reference: %w", imp.Motif, err)
		}
		resolved = r
	} else {
		return nil // full ref without oci: true — not an OCI import
	}

	if resolved.IsCached() {
		return nil
	}

	client, err := registry.NewClient()
	if err != nil {
		return fmt.Errorf("motif %q: initializing client: %w", imp.Motif, err)
	}
	if _, err := client.Pull(context.Background(), resolved, false); err != nil {
		return fmt.Errorf("motif %q: pull failed: %w", imp.Motif, err)
	}
	return nil
}
