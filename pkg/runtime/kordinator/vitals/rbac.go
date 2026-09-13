// pkg/runtime/kordinator/vitals/crd_rbac_health.go
package vitals

import (
	"fmt"
	"sort"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

type RBACInfo struct {
	Rules      []RBACRule `json:"rules"`
	Summary    string     `json:"summary"`
	TotalRules int        `json:"totalRules"`
}

type RBACRule struct {
	APIGroups []string `json:"apiGroups,omitempty"`
	Resources []string `json:"resources,omitempty"`
	Verbs     []string `json:"verbs,omitempty"`
	// Human-readable description
	Description string `json:"description,omitempty"`
}

func generateRBACInfo(crd orktypes.CRDEntry, v crdDisplayValues) RBACInfo {
	rules := []RBACRule{}

	// 1. CRD itself
	if !crd.IsBuiltInType() {
		rules = append(rules, RBACRule{
			APIGroups:   []string{crd.APITypes.Group},
			Resources:   []string{crd.APITypes.Plural},
			Verbs:       []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			Description: fmt.Sprintf("Manage %s custom resources", crd.Name),
		})

		// 2. Status subresource if needed
		rules = append(rules, RBACRule{
			APIGroups:   []string{crd.APITypes.Group},
			Resources:   []string{crd.APITypes.Plural + "/status"},
			Verbs:       []string{"get", "update", "patch"},
			Description: fmt.Sprintf("Update status of %s resources", crd.Name),
		})
	}
	box := crd.OperatorBox

	// 3. Resources from declarative templates
	if box.OnCreate != nil {
		resourceRules := extractResourceRules(box.OnCreate)
		rules = append(rules, resourceRules...)
	}

	// 4. Resources from onReconcile
	if box.OnReconcile != nil {
		resourceRules := extractResourceRules(box.OnReconcile)
		rules = append(rules, resourceRules...)
	}

	// 5. Resources from onDelete
	if box.OnDelete != nil {
		resourceRules := extractResourceRules(box.OnDelete)
		rules = append(rules, resourceRules...)
	}

	// 6. Deduplicate rules
	rules = deduplicateRules(rules)

	summary := generateSummary(rules)

	return RBACInfo{
		Rules:      rules,
		Summary:    summary,
		TotalRules: len(rules),
	}
}

func extractResourceRules(t *orktypes.HookTemplates) []RBACRule {
	rules := []RBACRule{}

	// Deployment
	if len(t.Deployments) > 0 {
		rules = append(rules, RBACRule{
			APIGroups:   []string{"apps"},
			Resources:   []string{"deployments"},
			Verbs:       []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			Description: "Manage Deployments (create, update, delete)",
		})
	}

	// Service
	if len(t.Services) > 0 {
		rules = append(rules, RBACRule{
			APIGroups:   []string{""},
			Resources:   []string{"services"},
			Verbs:       []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			Description: "Manage Services (create, update, delete)",
		})
	}

	// ConfigMap
	if len(t.ConfigMaps) > 0 {
		rules = append(rules, RBACRule{
			APIGroups:   []string{""},
			Resources:   []string{"configmaps"},
			Verbs:       []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			Description: "Manage ConfigMaps",
		})
	}

	// Secret
	if len(t.Secrets) > 0 {
		rules = append(rules, RBACRule{
			APIGroups:   []string{""},
			Resources:   []string{"secrets"},
			Verbs:       []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			Description: "Manage Secrets (sensitive data)",
		})
	}

	// Job
	if len(t.Jobs) > 0 {
		rules = append(rules, RBACRule{
			APIGroups:   []string{"batch"},
			Resources:   []string{"jobs"},
			Verbs:       []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			Description: "Manage Jobs (batch processing)",
		})
	}

	// CronJob
	if len(t.CronJobs) > 0 {
		rules = append(rules, RBACRule{
			APIGroups:   []string{"batch"},
			Resources:   []string{"cronjobs"},
			Verbs:       []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			Description: "Manage CronJobs (scheduled tasks)",
		})
	}

	// ServiceAccount
	if len(t.ServiceAccounts) > 0 {
		rules = append(rules, RBACRule{
			APIGroups:   []string{""},
			Resources:   []string{"serviceaccounts"},
			Verbs:       []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			Description: "Manage ServiceAccounts (identity)",
		})
	}

	// Ingress
	if len(t.Ingresses) > 0 {
		rules = append(rules, RBACRule{
			APIGroups:   []string{"networking.k8s.io"},
			Resources:   []string{"ingresses"},
			Verbs:       []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			Description: "Manage Ingresses (HTTP routing)",
		})
	}

	// PersistentVolumeClaim
	if len(t.PersistentVolumeClaims) > 0 {
		rules = append(rules, RBACRule{
			APIGroups:   []string{""},
			Resources:   []string{"persistentvolumeclaims"},
			Verbs:       []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			Description: "Manage PersistentVolumeClaims (storage)",
		})
	}

	return rules
}

func generateSummary(rules []RBACRule) string {
	resourceCount := make(map[string]int)
	for _, rule := range rules {
		for _, resource := range rule.Resources {
			resourceCount[resource]++
		}
	}

	if len(resourceCount) == 0 {
		return "No RBAC permissions required"
	}

	// Extract and sort keys
	keys := make([]string, 0, len(resourceCount))
	for resource := range resourceCount {
		keys = append(keys, resource)
	}
	sort.Strings(keys)

	// Build sorted output
	parts := make([]string, 0, len(keys))
	for _, resource := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", resourceCount[resource], resource))
	}

	return strings.Join(parts, ", ")
}

func deduplicateRules(rules []RBACRule) []RBACRule {
	seen := make(map[string]bool)
	result := []RBACRule{}

	for _, rule := range rules {
		key := fmt.Sprintf("%v|%v|%v", rule.APIGroups, rule.Resources, rule.Verbs)
		if !seen[key] {
			seen[key] = true
			result = append(result, rule)
		}
	}

	return result
}
