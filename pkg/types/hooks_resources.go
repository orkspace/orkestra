package types

import (
	"fmt"
	"strings"
)

// ResourceProfileEntry describes a single resources.profile reference found in a
// HookTemplates resource. Used by validation to fail fast on unknown profiles
// and to enforce mutual exclusivity between profile and explicit resource fields.
type ResourceProfileEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	Resource     string // e.g. "Deployment", "StatefulSet"
	ResourceName string // template name field (may be Empty()
	Profile      string // raw profile name as written in the katalog
	Mixed        bool   // true when profile is set alongside explicit requests/limits
}

// resourced is a tiny interface satisfied by resources that carry a ResourceRequirements block.
type resourced interface {
	GetResources() *ResourceRequirements
}

// GetResources returns the resource requirements for each supported workload type.
func (t DeploymentTemplateSource) GetResources() *ResourceRequirements  { return t.Resources }
func (t ReplicaSetTemplateSource) GetResources() *ResourceRequirements  { return t.Resources }
func (t StatefulSetTemplateSource) GetResources() *ResourceRequirements { return t.Resources }
func (t PodTemplateSource) GetResources() *ResourceRequirements         { return t.Resources }
func (t JobTemplateSource) GetResources() *ResourceRequirements         { return t.Resources }
func (t CronJobTemplateSource) GetResources() *ResourceRequirements     { return t.Resources }

// CollectResourceProfileEntries returns all resources.profile references declared for
// this CRD across OnCreate, OnReconcile, and OnDelete.
// Only entries with a non-empty Profile string are returned.
func (c *CRDEntry) CollectResourceProfileEntries() []ResourceProfileEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []ResourceProfileEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			rd, ok := res.(resourced)
			if !ok {
				return
			}
			spec := rd.GetResources()
			if spec == nil || spec.Profile == "" {
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

			mixed := len(spec.Requests) > 0 || len(spec.Limits) > 0

			out = append(out, ResourceProfileEntry{
				Phase:        phase,
				Resource:     rtype,
				ResourceName: rname,
				Profile:      spec.Profile,
				Mixed:        mixed,
			})
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
