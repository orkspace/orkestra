// pkg/reconciler/run_template_reconcile.go
//
// Execution order (each step can reference all previous steps):
//
//  1. Recieve NewResolver        → .spec.*, .status.*, .metadata.*
//  2. r.readCross     			  → .cross.<crd>.status.* (informer cache, zero API calls)
//  3. runExternal        	      → .external.<n>.status, .body (HTTP calls)
//  4. forEach expand             → N sources from N-element list fields
//  5. onCreate groups            → deployments, services, secrets, configmaps, ...
//  6. onReconcile groups
//  7. runProviders               → aws:, mongodb:, ... (external infra)
package reconciler

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/children"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	orklabels "github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/runners"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/client-go/tools/cache"
)

// runTemplateReconcile interprets the Katalog's onCreate and onReconcile blocks.
// Returns the enriched resolver so callers (reconcileImpl) can pass cross/external
// data into patchStatusWithChildren for status field evaluation.
func (r *GenericReconciler[PTR]) runTemplateReconcile(ctx context.Context, resolver *orktmpl.Resolver, obj domain.Object, box orktypes.OperatorBoxConfig) (*orktmpl.Resolver, error) {
	kube, ok := kubeclient.FromContext(ctx)
	if !ok {
		return resolver, fmt.Errorf("kubeclient not found in context")
	}

	// Step 1: We now receive a base resolver (already normalized) from reconcileImpl.
	// All subsequent steps (cross, git, external, docker, resources, providers)
	// enrich this resolver in-place.
	var err error

	// Step 2: cross-CRD observation
	// Reads from sibling CRD informer caches via r.katalogRegistry — zero API calls.
	// Must run first so git, docker, external calls, and resources can reference .cross.*
	if len(box.Cross) > 0 {
		crossData := r.readCross(ctx, obj, box.Cross, resolver)
		logger.FromContext(ctx).Debug().
			Str("observer", obj.GetName()).
			Int("cross_entries", len(crossData)).
			Interface("cross_keys", crossDataKeys(crossData)).
			Msg("cross: resolver enrichment")
		if len(crossData) > 0 {
			resolver = resolver.WithCross(crossData)
		}
	}

	// Step 3: Git hook
	// Runs before external calls so URLs, tokens, and payloads can reference .git.commit,
	// .git.changed, and .git.path. Git is a declarative precondition for pipelines.
	if t := box.OnReconcile; t != nil && t.Git != nil {
		resolver, err = runGit(ctx, r.crd.GVKString(), resolver, kube, obj, r.crd.GVR(), t.Git)
		if err != nil {
			return resolver, fmt.Errorf("git hook: %w", err)
		}
	}
	if t := box.OnCreate; t != nil && t.Git != nil {
		resolver, err = runGit(ctx, r.crd.GVKString(), resolver, kube, obj, r.crd.GVR(), t.Git)
		if err != nil {
			return resolver, fmt.Errorf("git hook: %w", err)
		}
	}

	// Step 4: external HTTP calls
	// Runs after Git so external URLs can embed commit hashes or paths.
	if t := box.OnReconcile; t != nil && len(t.External) > 0 {
		resolver, err = runExternal(ctx, r.crd.GVKString(), resolver, t.External, r.kube.Clientset())
		if err != nil {
			return resolver, fmt.Errorf("external calls: %w", err)
		}
	}
	if t := box.OnCreate; t != nil && len(t.External) > 0 {
		resolver, err = runExternal(ctx, r.crd.GVKString(), resolver, t.External, r.kube.Clientset())
		if err != nil {
			return resolver, fmt.Errorf("external calls: %w", err)
		}
	}

	// Step 5: Docker hook
	// Runs after external so build/push can use tokens or metadata from external calls.
	if t := box.OnReconcile; t != nil && t.Docker != nil {
		resolver, err = runDocker(ctx, r.crd.GVKString(), resolver, t.Docker)
		if err != nil {
			return resolver, fmt.Errorf("docker hook: %w", err)
		}
	}
	if t := box.OnCreate; t != nil && t.Docker != nil {
		resolver, err = runDocker(ctx, r.crd.GVKString(), resolver, t.Docker)
		if err != nil {
			return resolver, fmt.Errorf("docker hook: %w", err)
		}
	}

	// Step 6: onCreate resource groups (update=false)
	if t := box.OnCreate; t != nil {
		if err := r.runResourceGroup(ctx, kube, resolver, obj, t, false); err != nil {
			return resolver, err
		}
	}

	// Step 7: onReconcile resource groups (update=true)
	if t := box.OnReconcile; t != nil {
		if err := r.runResourceGroup(ctx, kube, resolver, obj, t, true); err != nil {
			return resolver, err
		}
	}

	// Step 8: provider dispatch
	if len(box.ProviderBlocks) > 0 && r.providerRegistry != nil && r.providerRegistry.Len() > 0 {
		kubeReader := &kubeReaderAdapter{kube: kube}
		if err := runProviders(ctx, obj, resolver, box.ProviderBlocks, r.providerRegistry, kubeReader, r.providerStats); err != nil {
			return resolver, fmt.Errorf("providers: %w", err)
		}
	}

	return resolver, nil
}

// runResourceGroup dispatches all resource types in one HookTemplates block.
// forEach expansion happens here — run_*.go receives already-expanded slices.
func (r *GenericReconciler[PTR]) runResourceGroup(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	obj domain.Object,
	t *orktypes.HookTemplates,
	update bool,
) error {
	// Guard closure — captures r for access to CRD config.
	// nil-safe: if CRD has no restrictions, guard is a no-op.
	guard := r.namespaceGuardFunc(ctx, obj)

	labelMgr := orklabels.NewManager(orklabels.Config{
		Standalone:                r.kat.IsStandaloneGateway(),
		DeletionProtectionEnabled: r.kat.IsDeletionProtectionEnabled(),
	})

	// Create namespaces first
	if err := runners.RunNamespaces(ctx, kube, resolver, obj,
		children.ExpandForEachNamespaces(resolver, t.Namespaces), update); err != nil {
		return err
	}

	if err := runners.RunSecrets(ctx, kube, resolver, obj,
		children.ExpandForEachSecrets(resolver, t.Secrets), update, guard); err != nil {
		return err
	}
	if err := runners.RunConfigMaps(ctx, kube, resolver, obj,
		children.ExpandForEachConfigMaps(resolver, t.ConfigMaps), update, guard); err != nil {
		return err
	}
	if err := runners.RunNetworkPolicies(ctx, kube, resolver, obj,
		children.ExpandForEachNetworkPolicies(resolver, t.NetworkPolicies), update, guard); err != nil {
		return err
	}
	if err := runners.RunResourceQuotas(ctx, kube, resolver, obj,
		children.ExpandForEachResourceQuotas(resolver, t.ResourceQuotas), update, guard); err != nil {
		return err
	}
	if err := runners.RunLimitRanges(ctx, kube, resolver, obj,
		children.ExpandForEachLimitRanges(resolver, t.LimitRanges), update, guard); err != nil {
		return err
	}
	if err := runners.RunClusterRoles(ctx, kube, resolver, obj,
		children.ExpandForEachClusterRoles(resolver, t.ClusterRoles), update); err != nil {
		return err
	}
	if err := runners.RunClusterRoleBindings(ctx, kube, resolver, obj,
		children.ExpandForEachClusterRoleBindings(resolver, t.ClusterRoleBindings), update); err != nil {
		return err
	}
	if err := runners.RunServiceAccounts(ctx, kube, resolver, obj,
		children.ExpandForEachServiceAccounts(resolver, t.ServiceAccounts), update, guard); err != nil {
		return err
	}
	if err := runners.RunRoles(ctx, kube, resolver, obj,
		children.ExpandForEachRoles(resolver, t.Roles), update, guard); err != nil {
		return err
	}
	if err := runners.RunRoleBindings(ctx, kube, resolver, obj,
		children.ExpandForEachRoleBindings(resolver, t.RoleBindings), update, guard); err != nil {
		return err
	}
	if err := runCustomResources(ctx, kube, resolver, obj,
		children.ExpandForEachCustomResources(resolver, t.CustomResource), update, guard, labelMgr,
		r.kat.IsDeletionProtectionEnabled() && r.crd.ShouldProtectCRs()); err != nil {
		return err
	}
	if err := runners.RunReplicaSets(ctx, kube, resolver, obj,
		children.ExpandForEachReplicaSets(resolver, t.ReplicaSets), update, guard); err != nil {
		return err
	}
	if err := runners.RunDeployments(ctx, kube, resolver, obj,
		children.ExpandForEachDeployments(resolver, t.Deployments), update, guard); err != nil {
		return err
	}
	if err := runners.RunServices(ctx, kube, resolver, obj,
		children.ExpandForEachServices(resolver, t.Services), update, guard); err != nil {
		return err
	}
	if err := runners.RunJobs(ctx, kube, resolver, obj,
		children.ExpandForEachJobs(resolver, t.Jobs), guard); err != nil {
		return err
	}
	if err := runners.RunCronJobs(ctx, kube, resolver, obj,
		children.ExpandForEachCronJobs(resolver, t.CronJobs), update, guard); err != nil {
		return err
	}
	if err := runners.RunStatefulSets(ctx, kube, resolver, obj,
		children.ExpandForEachStatefulSets(resolver, t.StatefulSets), update, guard); err != nil {
		return err
	}
	if err := runners.RunPVs(ctx, kube, resolver, obj,
		children.ExpandForEachPVs(resolver, t.PersistentVolumes), update); err != nil {
		return err
	}
	if err := runners.RunPVCs(ctx, kube, resolver, obj,
		children.ExpandForEachPVCs(resolver, t.PersistentVolumeClaims), update, guard); err != nil {
		return err
	}
	if err := runners.RunIngresses(ctx, kube, resolver, obj,
		children.ExpandForEachIngresses(resolver, t.Ingresses), update, guard); err != nil {
		return err
	}
	if err := runners.RunHPAs(ctx, kube, resolver, obj,
		children.ExpandForEachHPAs(resolver, t.HorizontalPodAutoscalers), update, guard); err != nil {
		return err
	}
	if err := runners.RunPDBs(ctx, kube, resolver, obj,
		children.ExpandForEachPDBs(resolver, t.PodDisruptionBudgets), update, guard); err != nil {
		return err
	}
	if err := runners.RunPods(ctx, kube, resolver, obj,
		children.ExpandForEachPods(resolver, t.Pods), update, guard); err != nil {
		return err
	}
	return nil
}

// runTemplateOnDelete interprets the onDelete block.
func (r *GenericReconciler[PTR]) runTemplateOnDelete(ctx context.Context, resolver *orktmpl.Resolver, obj domain.Object, box orktypes.OperatorBoxConfig) error {
	kube, ok := kubeclient.FromContext(ctx)
	if !ok {
		return fmt.Errorf("kubeclient not found in context")
	}

	guard := r.namespaceGuardFunc(ctx, obj)

	if t := box.OnDelete; t != nil {
		if t.Ordered {
			if err := r.runOrderedDelete(ctx, kube, resolver, obj, t, guard); err != nil {
				return err
			}
		} else {
			if err := runners.RunJobs(ctx, kube, resolver, obj,
				children.ExpandForEachJobs(resolver, t.Jobs), guard); err != nil {
				return err
			}
		}
	}

	if len(box.ProviderBlocks) > 0 && r.providerRegistry != nil {
		kubeReader := &kubeReaderAdapter{kube: kube}
		if err := runProviderDelete(ctx, obj, resolver, box.ProviderBlocks, r.providerRegistry, kubeReader, r.providerStats); err != nil {
			return fmt.Errorf("provider cleanup: %w", err)
		}
	}

	// Cluster-scoped resources cannot have namespace-scoped owners, so GC never cleans them up.
	// Always run explicit cleanup regardless of ordered/unordered path.
	if err := runners.DeleteOwnedClusterScopedResources(ctx, kube, resolver, obj, box); err != nil {
		return fmt.Errorf("cluster-scoped resource cleanup: %w", err)
	}

	return nil
}

func crossDataKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// readCross reads cross-CRD observations for all declared cross: entries.
// Returns the map injected via resolver.WithCross().
//
// Resolution priority per declaration:
//  1. Informer cache via r.katalogRegistry — zero API calls, same-binary CRDs
//  2. HTTP endpoint — cross-binary or cross-cluster (raw endpoint or ONCOP host inference)
//  3. Not-found result — when neither path is available
//
// Within the informer path, finding the informer and finding the CR are separate steps:
//   - IsCRDBased: informer looked up by CRD name
//   - IsLabelBased: informer looked up by decl.LabelSelector (CRD-entry labelSelector in registry)
//   - selector.IsNameBased: CR found by namespace/name key
//   - selector.MatchLabels: CR found by scanning instance labels in the informer
//   - neither selector: first CR matching decl.LabelSelector (label-based decl only)
func (r *GenericReconciler[PTR]) readCross(
	ctx context.Context,
	obj domain.Object,
	decls []orktypes.CrossCRDDeclaration,
	resolver *orktmpl.Resolver,
) map[string]interface{} {
	if len(decls) == 0 {
		return nil
	}

	log := logger.FromContext(ctx)
	result := make(map[string]interface{}, len(decls))

	for _, decl := range decls {
		as := decl.As
		if as == "" {
			as = decl.CRD
		}

		name, _ := resolver.Resolve(decl.Selector.Name)
		namespace, _ := resolver.Resolve(decl.Selector.Namespace)
		if namespace == "" {
			namespace = obj.GetNamespace()
		}

		// Step 1: find the informer. decl.LabelSelector keys the registry (CRD-entry labelSelector);
		// decl.CRD keys by name. These are mutually exclusive per ork validate.
		var inf cache.SharedIndexInformer
		if r.katalogRegistry != nil {
			switch {
			case decl.IsCRDBased():
				if i, found := r.katalogRegistry.GetInformerByName(decl.CRD); found {
					inf = i
				}
			case decl.IsLabelBased():
				for k, v := range decl.LabelSelector {
					if i, found := r.katalogRegistry.GetInformerByLabelSelector(k, v); found {
						inf = i
						break
					}
				}
			}
		}

		// Step 2: find the CR within the informer.
		if inf != nil {
			// CrossAccess is only meaningful for CRD-named declarations.
			var crossAccess *bool
			if decl.IsCRDBased() {
				crossAccess = r.katalogRegistry.GetCrossAccessByName(decl.CRD)
			}

			sel := decl.Selector
			var data map[string]interface{}
			switch {
			case !sel.MatchLabels.Empty():
				// selector.matchLabels filters CR instances within the informer by their labels.
				for k, v := range sel.MatchLabels {
					data = ReadCrossFromInformerByLabel(inf.GetIndexer(), k, v)
					break
				}
			case decl.IsLabelBased():
				// Label-based decl with no CR selector — first CR matching the decl label.
				for k, v := range decl.LabelSelector {
					data = ReadCrossFromInformerByLabel(inf.GetIndexer(), k, v)
					break
				}
			case sel.IsNameBased():
				// Name is the precision tool — used when label identity is insufficient.
				data = ReadCrossFromInformerByName(inf.GetIndexer(), crossKey(namespace, name), crossAccess)
			}

			if data != nil {
				result[as] = data
				log.Debug().
					Str("crd", decl.CRD).
					Str("as", as).
					Msg("cross: read from informer cache")
				continue
			}

			log.Warn().
				Str("crd", decl.CRD).
				Str("as", as).
				Msg("cross: informer found but CR not matched")
		} else if r.katalogRegistry != nil {
			log.Warn().
				Str("crd", decl.CRD).
				Str("as", as).
				Msg("cross: CRD not found in registry — trying HTTP")
		}

		// Step 3: HTTP fallback (cross-binary or cross-cluster).
		if decl.HasSource() {
			// 3a: raw endpoint — non-Orkestra operators or arbitrary JSON APIs.
			if decl.Source.HasEndpoint() {
				src := *decl.Source
				src.Endpoint, _ = resolver.Resolve(decl.Source.Endpoint)
				data := fetchCrossViaHTTP(ctx, r.kube.Clientset(), &src)
				if data != nil {
					result[as] = data
					log.Debug().
						Str("crd", decl.CRD).
						Str("as", as).
						Str("endpoint", src.Endpoint).
						Msg("cross: read via raw endpoint")
					continue
				}
				log.Warn().
					Str("crd", decl.CRD).
					Str("endpoint", src.Endpoint).
					Msg("cross: raw endpoint returned nil")
			}

			// 3b: ONCOP host inference — Orkestra-native operators.
			if decl.Source.HasHost() {
				src := *decl.Source
				src.Endpoint = orktypes.BuildONCOPURL(decl)
				data := fetchCrossViaHTTP(ctx, r.kube.Clientset(), &src)
				if data != nil {
					result[as] = data
					log.Debug().
						Str("crd", decl.CRD).
						Str("as", as).
						Str("endpoint", src.Endpoint).
						Msg("cross: read via ONCOP host")
					continue
				}
				log.Warn().
					Str("crd", decl.CRD).
					Str("endpoint", src.Endpoint).
					Msg("cross: ONCOP endpoint returned nil")
			}
		}

		// Step 4: not found.
		result[as] = map[string]interface{}{
			"found":     "false",
			"name":      name,
			"namespace": namespace,
			"status":    map[string]interface{}{},
			"spec":      map[string]interface{}{},
		}
		log.Debug().
			Str("crd", decl.CRD).
			Str("as", as).
			Msg("cross: not found — empty result")
	}

	return result
}
