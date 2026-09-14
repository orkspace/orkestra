package api

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/secrets"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ClusterRegistry maps gateway.clusters names to their ready kubeclient.Interface.
// Built at gateway startup from the katalog config; never mutated after construction.
type ClusterRegistry struct {
	clients map[string]kubeclient.Interface
}

// ClientFor returns the client for the named cluster and whether it was found.
func (r *ClusterRegistry) ClientFor(name string) (kubeclient.Interface, bool) {
	if r == nil {
		return nil, false
	}
	c, ok := r.clients[name]
	return c, ok
}

// Len returns the number of registered remote clusters.
func (r *ClusterRegistry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.clients)
}

// BuildClusterRegistry constructs a ClusterRegistry from gateway.clusters config.
// For each entry it reads the credential secret(s) from the local cluster using
// kube, then builds a kubeclient.Interface targeting that remote cluster.
// Returns an empty (non-nil) registry when no clusters are declared.
func BuildClusterRegistry(
	ctx context.Context,
	kat *katalog.Katalog,
	kube kubeclient.Interface,
	ownNS string,
) (*ClusterRegistry, error) {
	reg := &ClusterRegistry{clients: make(map[string]kubeclient.Interface)}
	if kat == nil || kat.Gateway == nil || !kat.Gateway.HasClusters() {
		return reg, nil
	}

	scheme := kube.Scheme()
	for name, cfg := range kat.GatewayClusters() {
		restCfg, err := BuildClusterRestConfig(ctx, cfg, kube, ownNS)
		if err != nil {
			return nil, fmt.Errorf("building client for cluster %q: %w", name, err)
		}
		client, err := kubeclient.NewKubeclientFromConfig(ctx, restCfg, scheme)
		if err != nil {
			return nil, fmt.Errorf("connecting to cluster %q (%s): %w", name, cfg.EndpointURL(), err)
		}
		reg.clients[name] = client
		logger.FromContext(ctx).Info().
			Str("cluster", name).
			Str("endpoint", cfg.EndpointURL()).
			Str("form", cfg.CredentialForm()).
			Msg("gateway cluster client registered")
	}
	return reg, nil
}

// BuildClusterRestConfig builds a *rest.Config for one gateway.clusters entry.
// It reads credential secrets from kube using ownNS as the fallback namespace.
func BuildClusterRestConfig(
	ctx context.Context,
	cfg orktypes.GatewayClusterConfig,
	kube kubeclient.Interface,
	ownNS string,
) (*rest.Config, error) {
	switch cfg.CredentialForm() {
	case "kubeconfig":
		return buildKubeconfigRestConfig(ctx, cfg, kube, ownNS)
	case "token":
		return buildTokenRestConfig(ctx, cfg, kube, ownNS)
	default:
		return nil, fmt.Errorf("no credential form declared")
	}
}

// buildKubeconfigRestConfig reads the kubeconfig from the referenced Secret and
// parses it into a *rest.Config.
func buildKubeconfigRestConfig(
	ctx context.Context,
	cfg orktypes.GatewayClusterConfig,
	kube kubeclient.Interface,
	ownNS string,
) (*rest.Config, error) {
	ref := cfg.SecretRef
	ns := ref.SecretNamespace()
	if ns == "" {
		ns = ownNS
	}
	kubeYAML, err := secrets.ReadSecretKey(ctx, kube.Clientset(), ns, ref.SecretName(), ref.SecretKey())
	if err != nil {
		return nil, fmt.Errorf("kubeconfig %s/%s[%s]: %w", ns, ref.SecretName(), ref.SecretKey(), err)
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeYAML))
	if err != nil {
		return nil, fmt.Errorf("parsing kubeconfig from %s/%s: %w", ns, ref.SecretName(), err)
	}
	return restCfg, nil
}

// buildTokenRestConfig builds a *rest.Config using a bearer token and optional CA cert.
func buildTokenRestConfig(
	ctx context.Context,
	cfg orktypes.GatewayClusterConfig,
	kube kubeclient.Interface,
	ownNS string,
) (*rest.Config, error) {
	tokenRef := cfg.TokenRef
	tokenNS := tokenRef.SecretNamespace()
	if tokenNS == "" {
		tokenNS = ownNS
	}
	token, err := secrets.ReadSecretKey(ctx, kube.Clientset(), tokenNS, tokenRef.SecretName(), tokenRef.SecretKey())
	if err != nil {
		return nil, fmt.Errorf("token %s/%s[%s]: %w", tokenNS, tokenRef.SecretName(), tokenRef.SecretKey(), err)
	}

	restCfg := &rest.Config{
		Host:        cfg.EndpointURL(),
		BearerToken: strings.TrimSpace(token),
	}

	if cfg.IsInsecure() {
		restCfg.TLSClientConfig.Insecure = true
		return restCfg, nil
	}

	caRef := cfg.CARef
	caNS := caRef.SecretNamespace()
	if caNS == "" {
		caNS = ownNS
	}
	caData, err := secrets.ReadSecretKey(ctx, kube.Clientset(), caNS, caRef.SecretName(), caRef.SecretKey())
	if err != nil {
		return nil, fmt.Errorf("CA cert %s/%s[%s]: %w", caNS, caRef.SecretName(), caRef.SecretKey(), err)
	}
	restCfg.TLSClientConfig.CAData = []byte(caData)
	return restCfg, nil
}

// clusterTarget pairs a cluster name with its client.
// name is "" for the local cluster.
type clusterTarget struct {
	name string
	kube kubeclient.Interface
}

// resolveClusterTargets returns the apply targets for a CRD+alias combination.
//
// Resolution cascade:
//  1. Target entry has Clusters → fan-out to those (must be subset of serve.Clusters).
//  2. serve.Clusters declared   → fan-out to all of serve.Clusters.
//  3. (nothing)                 → local cluster [{name:"", kube:localKube}].
//
// Static names are looked up in the registry. Template expressions are resolved
// via the full template engine (WithRequest). When fields is nil (read path),
// templates resolve to "" and the local cluster is used instead.
func resolveClusterTargets(
	crd *orktypes.CRDEntry,
	alias string,
	fields map[string]interface{},
	notes orktypes.NoteRegistry,
	registry *ClusterRegistry,
	localKube kubeclient.Interface,
) ([]clusterTarget, error) {
	var names []string

	if alias != "" {
		if t := crd.LookupTarget(alias); t.HasClusters() {
			names = t.TargetClusters()
		}
	}
	if len(names) == 0 && crd.Serve.HasClusters() {
		names = crd.Serve.Clusters
	}
	if len(names) == 0 {
		return []clusterTarget{{name: "", kube: localKube}}, nil
	}

	var targets []clusterTarget
	for _, expr := range names {
		name := expr
		if orktypes.IsTemplate(expr) {
			if fields == nil {
				return []clusterTarget{{name: "", kube: localKube}}, nil
			}
			resolver := orktmpl.NewResolverFromMap(fields).WithUserNotes(notes).WithRequest(fields)
			resolved, err := resolver.Resolve(expr)
			if err != nil {
				return nil, fmt.Errorf("resolving cluster expression %q: %w", expr, err)
			}
			name = strings.TrimSpace(resolved)
			if name == "" {
				continue
			}
		}
		kube, ok := registry.ClientFor(name)
		if !ok {
			return nil, fmt.Errorf("cluster %q is not registered in gateway.clusters", name)
		}
		targets = append(targets, clusterTarget{name: name, kube: kube})
	}

	if len(targets) == 0 {
		return []clusterTarget{{name: "", kube: localKube}}, nil
	}
	return targets, nil
}

// resolveReadCluster returns the single effective kubeclient.Interface for read operations
// (GET, LIST, DELETE). When serve.clusters has static names, the first is used.
// Empty templates (after resolution) and the local fallback return localKube.
func resolveReadCluster(
	crd *orktypes.CRDEntry,
	alias string,
	notes orktypes.NoteRegistry,
	registry *ClusterRegistry,
	localKube kubeclient.Interface,
) (kubeclient.Interface, error) {
	targets, err := resolveClusterTargets(crd, alias, nil, notes, registry, localKube)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 || targets[0].name == "" {
		return localKube, nil
	}
	return targets[0].kube, nil
}
