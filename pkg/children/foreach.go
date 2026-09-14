// pkg/children/foreach.go
//
// forEach expansion — expands template sources with a forEach declaration
// into N resolved sources, one per element in the list field.
//
// forEach works on every resource type. The expansion happens before the
// resource-specific run_*.go function is called — each run_*.go function
// receives an already-expanded slice and is unaware of forEach.
//
// Expansion sequence in runTemplateReconcile:
//
//	deployments := ExpandForEachDeployments(resolver, t.Deployments)
//	runDeployments(ctx, kube, resolver, obj, deployments, update)
//
// YAML:
//
//	onReconcile:
//	  deployments:
//	    - name: "{{ .metadata.name }}-{{ .item }}"
//	      image: "{{ .spec.image }}"
//	      forEach:
//	        field: spec.regions
//	        as: item
//
// For CR with spec.regions: ["us-east-1", "eu-west-1"]:
// Produces two DeploymentTemplateSources:
//
//	{Name: "my-app-us-east-1", Image: "nginx:latest", ...}
//	{Name: "my-app-eu-west-1", Image: "nginx:latest", ...}
//
// The expansion resolves ALL template expressions immediately — the
// returned slice contains static (non-template) values ready for the
// registry functions.
//
// when: and or: on forEach sources are evaluated per-item — each
// expanded source may pass or fail conditions independently.
package children

import (
	"sort"

	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ─────────────────────────────────────────────────────────────────────────────
// Generic core
// ─────────────────────────────────────────────────────────────────────────────

// expandForEach is the single generic forEach loop shared by all resource types.
//
// getForEach extracts the *ForEachSpec from one source element.
// resolve receives an item-scoped resolver and a copy of src; it must clear
// ForEach on the copy, resolve all template fields, and return the result.
func expandForEach[T any](
	resolver *orktmpl.Resolver,
	srcs []T,
	getForEach func(T) *orktypes.ForEachSpec,
	resolve func(ir *orktmpl.Resolver, src T) T,
) []T {
	if !anyHasForEach(len(srcs), func(i int) *orktypes.ForEachSpec { return getForEach(srcs[i]) }) {
		return srcs // fast path — no forEach in this list
	}
	var result []T
	for _, src := range srcs {
		fe := getForEach(src)
		if fe == nil {
			result = append(result, src)
			continue
		}
		for i, fi := range resolveForEachItems(resolver.Data(), fe.Field) {
			ir := itemResolver(resolver, fi, fe.As, i)
			result = append(result, resolve(ir, src))
		}
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared field-resolution helpers
// ─────────────────────────────────────────────────────────────────────────────

func resolveEnvVars(ir *orktmpl.Resolver, vars orktypes.EnvVarList) orktypes.EnvVarList {
	if len(vars) == 0 {
		return vars
	}
	out := make(orktypes.EnvVarList, 0, len(vars))
	for _, v := range vars {
		rv, _ := ir.Resolve(v.Value)
		out = append(out, orktypes.EnvVar{Name: v.Name, Value: rv})
	}
	return out
}

// resolveMap resolves template expressions in each value of a map[string]string —
// used for labels, annotations, and match selectors alike. Keys are never
// template expressions — only values are resolved.
func resolveMap(ir *orktmpl.Resolver, m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		rv, _ := ir.Resolve(v)
		out[k] = rv
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Public ExpandForEach* functions — one per resource type
// ─────────────────────────────────────────────────────────────────────────────

func ExpandForEachNamespaces(
	resolver *orktmpl.Resolver,
	srcs []orktypes.NamespaceTemplateSource,
) []orktypes.NamespaceTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.NamespaceTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.NamespaceTemplateSource) orktypes.NamespaceTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Labels = resolveMap(ir, src.Labels)
			if len(src.Finalizers) > 0 {
				out := make([]string, 0, len(src.Finalizers))
				for _, f := range src.Finalizers {
					rv, _ := ir.Resolve(f)
					out = append(out, rv)
				}
				src.Finalizers = out
			}
			return src
		},
	)
}

func ExpandForEachDeployments(
	resolver *orktmpl.Resolver,
	srcs []orktypes.DeploymentTemplateSource,
) []orktypes.DeploymentTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.DeploymentTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.DeploymentTemplateSource) orktypes.DeploymentTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Image, _ = ir.Resolve(src.Image)
			src.Replicas, _ = ir.Resolve(src.Replicas)
			src.Port, _ = ir.Resolve(src.Port)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Env = resolveEnvVars(ir, src.Env)
			src.Labels = resolveMap(ir, src.Labels)
			src.Annotations = resolveMap(ir, src.Annotations)
			return src
		},
	)
}

func ExpandForEachReplicaSets(
	resolver *orktmpl.Resolver,
	srcs []orktypes.ReplicaSetTemplateSource,
) []orktypes.ReplicaSetTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.ReplicaSetTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.ReplicaSetTemplateSource) orktypes.ReplicaSetTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Image, _ = ir.Resolve(src.Image)
			src.Replicas, _ = ir.Resolve(src.Replicas)
			src.Port, _ = ir.Resolve(src.Port)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Env = resolveEnvVars(ir, src.Env)
			src.Labels = resolveMap(ir, src.Labels)
			src.Annotations = resolveMap(ir, src.Annotations)
			return src
		},
	)
}

func ExpandForEachServices(
	resolver *orktmpl.Resolver,
	srcs []orktypes.ServiceTemplateSource,
) []orktypes.ServiceTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.ServiceTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.ServiceTemplateSource) orktypes.ServiceTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Port, _ = ir.Resolve(src.Port)
			src.TargetPort, _ = ir.Resolve(src.TargetPort)
			src.Labels = resolveMap(ir, src.Labels)
			src.Selector = resolveMap(ir, src.Selector)
			return src
		},
	)
}

func ExpandForEachSecrets(
	resolver *orktmpl.Resolver,
	srcs []orktypes.SecretTemplateSource,
) []orktypes.SecretTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.SecretTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.SecretTemplateSource) orktypes.SecretTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Labels = resolveMap(ir, src.Labels)
			return src
		},
	)
}

func ExpandForEachConfigMaps(
	resolver *orktmpl.Resolver,
	srcs []orktypes.ConfigMapTemplateSource,
) []orktypes.ConfigMapTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.ConfigMapTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.ConfigMapTemplateSource) orktypes.ConfigMapTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Labels = resolveMap(ir, src.Labels)
			return src
		},
	)
}

func ExpandForEachJobs(
	resolver *orktmpl.Resolver,
	srcs []orktypes.JobTemplateSource,
) []orktypes.JobTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.JobTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.JobTemplateSource) orktypes.JobTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Image, _ = ir.Resolve(src.Image)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Labels = resolveMap(ir, src.Labels)
			return src
		},
	)
}

func ExpandForEachCronJobs(
	resolver *orktmpl.Resolver,
	srcs []orktypes.CronJobTemplateSource,
) []orktypes.CronJobTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.CronJobTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.CronJobTemplateSource) orktypes.CronJobTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Schedule, _ = ir.Resolve(src.Schedule)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Labels = resolveMap(ir, src.Labels)
			return src
		},
	)
}

func ExpandForEachIngresses(
	resolver *orktmpl.Resolver,
	srcs []orktypes.IngressTemplateSource,
) []orktypes.IngressTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.IngressTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.IngressTemplateSource) orktypes.IngressTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Host, _ = ir.Resolve(src.Host)
			src.ServiceName, _ = ir.Resolve(src.ServiceName)
			src.ServicePort, _ = ir.Resolve(src.ServicePort)
			src.Path, _ = ir.Resolve(src.Path)
			src.IngressClass, _ = ir.Resolve(src.IngressClass)
			src.Labels = resolveMap(ir, src.Labels)
			src.Annotations = resolveMap(ir, src.Annotations)
			if src.TLS != nil {
				resolved := *src.TLS
				resolved.SecretName, _ = ir.Resolve(src.TLS.SecretName)
				if len(src.TLS.Hosts) > 0 {
					resolved.Hosts = make([]string, 0, len(src.TLS.Hosts))
					for _, h := range src.TLS.Hosts {
						rv, _ := ir.Resolve(h)
						resolved.Hosts = append(resolved.Hosts, rv)
					}
				}
				src.TLS = &resolved
			}
			return src
		},
	)
}

func ExpandForEachHPAs(
	resolver *orktmpl.Resolver,
	srcs []orktypes.HPATemplateSource,
) []orktypes.HPATemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.HPATemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.HPATemplateSource) orktypes.HPATemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.ScaleTargetRef.APIVersion, _ = ir.Resolve(src.ScaleTargetRef.APIVersion)
			src.ScaleTargetRef.Kind, _ = ir.Resolve(src.ScaleTargetRef.Kind)
			src.ScaleTargetRef.Name, _ = ir.Resolve(src.ScaleTargetRef.Name)
			src.MinReplicas, _ = ir.Resolve(src.MinReplicas)
			src.MaxReplicas, _ = ir.Resolve(src.MaxReplicas)
			src.TargetCPUUtilizationPercentage, _ = ir.Resolve(src.TargetCPUUtilizationPercentage)
			src.Labels = resolveMap(ir, src.Labels)
			return src
		},
	)
}

func ExpandForEachPDBs(
	resolver *orktmpl.Resolver,
	srcs []orktypes.PDBTemplateSource,
) []orktypes.PDBTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.PDBTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.PDBTemplateSource) orktypes.PDBTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.MinAvailable, _ = ir.Resolve(src.MinAvailable)
			src.MaxUnavailable, _ = ir.Resolve(src.MaxUnavailable)
			src.Labels = resolveMap(ir, src.Labels)
			src.Selector = resolveMap(ir, src.Selector)
			return src
		},
	)
}

func ExpandForEachServiceAccounts(
	resolver *orktmpl.Resolver,
	srcs []orktypes.ServiceAccountTemplateSource,
) []orktypes.ServiceAccountTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.ServiceAccountTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.ServiceAccountTemplateSource) orktypes.ServiceAccountTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Labels = resolveMap(ir, src.Labels)
			return src
		},
	)
}

func ExpandForEachStatefulSets(
	resolver *orktmpl.Resolver,
	srcs []orktypes.StatefulSetTemplateSource,
) []orktypes.StatefulSetTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.StatefulSetTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.StatefulSetTemplateSource) orktypes.StatefulSetTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Image, _ = ir.Resolve(src.Image)
			src.Tag, _ = ir.Resolve(src.Tag)
			src.Replicas, _ = ir.Resolve(src.Replicas)
			src.Port, _ = ir.Resolve(src.Port)
			src.ServiceName, _ = ir.Resolve(src.ServiceName)
			for i, vct := range src.VolumeClaimTemplates {
				src.VolumeClaimTemplates[i].StorageClass, _ = ir.Resolve(vct.StorageClass)
				src.VolumeClaimTemplates[i].StorageSize, _ = ir.Resolve(vct.StorageSize)
				src.VolumeClaimTemplates[i].MountPath, _ = ir.Resolve(vct.MountPath)
				src.VolumeClaimTemplates[i].Name, _ = ir.Resolve(vct.Name)
			}
			src.Labels = resolveMap(ir, src.Labels)
			src.Annotations = resolveMap(ir, src.Annotations)
			return src
		},
	)
}

func ExpandForEachPVCs(
	resolver *orktmpl.Resolver,
	srcs []orktypes.PVCTemplateSource,
) []orktypes.PVCTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.PVCTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.PVCTemplateSource) orktypes.PVCTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.StorageClassName, _ = ir.Resolve(src.StorageClassName)
			src.Storage, _ = ir.Resolve(src.Storage)
			src.VolumeName, _ = ir.Resolve(src.VolumeName)
			src.Labels = resolveMap(ir, src.Labels)
			return src
		},
	)
}

func ExpandForEachPVs(
	resolver *orktmpl.Resolver,
	srcs []orktypes.PVTemplateSource,
) []orktypes.PVTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.PVTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.PVTemplateSource) orktypes.PVTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.StorageClassName, _ = ir.Resolve(src.StorageClassName)
			src.Capacity, _ = ir.Resolve(src.Capacity)
			src.ReclaimPolicy, _ = ir.Resolve(src.ReclaimPolicy)
			src.HostPath, _ = ir.Resolve(src.HostPath)
			src.CSIDriver, _ = ir.Resolve(src.CSIDriver)
			src.CSIVolumeHandle, _ = ir.Resolve(src.CSIVolumeHandle)
			src.Labels = resolveMap(ir, src.Labels)
			return src
		},
	)
}

func ExpandForEachRoles(
	resolver *orktmpl.Resolver,
	srcs []orktypes.RoleTemplateSource,
) []orktypes.RoleTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.RoleTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.RoleTemplateSource) orktypes.RoleTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			return src
		},
	)
}

func ExpandForEachRoleBindings(
	resolver *orktmpl.Resolver,
	srcs []orktypes.RoleBindingTemplateSource,
) []orktypes.RoleBindingTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.RoleBindingTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.RoleBindingTemplateSource) orktypes.RoleBindingTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			return src
		},
	)
}

func ExpandForEachPods(
	resolver *orktmpl.Resolver,
	srcs []orktypes.PodTemplateSource,
) []orktypes.PodTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.PodTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.PodTemplateSource) orktypes.PodTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Image, _ = ir.Resolve(src.Image)
			src.Port, _ = ir.Resolve(src.Port)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			src.Labels = resolveMap(ir, src.Labels)
			src.Annotations = resolveMap(ir, src.Annotations)
			return src
		},
	)
}

func ExpandForEachNetworkPolicies(
	resolver *orktmpl.Resolver,
	srcs []orktypes.NetworkPolicyTemplateSource,
) []orktypes.NetworkPolicyTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.NetworkPolicyTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.NetworkPolicyTemplateSource) orktypes.NetworkPolicyTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			return src
		},
	)
}

func ExpandForEachResourceQuotas(
	resolver *orktmpl.Resolver,
	srcs []orktypes.ResourceQuotaTemplateSource,
) []orktypes.ResourceQuotaTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.ResourceQuotaTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.ResourceQuotaTemplateSource) orktypes.ResourceQuotaTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			return src
		},
	)
}

func ExpandForEachLimitRanges(
	resolver *orktmpl.Resolver,
	srcs []orktypes.LimitRangeTemplateSource,
) []orktypes.LimitRangeTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.LimitRangeTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.LimitRangeTemplateSource) orktypes.LimitRangeTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			src.Namespace, _ = ir.Resolve(src.Namespace)
			return src
		},
	)
}

func ExpandForEachClusterRoles(
	resolver *orktmpl.Resolver,
	srcs []orktypes.ClusterRoleTemplateSource,
) []orktypes.ClusterRoleTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.ClusterRoleTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.ClusterRoleTemplateSource) orktypes.ClusterRoleTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			return src
		},
	)
}

func ExpandForEachClusterRoleBindings(
	resolver *orktmpl.Resolver,
	srcs []orktypes.ClusterRoleBindingTemplateSource,
) []orktypes.ClusterRoleBindingTemplateSource {
	return expandForEach(resolver, srcs,
		func(s orktypes.ClusterRoleBindingTemplateSource) *orktypes.ForEachSpec { return s.ForEach },
		func(ir *orktmpl.Resolver, src orktypes.ClusterRoleBindingTemplateSource) orktypes.ClusterRoleBindingTemplateSource {
			src.ForEach = nil
			src.Name, _ = ir.Resolve(src.Name)
			return src
		},
	)
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

// forEachItem is one iteration step produced by resolveForEachItems.
// For list fields: key = element value, value = nil.
// For map fields:  key = map key (string), value = map value (object or string).
type forEachItem struct {
	key   interface{}
	value interface{}
}

// resolveForEachItems navigates a dot-notation path and returns iteration items.
//
// List field → one item per element; item.value is nil.
//
//	spec.regions: [us-east-1, eu-west-1]
//	→ [{key:"us-east-1"}, {key:"eu-west-1"}]
//
// Map field → one item per key (sorted); item.value is the map value.
//
//	spec.regions: {us-east-1: {replicas: 3}, eu-west-1: {replicas: 1}}
//	→ [{key:"eu-west-1", value:{replicas:1}}, {key:"us-east-1", value:{replicas:3}}]
//
// Template access for map items:
//
//	{{ .item }}            → map key  ("us-east-1")
//	{{ .<as> }}            → same as .item
//	{{ .value.replicas }}  → nested field in the map value
func resolveForEachItems(data map[string]interface{}, path string) []forEachItem {
	var current interface{} = data
	for _, part := range splitFieldPath(path) {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[part]
	}

	if list, ok := current.([]interface{}); ok {
		items := make([]forEachItem, len(list))
		for i, v := range list {
			items[i] = forEachItem{key: v}
		}
		return items
	}

	if m, ok := current.(map[string]interface{}); ok {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		items := make([]forEachItem, len(keys))
		for i, k := range keys {
			items[i] = forEachItem{key: k, value: m[k]}
		}
		return items
	}

	return nil
}

// itemResolver returns an item-scoped resolver for one forEach iteration step.
// For list items (fi.value == nil) only .item is injected.
// For map items (fi.value != nil) both .item and .value are injected.
func itemResolver(base *orktmpl.Resolver, fi forEachItem, as string, index int) *orktmpl.Resolver {
	if fi.value != nil {
		return base.WithItemAndValue(fi.key, fi.value, as, index)
	}
	return base.WithItem(fi.key, as, index)
}

// splitFieldPath splits a dot-notation path into segments.
func splitFieldPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			parts = append(parts, path[start:i])
			start = i + 1
		}
	}
	return append(parts, path[start:])
}

// anyHasForEach returns true if any element has a non-nil ForEach.
// Used as a fast-path check to avoid allocation when no forEach is declared.
func anyHasForEach(n int, getForEach func(int) *orktypes.ForEachSpec) bool {
	for i := 0; i < n; i++ {
		if getForEach(i) != nil {
			return true
		}
	}
	return false
}
