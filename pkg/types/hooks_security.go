package types

import (
	"fmt"
	"strings"
)

// SecurityProfileEntry describes a single security profile reference found in a
// HookTemplates resource. Used by validation to fail fast on unknown profiles
// and to enforce mutual exclusivity between profile and explicit security fields.
type SecurityProfileEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	Resource     string // e.g. "Deployment", "StatefulSet"
	ResourceName string // template name field (may be Empty()
	Kind         string // "container" or "pod"
	Profile      string // raw profile name as written in the katalog
	Mixed        bool   // true when profile is set alongside explicit fields
}

// securityContexted is satisfied by resources that carry a ContainerSecurityContext.
type securityContexted interface {
	GetSecurityContext() *ContainerSecurityContext
}

// podSecurityed is satisfied by resources that carry a PodSecurityContext.
type podSecurityed interface {
	GetPodSecurity() *PodSecurityContext
}

// GetSecurityContext returns the container-level security context for each supported workload type.
func (t DeploymentTemplateSource) GetSecurityContext() *ContainerSecurityContext {
	return t.SecurityContext
}
func (t ReplicaSetTemplateSource) GetSecurityContext() *ContainerSecurityContext {
	return t.SecurityContext
}
func (t StatefulSetTemplateSource) GetSecurityContext() *ContainerSecurityContext {
	return t.SecurityContext
}
func (t PodTemplateSource) GetSecurityContext() *ContainerSecurityContext { return t.SecurityContext }
func (t JobTemplateSource) GetSecurityContext() *ContainerSecurityContext { return t.SecurityContext }
func (t CronJobTemplateSource) GetSecurityContext() *ContainerSecurityContext {
	return t.SecurityContext
}

// GetPodSecurity returns the pod-level security context for each supported workload type.
func (t DeploymentTemplateSource) GetPodSecurity() *PodSecurityContext  { return t.PodSecurity }
func (t ReplicaSetTemplateSource) GetPodSecurity() *PodSecurityContext  { return t.PodSecurity }
func (t StatefulSetTemplateSource) GetPodSecurity() *PodSecurityContext { return t.PodSecurity }
func (t PodTemplateSource) GetPodSecurity() *PodSecurityContext         { return t.PodSecurity }
func (t JobTemplateSource) GetPodSecurity() *PodSecurityContext         { return t.PodSecurity }
func (t CronJobTemplateSource) GetPodSecurity() *PodSecurityContext     { return t.PodSecurity }

// CapabilityEntry describes a single capability value found in a
// ContainerSecurityContext.Capabilities block. Used to validate names against
// the known Linux capability set at katalog load time.
type CapabilityEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	Resource     string // e.g. "Deployment", "StatefulSet"
	ResourceName string
	Side         string // "add" or "drop"
	Value        string // raw capability name as written (e.g. "NET_RAW", "ALL")
}

// CollectCapabilityEntries returns all capabilities declared across every hook phase.
// Both add and drop lists are included. Template expressions are included as-is
// and must be skipped by the caller.
func (c *CRDEntry) CollectCapabilityEntries() []CapabilityEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []CapabilityEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			sc, ok := res.(securityContexted)
			if !ok {
				return
			}
			ctx := sc.GetSecurityContext()
			if ctx == nil || ctx.Capabilities == nil {
				return
			}

			var rname string
			if n, ok := res.(namer); ok {
				rname = n.GetName()
			}
			rtype := fmt.Sprintf("%T", res)
			if i := strings.LastIndex(rtype, "."); i >= 0 {
				rtype = rtype[i+1:]
			}

			for _, cap := range ctx.Capabilities.Add {
				out = append(out, CapabilityEntry{
					Phase:        phase,
					Resource:     rtype,
					ResourceName: rname,
					Side:         "add",
					Value:        cap,
				})
			}
			for _, cap := range ctx.Capabilities.Drop {
				out = append(out, CapabilityEntry{
					Phase:        phase,
					Resource:     rtype,
					ResourceName: rname,
					Side:         "drop",
					Value:        cap,
				})
			}
		})
	}

	if c.HasOnCreate() {
		collect("onCreate", c.OperatorBox.OnCreate)
	}
	if c.HasOnReconcile() {
		collect("onReconcile", c.OperatorBox.OnReconcile)
	}
	if c.HasOnDelete() {
		collect("onDelete", c.OperatorBox.OnDelete)
	}

	return out
}

// CollectSecurityProfileEntries returns all security profile references declared for
// this CRD across OnCreate, OnReconcile, and OnDelete.
// Only entries with a non-empty Profile string are returned.
func (c *CRDEntry) CollectSecurityProfileEntries() []SecurityProfileEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []SecurityProfileEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			var rname string
			if n, ok := res.(namer); ok {
				rname = n.GetName()
			}
			rtype := fmt.Sprintf("%T", res)
			if i := strings.LastIndex(rtype, "."); i >= 0 {
				rtype = rtype[i+1:]
			}

			if sc, ok := res.(securityContexted); ok {
				ctx := sc.GetSecurityContext()
				if ctx != nil && ctx.Profile != "" {
					out = append(out, SecurityProfileEntry{
						Phase:        phase,
						Resource:     rtype,
						ResourceName: rname,
						Kind:         "container",
						Profile:      ctx.Profile,
						Mixed:        ctx.hasMixedFields(),
					})
				}
			}

			if ps, ok := res.(podSecurityed); ok {
				pod := ps.GetPodSecurity()
				if pod != nil && pod.Profile != "" {
					out = append(out, SecurityProfileEntry{
						Phase:        phase,
						Resource:     rtype,
						ResourceName: rname,
						Kind:         "pod",
						Profile:      pod.Profile,
						Mixed:        pod.hasMixedFields(),
					})
				}
			}
		})
	}

	if c.HasOnCreate() {
		collect("onCreate", c.OperatorBox.OnCreate)
	}
	if c.HasOnReconcile() {
		collect("onReconcile", c.OperatorBox.OnReconcile)
	}
	if c.HasOnDelete() {
		collect("onDelete", c.OperatorBox.OnDelete)
	}

	return out
}
