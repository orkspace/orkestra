package types

// PDBProfileEntry describes a single behavior.profile reference found in a
// PDBTemplateSource. Used by katalog validation to fail fast on unknown profiles
// and to enforce mutual exclusivity with explicit minAvailable/maxUnavailable.
type PDBProfileEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	ResourceName string // PDB name template (may be Empty()
	Profile      string // raw profile name as written in the katalog
	Mixed        bool   // true when profile is set alongside explicit minAvailable or maxUnavailable
}

type pdbProfiled interface {
	GetPDBBehavior() *PDBBehavior
}

func (t PDBTemplateSource) GetPDBBehavior() *PDBBehavior { return t.Behavior }

// CollectPDBProfileEntries returns all behavior.profile references declared for
// this CRD across OnCreate, OnReconcile, and OnDelete.
// Only entries with a non-empty Profile string are returned.
func (c *CRDEntry) CollectPDBProfileEntries() []PDBProfileEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []PDBProfileEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			pp, ok := res.(pdbProfiled)
			if !ok {
				return
			}
			b := pp.GetPDBBehavior()
			if b == nil || b.Profile == "" {
				return
			}

			var rname string
			if n, ok := res.(namer); ok {
				rname = n.GetName()
			}

			mixed := b.MinAvailable != "" || b.MaxUnavailable != ""

			out = append(out, PDBProfileEntry{
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
