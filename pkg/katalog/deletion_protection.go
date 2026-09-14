// pkg/katalog/deletion_protection.go
//
// Deletion protection — webhook rules and protected CRD name resolution.
//
// Architecture: two-level filtering.
//
//	Level 1 — Webhook rules (DeletionProtectionGVRs):
//	  Intercepts ALL DELETE on customresourcedefinitions and Orkestra deployments.
//	  Must be broad — Kubernetes webhook rules filter by GVR, not by object name.
//
//	Level 2 — Handler (isProtectedCRD):
//	  Narrows to only the CRDs managed by THIS Katalog.
//	  "websites.demo.orkestra.io" from a different operator → allowed.
//	  "cronjobs.demo.orkestra.io" from this Katalog → denied.
//
// When to register the webhook:
//
//	Requires a reachable Kubernetes Service. With ork run the operator runs
//	locally — there is no Service. failurePolicy: Fail would block ALL CRD
//	deletions when unreachable. Only register when running inside the cluster.
package katalog

import (
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/children"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// DeletionProtectionGVRs returns the list of GVRs that the deletion‑protection
// admission webhook should intercept.
//
// This includes:
//   - CRDs managed by this Katalog (broad match; handler filters by name)
//   - Orkestra’s internal control‑plane resources (deployment, service,
//     serviceaccount, configmap, RBAC objects, ingress, etc.)
//
// The internal resources are derived from the built‑ins registry via
// OrkestraInternalGVRs(), ensuring the list is declarative and maintained
// in a single place.
//
// For custom CRDs, the inclusion of their GVRs is controlled by
// CRDEntry.ShouldProtectCRs() (default true). This allows per‑CRD opt‑out
// of instance‑level protection.
//
// When running outside the cluster (e.g. `ork run`), the webhook cannot be
// reached, so no rules are returned.
func (k *Katalog) DeletionProtectionGVRs() []GVREntry {
	if !k.IsDeletionProtectionEnabled() {
		return nil
	}
	if !utils.IsRunningInCluster() {
		return nil
	}

	gvr := append(k.customResourceGVRs(), orkestraInternalGVRs()...)
	return gvr
}

// orkestraInternalGVRs builds the GVREntry list for Orkestra's own control-plane
// resources by iterating over the children built-in registry and filtering on
// the OrkestraInternal flag.
func orkestraInternalGVRs() []GVREntry {
	var out []GVREntry
	for _, b := range children.AllBuiltInKindDefs() {
		if !b.OrkestraInternal {
			continue
		}
		out = append(out, GVREntry{
			Key:        fmt.Sprintf("%s/%s/%s", b.Group, b.Version, b.Plural),
			Group:      b.Group,
			Version:    b.Version,
			Resource:   b.Plural,
			Operations: []string{"DELETE"},
		})
	}
	return out
}

// customResourceGVRs returns the list of GVRs for all custom resource instances
// managed by this Katalog that should be protected at the instance level.
//
// The returned slice is intended to be added to the second webhook in
// registerDeletionProtectionWebhook (the "protect.resources" webhook).
// That webhook intercepts DELETE requests on these GVRs and the handler checks
// whether the specific instance has the label `orkestra.io/deletion-protection=true`.
//
// Built-in resource types (e.g., ConfigMap, Deployment, Namespace) are excluded
// from this list — they are handled separately via the built‑in registry
// (OrkestraInternal flag) and appear in orkestraInternalGVRs().
//
// Per‑CRD control: a custom resource is added only if
//   - Global deletion protection is enabled
//   - The CRD is fully specified (plural, group)
//   - CRDEntry.ShouldProtectCRs() returns true
//
// This allows administrators to opt out of instance protection for specific CRDs
// via the Katalog's per‑CRD `deletionProtection.protectCRs: false` override.
//
// When running outside the cluster (e.g. `ork run`), the webhook is not reachable
// and this function returns nil (via the caller's guard).
//
// Example:
//
//	For a Katalog enabling "websites.demo.orkestra.io" with protectCRs=true:
//	[
//	  {
//	    Key: "demo.orkestra.io/v1, Resource=websites",
//	    Group: "demo.orkestra.io",
//	    Version: "v1",
//	    Resource: "websites",
//	    Operations: ["DELETE"]
//	  }
//	]
//
// Important: This function does NOT protect the CRD definitions themselves.
// Those are protected separately by the first webhook (CRD protection) using
// DeletionProtectedCRDNames(), which respects ShouldProtectCRD().
func (k *Katalog) customResourceGVRs() []GVREntry {
	seen := make(map[string]bool)
	var gvrList []GVREntry

	// Owner CRDs managed by this Katalog.
	for _, crd := range k.enabledCRDs {
		if !crd.ShouldProtectCRs() {
			continue
		}
		if crd.APITypes.Plural == "" || crd.APITypes.Group == "" {
			continue
		}
		gvr := crd.GVR()
		key := gvr.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		gvrList = append(gvrList, GVREntry{
			Key:        key,
			Group:      gvr.Group,
			Version:    gvr.Version,
			Resource:   gvr.Resource,
			Operations: []string{"DELETE"},
		})
	}

	// Custom children declared in onCreate/onReconcile custom blocks.
	// Respects the parent CRD's ShouldProtectCRs() — if the parent opts out,
	// its children are not registered either.
	for _, crd := range k.enabledCRDs {
		if !crd.ShouldProtectCRs() {
			continue
		}
		gvrList = append(gvrList, customChildGVRs(crd, seen)...)
	}

	return gvrList
}

// customChildGVRs derives GVREntries from the custom: blocks of one CRD's
// onCreate and onReconcile, skipping any already in seen.
func customChildGVRs(crd orktypes.CRDEntry, seen map[string]bool) []GVREntry {
	var entries []orktypes.CustomResourceTemplateSource
	if crd.OperatorBox.OnCreate != nil {
		entries = append(entries, crd.OperatorBox.OnCreate.CustomResource...)
	}
	if crd.OperatorBox.OnReconcile != nil {
		entries = append(entries, crd.OperatorBox.OnReconcile.CustomResource...)
	}

	var out []GVREntry
	for _, entry := range entries {
		if entry.APIVersion == "" || entry.Kind == "" {
			continue
		}
		gv, err := schema.ParseGroupVersion(entry.APIVersion)
		if err != nil {
			continue
		}
		plural := strings.ToLower(entry.Kind) + "s"
		gvr := schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: plural}
		key := gvr.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, GVREntry{
			Key:        key,
			Group:      gv.Group,
			Version:    gv.Version,
			Resource:   plural,
			Operations: []string{"DELETE"},
		})
	}
	return out
}

// DeletionProtectedCRDNames returns the set of CRD full names (plural.group)
// managed by this Katalog that should be protected at the **CRD type level**.
// e.g. {"cronjobs.demo.orkestra.io": {}}
//
// Used by the /deletion-protection handler for name‑based filtering when a DELETE
// request arrives on the CRD endpoint. A CRD not in this set is allowed through
// even though the webhook intercepted it.
//
// Per‑CRD control: a CRD name is included only if:
//   - Global deletion protection is enabled
//   - The CRD is not a built‑in (only custom CRDs can be protected at type level)
//   - CRDEntry.ShouldProtectCRD() returns true
//
// This allows administrators to opt out of CRD type protection for specific CRDs
// via the Katalog's per‑CRD `deletionProtection.protectCRD: false` override.
//
// When running outside the cluster (e.g. `ork run`), the webhook cannot be
// reached, so no protection is guaranteed.
func (k *Katalog) DeletionProtectedCRDNames() map[string]struct{} {
	if !k.IsDeletionProtectionEnabled() {
		return nil
	}
	if !utils.IsRunningInCluster() {
		return nil
	}
	names := make(map[string]struct{}, k.Len())

	// Owner CRDs managed by this Katalog.
	for _, crd := range k.enabledCRDs {
		if crd.IsBuiltIn {
			continue
		}
		if !crd.ShouldProtectCRD() {
			continue
		}
		if crd.APITypes.Plural != "" && crd.APITypes.Group != "" {
			names[crd.APITypes.Plural+"."+crd.APITypes.Group] = struct{}{}
		}
	}

	// Custom children declared in onCreate/onReconcile custom blocks.
	// Protecting CR instances without protecting the CRD type is incomplete —
	// deleting the CRD cascades all instances regardless of instance-level protection.
	// Respects the parent CRD's ShouldProtectCRD() — if the parent opts out,
	// its children's CRD types are not protected either.
	for _, crd := range k.enabledCRDs {
		if !crd.ShouldProtectCRD() {
			continue
		}
		var entries []orktypes.CustomResourceTemplateSource
		if crd.OperatorBox.OnCreate != nil {
			entries = append(entries, crd.OperatorBox.OnCreate.CustomResource...)
		}
		if crd.OperatorBox.OnReconcile != nil {
			entries = append(entries, crd.OperatorBox.OnReconcile.CustomResource...)
		}
		for _, entry := range entries {
			if entry.APIVersion == "" || entry.Kind == "" {
				continue
			}
			gv, err := schema.ParseGroupVersion(entry.APIVersion)
			if err != nil || gv.Group == "" {
				continue
			}
			plural := strings.ToLower(entry.Kind) + "s"
			names[plural+"."+gv.Group] = struct{}{}
		}
	}

	return names
}
