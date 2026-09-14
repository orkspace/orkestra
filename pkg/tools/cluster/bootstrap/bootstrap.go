// Package bootstrap provisions least-privilege gateway access on remote clusters.
//
// Given a target cluster's kubeconfig context it creates a ServiceAccount,
// ClusterRole scoped to the katalog's serve-enabled CRDs, ClusterRoleBinding,
// and a long-lived token Secret. It then stores the credential in the gateway
// cluster so the gateway can route applies to the target.
//
// The package is generic: Orkestra is the primary consumer, but any tool that
// needs a scoped ServiceAccount + token on a remote cluster can use Bootstrap
// by setting ClusterEntry.SAName to something other than the default.
package bootstrap

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/utils"
)

// RunOptions holds the runtime settings for a bootstrap run.
// These come from CLI flags, not from the config file.
type RunOptions struct {
	// Namespace is the namespace in the gateway cluster where the credential Secret is written.
	Namespace string
	// DryRun emits what would happen without making any changes.
	DryRun bool
	// EmitRBACOnly returns the ClusterRole rules without connecting to any cluster.
	EmitRBACOnly bool
}

// Result holds what was created and the data needed to render output.
type Result struct {
	Entry            ClusterEntry
	Endpoint         string
	SecretName       string
	SecretNamespace  string
	HasCA            bool
	ClusterRoleRules []rbacv1.PolicyRule
}

// Cluster provisions gateway access on a target cluster for the given ClusterEntry.
// When k is non-nil, ClusterRole rules are derived from the katalog's serve-enabled CRDs.
// When k is nil, rules are taken from entry.Rules. If both are absent, ClusterRole and
// ClusterRoleBinding are skipped — only the SA and token Secret are provisioned.
func Cluster(ctx context.Context, k *katalog.Katalog, entry ClusterEntry, opts RunOptions, log func(string)) (*Result, error) {
	if opts.Namespace == "" {
		opts.Namespace = "default"
	}

	var rules []rbacv1.PolicyRule
	if k != nil {
		rules = buildClusterRoleRules(k)
	} else {
		rules = entry.Rules
	}

	result := &Result{
		Entry:            entry,
		SecretName:       GatewaySecretName(entry),
		SecretNamespace:  opts.Namespace,
		ClusterRoleRules: rules,
	}

	if opts.EmitRBACOnly {
		return result, nil
	}

	targetCS, endpoint, caData, err := connectTarget(entry.Context)
	if err != nil {
		return nil, err
	}
	result.Endpoint = endpoint
	result.HasCA = len(caData) > 0

	if err := applySA(ctx, targetCS, entry, opts.DryRun, log); err != nil {
		return nil, err
	}
	if len(rules) > 0 {
		if err := applyClusterRole(ctx, targetCS, entry, rules, opts.DryRun, log); err != nil {
			return nil, err
		}
		if err := applyClusterRoleBinding(ctx, targetCS, entry, opts.DryRun, log); err != nil {
			return nil, err
		}
	}
	token, err := applyTokenSecret(ctx, targetCS, entry, opts.DryRun, log)
	if err != nil {
		return nil, err
	}

	if opts.DryRun {
		return result, nil
	}

	gatewayCS, err := connectGateway()
	if err != nil {
		return nil, err
	}
	if err := applyCredentialSecret(ctx, gatewayCS, result.SecretName, opts.Namespace, token, caData, log); err != nil {
		return nil, err
	}

	return result, nil
}

// ── internal ──────────────────────────────────────────────────────────────────

func connectTarget(kubectx string) (kubernetes.Interface, string, []byte, error) {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: kubectx},
	).ClientConfig()
	if err != nil {
		return nil, "", nil, fmt.Errorf("cannot load context %q: %w", kubectx, err)
	}

	caData := cfg.TLSClientConfig.CAData
	if len(caData) == 0 && cfg.TLSClientConfig.CAFile != "" {
		caData, err = readLocal(cfg.TLSClientConfig.CAFile)
		if err != nil {
			return nil, "", nil, fmt.Errorf("reading CA file: %w", err)
		}
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, "", nil, fmt.Errorf("connecting to target cluster: %w", err)
	}
	return cs, cfg.Host, caData, nil
}

func connectGateway() (kubernetes.Interface, error) {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot load current context: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("connecting to gateway cluster: %w", err)
	}
	return cs, nil
}

func buildClusterRoleRules(k *katalog.Katalog) []rbacv1.PolicyRule {
	type groupEntry struct{ resources []string }
	groups := map[string]*groupEntry{}
	verbs := []string{"get", "list", "watch", "create", "update", "patch", "delete"}

	for _, crd := range k.ServeEnabledCRDs() {
		gvr := crd.GVR()
		if groups[gvr.Group] == nil {
			groups[gvr.Group] = &groupEntry{}
		}
		groups[gvr.Group].resources = append(groups[gvr.Group].resources, gvr.Resource)
		groups[gvr.Group].resources = append(groups[gvr.Group].resources, gvr.Resource+"/status")
	}

	rules := make([]rbacv1.PolicyRule, 0, len(groups))
	for group, entry := range groups {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{group},
			Resources: utils.DedupStrings(entry.resources),
			Verbs:     verbs,
		})
	}
	return rules
}
