package types

// RollingUpdateProfileEntry describes a single rollingUpdate.profile reference found in a
// DeploymentTemplateSource. Used by katalog validation to fail fast on unknown profiles
// and to enforce mutual exclusivity with explicit maxSurge/maxUnavailable.
type RollingUpdateProfileEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	ResourceName string // Deployment name template (may be Empty()
	Profile      string // raw profile name as written in the katalog
	Mixed        bool   // true when profile is set alongside explicit maxSurge or maxUnavailable
}

type rollingUpdateProfiled interface {
	GetRollingUpdate() *RollingUpdateBehavior
}

func (t DeploymentTemplateSource) GetRollingUpdate() *RollingUpdateBehavior  { return t.RollingUpdate }
func (t StatefulSetTemplateSource) GetRollingUpdate() *RollingUpdateBehavior { return t.RollingUpdate }
func (t ReplicaSetTemplateSource) GetRollingUpdate() *RollingUpdateBehavior  { return t.RollingUpdate }

// CollectRollingUpdateProfileEntries returns all rollingUpdate.profile references declared
// for this CRD across OnCreate, OnReconcile, and OnDelete.
// Only entries with a non-empty Profile string are returned.
func (c *CRDEntry) CollectRollingUpdateProfileEntries() []RollingUpdateProfileEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []RollingUpdateProfileEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			rp, ok := res.(rollingUpdateProfiled)
			if !ok {
				return
			}
			b := rp.GetRollingUpdate()
			if b == nil || b.Profile == "" {
				return
			}

			var rname string
			if n, ok := res.(namer); ok {
				rname = n.GetName()
			}

			mixed := b.MaxSurge != "" || b.MaxUnavailable != ""

			out = append(out, RollingUpdateProfileEntry{
				Phase:        phase,
				ResourceName: rname,
				Profile:      b.Profile,
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
