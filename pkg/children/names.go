package children

import (
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// resolvedChildName holds a resolved (non-template) name and namespace
// for one child resource.
type resolvedChildName struct {
	name       string
	namespace  string
	namespaced bool
}

// resolveName resolves a name and namespace from raw template strings.
// Returns false when the name is empty or unresolvable — indicating a resource
// whose name depends on a field the CR does not have.
func resolveName(resolver *orktmpl.Resolver, rawName, rawNamespace string) (resolvedChildName, bool) {
	name, err := resolver.Resolve(rawName)
	if err != nil || name == "" {
		return resolvedChildName{}, false
	}
	ns, _ := resolver.Resolve(rawNamespace) // namespace resolution failure is non-fatal
	return resolvedChildName{name: name, namespace: ns}, true
}

// **Names collects all resource names from the template list.
// Conditions are NOT evaluated here — we read all declared children
// so status can reference any of them regardless of phase.
// Conditions only gate creation in run_*.go.
func deploymentNames(resolver *orktmpl.Resolver, srcs []orktypes.DeploymentTemplateSource) []resolvedChildName {
	expanded := ExpandForEachDeployments(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func statefulSetNames(resolver *orktmpl.Resolver, srcs []orktypes.StatefulSetTemplateSource) []resolvedChildName {
	expanded := ExpandForEachStatefulSets(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func replicaSetNames(resolver *orktmpl.Resolver, srcs []orktypes.ReplicaSetTemplateSource) []resolvedChildName {
	expanded := ExpandForEachReplicaSets(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func serviceNames(resolver *orktmpl.Resolver, srcs []orktypes.ServiceTemplateSource) []resolvedChildName {
	expanded := ExpandForEachServices(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func secretNames(resolver *orktmpl.Resolver, srcs []orktypes.SecretTemplateSource) []resolvedChildName {
	expanded := ExpandForEachSecrets(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func configMapNames(resolver *orktmpl.Resolver, srcs []orktypes.ConfigMapTemplateSource) []resolvedChildName {
	expanded := ExpandForEachConfigMaps(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func jobNames(resolver *orktmpl.Resolver, srcs []orktypes.JobTemplateSource) []resolvedChildName {
	expanded := ExpandForEachJobs(resolver, srcs)
	var names []resolvedChildName
	for _, src := range expanded {
		name, err := resolver.Resolve(src.Name)
		if err != nil || name == "" {
			continue
		}
		ns, _ := resolver.Resolve(src.Namespace)
		names = append(names, resolvedChildName{name: name, namespace: ns})
	}
	return names
}

func cronJobNames(resolver *orktmpl.Resolver, srcs []orktypes.CronJobTemplateSource) []resolvedChildName {
	expanded := ExpandForEachCronJobs(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func podNames(resolver *orktmpl.Resolver, srcs []orktypes.PodTemplateSource) []resolvedChildName {
	names := make([]resolvedChildName, 0, len(srcs))
	for _, s := range srcs {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func serviceAccountNames(resolver *orktmpl.Resolver, srcs []orktypes.ServiceAccountTemplateSource) []resolvedChildName {
	expanded := ExpandForEachServiceAccounts(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func namespaceNames(resolver *orktmpl.Resolver, srcs []orktypes.NamespaceTemplateSource) []resolvedChildName {
	expanded := ExpandForEachNamespaces(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, ""); ok {
			names = append(names, n)
		}
	}
	return names
}

func ingressNames(resolver *orktmpl.Resolver, srcs []orktypes.IngressTemplateSource) []resolvedChildName {
	expanded := ExpandForEachIngresses(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func hpaNames(resolver *orktmpl.Resolver, srcs []orktypes.HPATemplateSource) []resolvedChildName {
	expanded := ExpandForEachHPAs(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func pvcNames(resolver *orktmpl.Resolver, srcs []orktypes.PVCTemplateSource) []resolvedChildName {
	expanded := ExpandForEachPVCs(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func networkPolicyNames(resolver *orktmpl.Resolver, srcs []orktypes.NetworkPolicyTemplateSource) []resolvedChildName {
	expanded := ExpandForEachNetworkPolicies(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func resourceQuotaNames(resolver *orktmpl.Resolver, srcs []orktypes.ResourceQuotaTemplateSource) []resolvedChildName {
	expanded := ExpandForEachResourceQuotas(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func limitRangeNames(resolver *orktmpl.Resolver, srcs []orktypes.LimitRangeTemplateSource) []resolvedChildName {
	expanded := ExpandForEachLimitRanges(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, s.Namespace); ok {
			names = append(names, n)
		}
	}
	return names
}

func clusterRoleNames(resolver *orktmpl.Resolver, srcs []orktypes.ClusterRoleTemplateSource) []resolvedChildName {
	expanded := ExpandForEachClusterRoles(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, ""); ok {
			names = append(names, n)
		}
	}
	return names
}

func clusterRoleBindingNames(resolver *orktmpl.Resolver, srcs []orktypes.ClusterRoleBindingTemplateSource) []resolvedChildName {
	expanded := ExpandForEachClusterRoleBindings(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		if n, ok := resolveName(resolver, s.Name, ""); ok {
			names = append(names, n)
		}
	}
	return names
}

func pvNames(resolver *orktmpl.Resolver, srcs []orktypes.PVTemplateSource) []resolvedChildName {
	expanded := ExpandForEachPVs(resolver, srcs)
	names := make([]resolvedChildName, 0, len(expanded))
	for _, s := range expanded {
		// PVs are cluster-scoped — namespace is always empty.
		if n, ok := resolveName(resolver, s.Name, ""); ok {
			names = append(names, n)
		}
	}
	return names
}
