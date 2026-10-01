package types

// FilterResources returns a new HookTemplates containing only the resources
// that pass fn, with their conditions updated to whatever fn returns.
//
// fn receives the current (conditions, or) for a resource and returns:
//   - keep: whether to include the resource in the output
//   - conditions: the conditions to set on the kept resource
//   - or: the or conditions to set on the kept resource
//
// Callers use this to separate motif-time static conditions (evaluated now)
// from runtime conditions (preserved on the resource for the reconciler).
//
// External calls are copied unchanged — their when: conditions are always
// runtime conditions evaluated by the reconciler, never by the expander.
func (h HookTemplates) FilterResources(fn func(conditions, or []Condition) (keep bool, newConditions, newOr []Condition)) HookTemplates {
	var out HookTemplates

	for _, s := range h.Deployments {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.Deployments = append(out.Deployments, s)
		}
	}
	for _, s := range h.ReplicaSets {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.ReplicaSets = append(out.ReplicaSets, s)
		}
	}
	for _, s := range h.StatefulSets {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.StatefulSets = append(out.StatefulSets, s)
		}
	}
	for _, s := range h.Services {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.Services = append(out.Services, s)
		}
	}
	for _, s := range h.Pods {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.Pods = append(out.Pods, s)
		}
	}
	for _, s := range h.Jobs {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.Jobs = append(out.Jobs, s)
		}
	}
	for _, s := range h.CronJobs {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.CronJobs = append(out.CronJobs, s)
		}
	}
	for _, s := range h.Secrets {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.Secrets = append(out.Secrets, s)
		}
	}
	for _, s := range h.ConfigMaps {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.ConfigMaps = append(out.ConfigMaps, s)
		}
	}
	for _, s := range h.ServiceAccounts {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.ServiceAccounts = append(out.ServiceAccounts, s)
		}
	}
	for _, s := range h.Ingresses {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.Ingresses = append(out.Ingresses, s)
		}
	}
	for _, s := range h.PersistentVolumes {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.PersistentVolumes = append(out.PersistentVolumes, s)
		}
	}
	for _, s := range h.PersistentVolumeClaims {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.PersistentVolumeClaims = append(out.PersistentVolumeClaims, s)
		}
	}
	for _, s := range h.HorizontalPodAutoscalers {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.HorizontalPodAutoscalers = append(out.HorizontalPodAutoscalers, s)
		}
	}
	for _, s := range h.PodDisruptionBudgets {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.PodDisruptionBudgets = append(out.PodDisruptionBudgets, s)
		}
	}
	for _, s := range h.Namespaces {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.Namespaces = append(out.Namespaces, s)
		}
	}
	for _, s := range h.Roles {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.Roles = append(out.Roles, s)
		}
	}
	for _, s := range h.RoleBindings {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.RoleBindings = append(out.RoleBindings, s)
		}
	}
	for _, s := range h.ClusterRoles {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.ClusterRoles = append(out.ClusterRoles, s)
		}
	}
	for _, s := range h.ClusterRoleBindings {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.ClusterRoleBindings = append(out.ClusterRoleBindings, s)
		}
	}
	for _, s := range h.NetworkPolicies {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.NetworkPolicies = append(out.NetworkPolicies, s)
		}
	}
	for _, s := range h.ResourceQuotas {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.ResourceQuotas = append(out.ResourceQuotas, s)
		}
	}
	for _, s := range h.LimitRanges {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.LimitRanges = append(out.LimitRanges, s)
		}
	}
	for _, s := range h.CustomResource {
		if keep, c, a := fn(s.Conditions, s.Or); keep {
			s.Conditions, s.Or = c, a
			out.CustomResource = append(out.CustomResource, s)
		}
	}

	// External calls are copied unchanged — their conditions are runtime-only.
	out.External = h.External
	out.Timeout = h.Timeout

	return out
}

// MergeFrom appends all resources from src into h.
func (h *HookTemplates) MergeFrom(src *HookTemplates) {
	if h == nil || src == nil {
		return
	}
	h.Deployments = append(h.Deployments, src.Deployments...)
	h.ReplicaSets = append(h.ReplicaSets, src.ReplicaSets...)
	h.StatefulSets = append(h.StatefulSets, src.StatefulSets...)
	h.Services = append(h.Services, src.Services...)
	h.Pods = append(h.Pods, src.Pods...)
	h.Jobs = append(h.Jobs, src.Jobs...)
	h.CronJobs = append(h.CronJobs, src.CronJobs...)
	h.Secrets = append(h.Secrets, src.Secrets...)
	h.ConfigMaps = append(h.ConfigMaps, src.ConfigMaps...)
	h.ServiceAccounts = append(h.ServiceAccounts, src.ServiceAccounts...)
	h.Ingresses = append(h.Ingresses, src.Ingresses...)
	h.PersistentVolumes = append(h.PersistentVolumes, src.PersistentVolumes...)
	h.PersistentVolumeClaims = append(h.PersistentVolumeClaims, src.PersistentVolumeClaims...)
	h.HorizontalPodAutoscalers = append(h.HorizontalPodAutoscalers, src.HorizontalPodAutoscalers...)
	h.PodDisruptionBudgets = append(h.PodDisruptionBudgets, src.PodDisruptionBudgets...)
	h.Namespaces = append(h.Namespaces, src.Namespaces...)
	h.Roles = append(h.Roles, src.Roles...)
	h.RoleBindings = append(h.RoleBindings, src.RoleBindings...)
	h.ClusterRoles = append(h.ClusterRoles, src.ClusterRoles...)
	h.ClusterRoleBindings = append(h.ClusterRoleBindings, src.ClusterRoleBindings...)
	h.NetworkPolicies = append(h.NetworkPolicies, src.NetworkPolicies...)
	h.ResourceQuotas = append(h.ResourceQuotas, src.ResourceQuotas...)
	h.LimitRanges = append(h.LimitRanges, src.LimitRanges...)
	h.CustomResource = append(h.CustomResource, src.CustomResource...)
	h.External = append(h.External, src.External...)
}
