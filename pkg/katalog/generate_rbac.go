package katalog

import (
	"strings"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/children"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Standard verbs for managed resources.
var defaultVerbs = []string{
	"get", "list", "watch", "create", "update", "patch", "delete",
}

// watchVerbs is the minimal verb set for secondary watch resources.
// Watch entries only observe — they never write to watched resources.
var watchVerbs = []string{"get", "list", "watch"}

// rbacVerbsFor returns the appropriate verb set for a built-in resource.
//
// Roles and ClusterRoles require two extra verbs beyond standard CRUD:
//   - "escalate": allows creating/updating a Role or ClusterRole that grants
//     permissions the Orkestra SA does not already hold. Without it Kubernetes
//     blocks any attempt to provision a role with a broader permission set.
//   - "bind": allows creating a RoleBinding or ClusterRoleBinding that
//     references a Role/ClusterRole whose permissions the SA doesn't hold.
//     Without it the binding creation is blocked even after the role exists.
//
// Both verbs are required whenever the operator provisions RBAC on behalf of
// tenant service accounts (e.g. via clusterRoles:/roles: in onCreate).
// They are absent from the generated bundle when no Roles or ClusterRoles are
// detected in the Katalog, preserving least-privilege for all other operators.
func rbacVerbsFor(group, plural string) []string {
	if group == "rbac.authorization.k8s.io" && (plural == "roles" || plural == "clusterroles") {
		return append(defaultVerbs, "escalate", "bind")
	}
	return defaultVerbs
}

func (k *Katalog) GenerateRBACRules() []rbacv1.PolicyRule {
	var rules []rbacv1.PolicyRule

	// ───────────────────────────────────────────────
	// Base RBAC (always required)
	// ───────────────────────────────────────────────
	rules = append(rules,
		rbacv1.PolicyRule{
			APIGroups: []string{"coordination.k8s.io"},
			Resources: []string{"leases"},
			Verbs:     []string{"get", "create", "update"},
		},
		rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"events"},
			Verbs:     []string{"create", "patch"},
		},
	)

	// ───────────────────────────────────────────────
	// Admission webhook RBAC (conditional)
	// ───────────────────────────────────────────────
	if k.NeedsCertificates() {
		webhookResources := k.WebhookResources()

		if len(webhookResources) > 0 {
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: []string{"admissionregistration.k8s.io"},
				Resources: webhookResources,
				Verbs:     defaultVerbs,
			})
		}

		// ───────────────────────────────────────────────
		// Needs permission to create and manage secret
		// ───────────────────────────────────────────────
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     defaultVerbs,
		})
	}

	// ───────────────────────────────────────────────
	// Deletion protection — namespace labeling
	// ───────────────────────────────────────────────
	// ensureNamespaceLabeled patches the Orkestra namespace with deletion-protection
	// labels at startup so the admission webhook's ObjectSelector matches it.
	// Requires get (to confirm the namespace exists) and patch (to apply labels).
	if k.IsDeletionProtectionEnabled() {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Verbs:     []string{"get", "patch"},
		})

		// Custom CRs registered in the deletion-protection webhook need GET so
		// the gateway can read the instance and check its protection label/annotation.
		rules = append(rules, k.customResourceDeletionProtectionRBACRules()...)
	}

	// ───────────────────────────────────────────────
	// CRD RBAC (main + status)
	// ───────────────────────────────────────────────
	for _, crd := range k.Enabled() {
		if crd.APITypes.Group == "" || crd.APITypes.Plural == "" {
			if !crd.IsBuiltInType() {
				continue
			}
		}

		// Main resource
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{crd.APITypes.Group},
			Resources: []string{crd.APITypes.Plural},
			Verbs:     defaultVerbs,
		})

		// Status subresource
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{crd.APITypes.Group},
			Resources: []string{crd.APITypes.Plural + "/status"},
			Verbs:     []string{"get", "update", "patch"},
		})

		// CRD patching with CA bundle and watching for MODIFIED event by housekeeper
		if crd.Conversion != nil && crd.UpdateCRDCaBundle() {
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups:     []string{"apiextensions.k8s.io"},
				Resources:     []string{"customresourcedefinitions"},
				Verbs:         []string{"patch", "watch"},
				ResourceNames: []string{crd.APITypes.Plural + "." + crd.APITypes.Group},
			})
		}

		// Cross Declarations with secret references
		if crd.HasCrossSecretRef() {
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: []string{},
				Resources: []string{"secrets"},
				Verbs:     []string{"get"},
			})
		}
	}

	// ───────────────────────────────────────────────
	// Typed‑mode RBAC (hooks or constructor)
	// ───────────────────────────────────────────────
	for _, crd := range k.Enabled() {

		// Hooks-managed resources
		if crd.WithHookManagedResources() {
			for _, r := range crd.HookManagedResources() {
				gvr, ok := k.ResolveGVR(r)
				if !ok {
					// optional: skip or log
					continue
				}
				rules = append(rules, rbacv1.PolicyRule{
					APIGroups: []string{gvr.Group},
					Resources: []string{gvr.Resource},
					Verbs:     defaultVerbs,
				})
			}
		}

		// Constructor-managed resources
		if crd.WithConstructorManagedResources() {
			for _, r := range crd.ConstructorManagedResources() {
				gvr, ok := k.ResolveGVR(r)
				if !ok {
					// optional: skip or log
					continue
				}
				rules = append(rules, rbacv1.PolicyRule{
					APIGroups: []string{gvr.Group},
					Resources: []string{gvr.Resource},
					Verbs:     defaultVerbs,
				})
			}
		}

		// Watch-entry resources — read-only
		for _, w := range crd.WatchEntries() {
			gvr, ok := k.ResolveGVR(w.ToManagedResource())
			if !ok {
				continue
			}
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: []string{gvr.Group},
				Resources: []string{gvr.Resource},
				Verbs:     watchVerbs,
			})
		}
	}

	// ───────────────────────────────────────────────
	// Built-in resource RBAC — driven by builtInRegistry.
	// Any entry with a Detect function is a candidate; emit a rule when
	// at least one enabled CRD actually uses that resource.
	// ───────────────────────────────────────────────
	for _, b := range children.AllBuiltInKindDefs() {
		if b.Detect == nil {
			continue
		}
		if k.anyDetects(b.Detect) {
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: []string{b.Group},
				Resources: []string{b.Plural},
				Verbs:     rbacVerbsFor(b.Group, b.Plural),
			})
		}
	}

	// ───────────────────────────────────────────────
	// Custom resource RBAC — derived from onCreate/onReconcile custom: entries.
	// Built-in kinds are covered above; third-party CRDs (cert-manager, ArgoCD,
	// Crossplane, etc.) are not in the built-in registry and must be emitted here.
	// ───────────────────────────────────────────────
	rules = append(rules, k.customResourceRBACRules()...)

	return rules
}

// WebhookResources returns the list of admission webhook resources that Orkestra
// needs to manage when webhooks/certificates are required.
//
// Rules:
//   - validatingwebhookconfigurations is required for deletion protection,
//     namespace protection, or any validation rules.
//   - mutatingwebhookconfigurations is required only when mutation rules exist.
//   - conversion webhooks are handled separately and do not require these resources.
func (k *Katalog) WebhookResources() []string {
	var resources []string

	// validatingwebhookconfigurations is needed for:
	// - deletion protection
	// - namespace protection
	// - validation rules (HasValidationRules)
	if k.IsDeletionProtectionEnabled() || k.IsNamespaceProtectionEnabled() || k.HasValidationRules() {
		resources = append(resources, "validatingwebhookconfigurations")
	}

	// mutatingwebhookconfigurations is only needed when mutation rules exist
	if k.HasMutationRules() {
		resources = append(resources, "mutatingwebhookconfigurations")
	}

	return resources
}

// HasExternalSecretRefs returns true when any enabled CRD declares an external call
// with auth.secretRef. Used to gate automatic secrets-get RBAC generation.
func (k *Katalog) HasExternalSecretRefs() bool {
	for _, crd := range k.enabledCRDs {
		var calls []orktypes.ExternalCallSpec
		if crd.OperatorBox.OnReconcile != nil {
			calls = append(calls, crd.OperatorBox.OnReconcile.External...)
		}
		if crd.OperatorBox.OnCreate != nil {
			calls = append(calls, crd.OperatorBox.OnCreate.External...)
		}
		calls = append(calls, crd.HooksExternal()...)
		if crd.Validation != nil {
			calls = append(calls, crd.Validation.External...)
		}
		if crd.Mutation != nil {
			calls = append(calls, crd.Mutation.External...)
		}
		for _, call := range calls {
			if call.HasSecretRef() {
				return true
			}
		}
	}
	return false
}

// GenerateRuntimeRBACRules returns the RBAC rules required by the runtime reconciler process.
// This is GenerateRBACRules() minus the NeedsCertificates() block (webhook/secrets),
// minus the IsDeletionProtectionEnabled() namespace block, and minus CRD CA-bundle patch rules.
func (k *Katalog) GenerateRuntimeRBACRules() []rbacv1.PolicyRule {
	var rules []rbacv1.PolicyRule

	// ───────────────────────────────────────────────
	// Base RBAC (always required)
	// ───────────────────────────────────────────────
	rules = append(rules,
		rbacv1.PolicyRule{
			APIGroups: []string{"coordination.k8s.io"},
			Resources: []string{"leases"},
			Verbs:     []string{"get", "create", "update"},
		},
		rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"events"},
			Verbs:     []string{"create", "patch"},
		},
	)

	// ───────────────────────────────────────────────
	// External auth.secretRef — secrets get
	// ───────────────────────────────────────────────
	if k.HasExternalSecretRefs() {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"get"},
		})
	}

	// ───────────────────────────────────────────────
	// CRD RBAC (main + status, no CA bundle patch)
	// ───────────────────────────────────────────────
	for _, crd := range k.Enabled() {
		if crd.APITypes.Group == "" || crd.APITypes.Plural == "" {
			if !crd.IsBuiltInType() {
				continue
			}
		}

		// Main resource
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{crd.APITypes.Group},
			Resources: []string{crd.APITypes.Plural},
			Verbs:     defaultVerbs,
		})

		// Status subresource
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{crd.APITypes.Group},
			Resources: []string{crd.APITypes.Plural + "/status"},
			Verbs:     []string{"get", "update", "patch"},
		})
	}

	// ───────────────────────────────────────────────
	// Typed‑mode RBAC (hooks or constructor)
	// ───────────────────────────────────────────────
	for _, crd := range k.Enabled() {

		// Hooks-managed resources
		if crd.WithHookManagedResources() {
			for _, r := range crd.HookManagedResources() {
				gvr, ok := k.ResolveGVR(r)
				if !ok {
					continue
				}
				rules = append(rules, rbacv1.PolicyRule{
					APIGroups: []string{gvr.Group},
					Resources: []string{gvr.Resource},
					Verbs:     defaultVerbs,
				})
			}
		}

		// Constructor-managed resources
		if crd.WithConstructorManagedResources() {
			for _, r := range crd.ConstructorManagedResources() {
				gvr, ok := k.ResolveGVR(r)
				if !ok {
					continue
				}
				rules = append(rules, rbacv1.PolicyRule{
					APIGroups: []string{gvr.Group},
					Resources: []string{gvr.Resource},
					Verbs:     defaultVerbs,
				})
			}
		}

		// Watch-entry resources — read-only
		for _, w := range crd.WatchEntries() {
			gvr, ok := k.ResolveGVR(w.ToManagedResource())
			if !ok {
				continue
			}
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: []string{gvr.Group},
				Resources: []string{gvr.Resource},
				Verbs:     watchVerbs,
			})
		}
	}

	// ───────────────────────────────────────────────
	// Built-in resource RBAC
	// ───────────────────────────────────────────────
	for _, b := range children.AllBuiltInKindDefs() {
		if b.Detect == nil {
			continue
		}
		if k.anyDetects(b.Detect) {
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: []string{b.Group},
				Resources: []string{b.Plural},
				Verbs:     rbacVerbsFor(b.Group, b.Plural),
			})
		}
	}

	// ───────────────────────────────────────────────
	// Custom resource RBAC
	// ───────────────────────────────────────────────
	rules = append(rules, k.customResourceRBACRules()...)

	return rules
}

// GenerateGatewayRBACRules returns the RBAC rules required by the gateway process
// (webhook server, certificate management, namespace labeling).
func (k *Katalog) GenerateGatewayRBACRules() []rbacv1.PolicyRule {
	if !k.IsGatewayEnabled() {
		return nil
	}

	var rules []rbacv1.PolicyRule

	// ───────────────────────────────────────────────
	// Admission webhook RBAC (conditional)
	// ───────────────────────────────────────────────
	if k.NeedsCertificates() {
		webhookResources := k.WebhookResources()

		if len(webhookResources) > 0 {
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: []string{"admissionregistration.k8s.io"},
				Resources: webhookResources,
				Verbs:     defaultVerbs,
			})
		}

		// Needs permission to create and manage secret
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     defaultVerbs,
		})
	}

	// ───────────────────────────────────────────────
	// Deletion protection — namespace labeling
	// ───────────────────────────────────────────────
	if k.IsDeletionProtectionEnabled() {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Verbs:     []string{"get", "patch"},
		})
	}

	// ───────────────────────────────────────────────
	// Gateway API — secretRef token bootstrap/rotation
	// ───────────────────────────────────────────────
	// get: read existing token; create: self-bootstrap when Secret is absent.
	// Uses the same annotation-based rotation as pkg/runners secrets_once.go.
	if k.HasGatewayAPISecretRefs() {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"get", "create"},
		})
	}

	// ───────────────────────────────────────────────
	// Gateway API — CR create/update/delete/get/list
	// Only CRDs whose default routing is local (serve.cluster absent).
	// CRDs with a remote serve.cluster are handled in GenerateGatewayClusterRBACRules.
	// ───────────────────────────────────────────────
	if k.HasServeEnabled() {
		for _, crd := range k.Enabled() {
			if !crd.ServeEnabled() {
				continue
			}
			if crd.Serve.HasClusters() {
				continue // routed to remote clusters
			}
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: []string{crd.APITypes.Group},
				Resources: []string{crd.APITypes.Plural},
				Verbs:     []string{"get", "list", "create", "update", "patch", "delete"},
			})
			// Schema endpoint reads the CRD object to extract the OpenAPI spec.
			// Scoped to the exact CRD name — least-privilege get only.
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups:     []string{"apiextensions.k8s.io"},
				Resources:     []string{"customresourcedefinitions"},
				Verbs:         []string{"get"},
				ResourceNames: []string{crd.APITypes.Plural + "." + crd.APITypes.Group},
			})
		}
	}

	// ───────────────────────────────────────────────
	// CRD CA bundle patching (conversion webhooks)
	// ───────────────────────────────────────────────
	for _, crd := range k.Enabled() {
		if crd.Conversion != nil && crd.UpdateCRDCaBundle() {
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups:     []string{"apiextensions.k8s.io"},
				Resources:     []string{"customresourcedefinitions"},
				Verbs:         []string{"patch", "watch"},
				ResourceNames: []string{crd.APITypes.Plural + "." + crd.APITypes.Group},
			})
		}
	}

	return rules
}

// GenerateGatewayClusterRBACRules returns the RBAC rules the gateway needs on each
// registered remote cluster. The map key is the cluster name; the value is the
// minimal set of PolicyRules for CRDs statically routed to that cluster.
//
// The second return value lists the kinds of CRDs whose cluster routing uses a
// template expression — those rules are added to every cluster's entry. Callers
// should warn the user and ask them to remove entries for clusters that should not
// have access to template-routed CRDs.
//
// CRDs with no serve.cluster (local fallback) do not appear in the returned map.
// They are handled by GenerateGatewayRBACRules instead.
func (k *Katalog) GenerateGatewayClusterRBACRules() (map[string][]rbacv1.PolicyRule, []string) {
	// Return early if gateway is disabled, serve is disabled, or NO clusters are defined
	if !k.IsGatewayEnabled() || !k.HasServeEnabled() || k.GatewayClustersEmpty() {
		return nil, nil
	}

	clusters := k.GatewayClusters()
	// Initialize a slot for every registered cluster
	clusterRules := make(map[string][]rbacv1.PolicyRule, len(clusters))
	for name := range clusters {
		clusterRules[name] = nil
	}

	var templateKinds []string

	for _, crd := range k.ServeEnabledCRDs() {
		// Build rules for this specific CRD
		crdRules := []rbacv1.PolicyRule{
			{
				APIGroups: []string{crd.APITypes.Group},
				Resources: []string{crd.APITypes.Plural, crd.APITypes.Plural + "/status"},
				Verbs:     []string{"get", "list", "create", "update", "patch", "delete"},
			},
			{
				APIGroups:     []string{"apiextensions.k8s.io"},
				Resources:     []string{"customresourcedefinitions"},
				Verbs:         []string{"get", "list"},
				ResourceNames: []string{crd.APITypes.Plural + "." + crd.APITypes.Group},
			},
		}

		staticTargets := map[string]bool{}
		hasTemplate := false

		// Check clusters defined at CRD level
		if crd.Serve != nil {
			for _, sc := range crd.Serve.Clusters {
				if isTemplate(sc) {
					hasTemplate = true
				} else {
					staticTargets[sc] = true
				}
			}

			// Check clusters defined at target level
			for _, entry := range crd.Serve.Target.Entries {
				for _, tc := range entry.TargetClusters() {
					if isTemplate(tc) {
						hasTemplate = true
					} else {
						staticTargets[tc] = true
					}
				}
			}
		}

		if hasTemplate {
			templateKinds = append(templateKinds, crd.APITypes.Kind)
			for name := range clusters {
				clusterRules[name] = append(clusterRules[name], crdRules...)
			}
		} else if len(staticTargets) > 0 {
			// Assign rules only to static targets
			for clusterName := range staticTargets {
				if _, ok := clusterRules[clusterName]; ok {
					clusterRules[clusterName] = append(clusterRules[clusterName], crdRules...)
				}
			}
		}
		// If no clusters and no targets (local only), skip - no RBAC needed
	}

	// Drop clusters that received no rules
	for name, rules := range clusterRules {
		if len(rules) == 0 {
			delete(clusterRules, name)
		}
	}

	return clusterRules, templateKinds
}

// ResolveGVR resolves a ManagedResource into a concrete GroupVersionResource.
//
// Resolution priority (explicit always wins):
//
//  1. Full explicit GVR:
//     - group + version + plural are all provided
//     → use them directly.
//
//  2. Explicit group + version (plural omitted)
//     → infer plural as strings.ToLower(kind) + "s".
//
//  3. APIVersion + plural:
//     - apiVersion: "group/version"
//     - plural provided
//     → parse apiVersion and use provided plural.
//
//  4. APIVersion only:
//     - apiVersion: "group/version"
//     - plural omitted
//     → parse apiVersion and infer plural as strings.ToLower(kind) + "s".
//
//  5. Built‑in Kubernetes resource:
//     - kind matches Orkestra's built‑in registry
//     → use children.GVRForBuiltIn(kind).
//
//  6. Otherwise:
//     → resolution fails and (GVR{}, false) is returned.
//
// This ensures:
//   - Explicit declarations always override inference.
//   - Custom resources can be fully specified without guessing.
//   - Built‑ins remain simple (kind‑only).
//   - RBAC generation remains deterministic and zero‑footprint safe.
func (k *Katalog) ResolveGVR(r domain.ManagedResource) (schema.GroupVersionResource, bool) {
	// ───────────────────────────────────────────────
	// 1. Full explicit GVR: group + version + plural
	// ───────────────────────────────────────────────
	if r.Group != "" && r.Version != "" && r.Plural != "" {
		return schema.GroupVersionResource{
			Group:    r.Group,
			Version:  r.Version,
			Resource: r.Plural,
		}, true
	}

	// ───────────────────────────────────────────────
	// 2. Explicit group + version, infer plural
	// ───────────────────────────────────────────────
	if r.Group != "" && r.Version != "" {
		return schema.GroupVersionResource{
			Group:    r.Group,
			Version:  r.Version,
			Resource: strings.ToLower(r.Kind) + "s",
		}, true
	}

	// ───────────────────────────────────────────────
	// 3. APIVersion + plural
	// ───────────────────────────────────────────────
	if r.APIVersion != "" && r.Plural != "" {
		gv, err := schema.ParseGroupVersion(r.APIVersion)
		if err != nil {
			return schema.GroupVersionResource{}, false
		}
		return schema.GroupVersionResource{
			Group:    gv.Group,
			Version:  gv.Version,
			Resource: r.Plural,
		}, true
	}

	// ───────────────────────────────────────────────
	// 4. APIVersion only, infer plural
	// ───────────────────────────────────────────────
	if r.APIVersion != "" {
		gv, err := schema.ParseGroupVersion(r.APIVersion)
		if err != nil {
			return schema.GroupVersionResource{}, false
		}
		return schema.GroupVersionResource{
			Group:    gv.Group,
			Version:  gv.Version,
			Resource: strings.ToLower(r.Kind) + "s",
		}, true
	}

	// ───────────────────────────────────────────────
	// 5. Built‑in resource
	// ───────────────────────────────────────────────
	if gvr, ok := children.GVRForBuiltIn(r.Kind); ok {
		return gvr, true
	}

	// ───────────────────────────────────────────────
	// 6. Could not resolve
	// ───────────────────────────────────────────────
	return schema.GroupVersionResource{}, false
}

// GeneratePerCRDRBACRules returns the RBAC rules attributed to each enabled CRD.
// The map key is the CRD name. Excludes system-level rules (leases, events,
// secrets, namespaces, webhook configurations) — use GenerateRuntimeRBACRules /
// GenerateGatewayRBACRules for those.
func (k *Katalog) GeneratePerCRDRBACRules() map[string][]rbacv1.PolicyRule {
	result := make(map[string][]rbacv1.PolicyRule, k.Len())

	for name, crd := range k.Enabled() {
		var rules []rbacv1.PolicyRule

		if crd.APITypes.Group != "" || crd.IsBuiltInType() {
			if crd.APITypes.Plural != "" {
				rules = append(rules,
					rbacv1.PolicyRule{
						APIGroups: []string{crd.APITypes.Group},
						Resources: []string{crd.APITypes.Plural},
						Verbs:     defaultVerbs,
					},
					rbacv1.PolicyRule{
						APIGroups: []string{crd.APITypes.Group},
						Resources: []string{crd.APITypes.Plural + "/status"},
						Verbs:     []string{"get", "update", "patch"},
					},
				)
			}
			if crd.Conversion != nil && crd.UpdateCRDCaBundle() {
				rules = append(rules, rbacv1.PolicyRule{
					APIGroups:     []string{"apiextensions.k8s.io"},
					Resources:     []string{"customresourcedefinitions"},
					Verbs:         []string{"patch"},
					ResourceNames: []string{crd.APITypes.Plural + "." + crd.APITypes.Group},
				})
			}
		}

		if crd.WithHookManagedResources() {
			for _, r := range crd.HookManagedResources() {
				if gvr, ok := k.ResolveGVR(r); ok {
					rules = append(rules, rbacv1.PolicyRule{
						APIGroups: []string{gvr.Group},
						Resources: []string{gvr.Resource},
						Verbs:     defaultVerbs,
					})
				}
			}
		}

		if crd.WithConstructorManagedResources() {
			for _, r := range crd.ConstructorManagedResources() {
				if gvr, ok := k.ResolveGVR(r); ok {
					rules = append(rules, rbacv1.PolicyRule{
						APIGroups: []string{gvr.Group},
						Resources: []string{gvr.Resource},
						Verbs:     defaultVerbs,
					})
				}
			}
		}

		// Watch-entry resources — read-only
		for _, w := range crd.WatchEntries() {
			if gvr, ok := k.ResolveGVR(w.ToManagedResource()); ok {
				rules = append(rules, rbacv1.PolicyRule{
					APIGroups: []string{gvr.Group},
					Resources: []string{gvr.Resource},
					Verbs:     watchVerbs,
				})
			}
		}

		for _, b := range children.AllBuiltInKindDefs() {
			if b.Detect != nil && b.Detect(crd) {
				rules = append(rules, rbacv1.PolicyRule{
					APIGroups: []string{b.Group},
					Resources: []string{b.Plural},
					Verbs:     defaultVerbs,
				})
			}
		}

		for _, rule := range customRBACRulesForCRD(crd) {
			rules = append(rules, rule)
		}

		result[name] = rules
	}

	return result
}

// customResourceRBACRules collects RBAC rules for all third-party CRDs declared
// in onCreate/onReconcile custom: blocks across every enabled CRD.
// Built-in Kubernetes kinds are already covered by the builtInRegistry loop;
// this handles everything else (cert-manager, ArgoCD, Crossplane, etc.).
func (k *Katalog) customResourceRBACRules() []rbacv1.PolicyRule {
	var rules []rbacv1.PolicyRule
	seen := make(map[string]bool)
	for _, crd := range k.Enabled() {
		for _, rule := range customRBACRulesForCRD(crd) {
			key := strings.Join(rule.APIGroups, ",") + "/" + strings.Join(rule.Resources, ",")
			if !seen[key] {
				seen[key] = true
				rules = append(rules, rule)
			}
		}
	}
	return rules
}

// customRBACRulesForCRD derives RBAC rules from the custom: entries of one CRD's
// onCreate and onReconcile blocks. Each entry's apiVersion + kind is resolved into
// a group + plural via ParseGroupVersion and lowercase+s inference.
func customRBACRulesForCRD(crd orktypes.CRDEntry) []rbacv1.PolicyRule {
	var entries []orktypes.CustomResourceTemplateSource
	if crd.OperatorBox.OnCreate != nil {
		entries = append(entries, crd.OperatorBox.OnCreate.CustomResource...)
	}
	if crd.OperatorBox.OnReconcile != nil {
		entries = append(entries, crd.OperatorBox.OnReconcile.CustomResource...)
	}

	seen := make(map[string]bool)
	var rules []rbacv1.PolicyRule
	for _, entry := range entries {
		if entry.APIVersion == "" || entry.Kind == "" {
			continue
		}
		gv, err := schema.ParseGroupVersion(entry.APIVersion)
		if err != nil {
			continue
		}
		plural := strings.ToLower(entry.Kind) + "s"
		key := gv.Group + "/" + plural
		if seen[key] {
			continue
		}
		seen[key] = true
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{gv.Group},
			Resources: []string{plural},
			Verbs:     defaultVerbs,
		})
	}
	return rules
}

// customResourceDeletionProtectionRBACRules returns gateway RBAC rules granting
// GET on every custom child resource that the deletion-protection webhook
// intercepts. Mirrors customRBACRulesForCRD but uses get-only verbs — the
// gateway reads each object to check its protection label before allowing the DELETE.
func (k *Katalog) customResourceDeletionProtectionRBACRules() []rbacv1.PolicyRule {
	seen := make(map[string]bool)
	var rules []rbacv1.PolicyRule
	for _, crd := range k.Enabled() {
		for _, rule := range customRBACRulesForCRD(crd) {
			key := strings.Join(rule.APIGroups, ",") + "/" + strings.Join(rule.Resources, ",")
			if seen[key] {
				continue
			}
			seen[key] = true
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: rule.APIGroups,
				Resources: rule.Resources,
				Verbs:     []string{"get"},
			})
		}
	}
	return rules
}
