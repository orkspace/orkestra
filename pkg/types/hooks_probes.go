package types

import (
	"fmt"
	"strings"
)

// ProbeProfileEntry describes a single probe profile reference found in a
// HookTemplates resource. Used by validation to fail fast on unknown profiles
// and to enforce mutual exclusivity between profile and explicit timing fields.
type ProbeProfileEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	Resource     string // e.g. "Deployment", "StatefulSet"
	ResourceName string // template name field (may be Empty()
	ProbeType    string // "startup", "liveness", "readiness"
	Profile      string // raw profile name as written in the katalog
	Mixed        bool   // true when profile is set alongside explicit timing overrides
}

// prober is a tiny interface satisfied by resources that carry a ProbesConfig.
type prober interface {
	GetProbes() *ProbesConfig
}

// GetProbes returns the probe configuration for each supported workload type.
// Value receivers so the method is available on both values and pointers.
func (t DeploymentTemplateSource) GetProbes() *ProbesConfig  { return t.Probes }
func (t ReplicaSetTemplateSource) GetProbes() *ProbesConfig  { return t.Probes }
func (t StatefulSetTemplateSource) GetProbes() *ProbesConfig { return t.Probes }
func (t PodTemplateSource) GetProbes() *ProbesConfig         { return t.Probes }

// CollectProbeProfileEntries returns all probe profile references declared for
// this CRD across OnCreate, OnReconcile, and OnDelete.
// Only entries with a non-empty Profile string are returned — omitted profiles
// (which fall back to "standard") are not surfaced for validation.
func (c *CRDEntry) CollectProbeProfileEntries() []ProbeProfileEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []ProbeProfileEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			pb, ok := res.(prober)
			if !ok {
				return
			}
			probes := pb.GetProbes()
			if probes == nil {
				return
			}

			// Derive friendly names
			var rname string
			if n, ok := res.(namer); ok {
				rname = n.GetName()
			}
			rtype := fmt.Sprintf("%T", res)
			if i := strings.LastIndex(rtype, "."); i >= 0 {
				rtype = rtype[i+1:]
			}

			for probeType, cfg := range map[string]*ProbeConfig{
				"startup":   probes.Startup,
				"liveness":  probes.Liveness,
				"readiness": probes.Readiness,
			} {
				if cfg == nil || cfg.Profile == "" {
					continue
				}
				mixed := cfg.InitialDelaySeconds != nil ||
					cfg.PeriodSeconds != nil ||
					cfg.FailureThreshold != nil ||
					cfg.SuccessThreshold != nil ||
					cfg.TimeoutSeconds != nil
				out = append(out, ProbeProfileEntry{
					Phase:        phase,
					Resource:     rtype,
					ResourceName: rname,
					ProbeType:    probeType,
					Profile:      cfg.Profile,
					Mixed:        mixed,
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
