package types

// ResourceQuotaProfileEntry describes a single profile reference found in a
// ResourceQuotaTemplateSource. Used by katalog validation to fail fast on unknown
// profiles and to enforce mutual exclusivity with explicit hard limits.
type ResourceQuotaProfileEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	ResourceName string // ResourceQuota name template (may be Empty()
	Profile      string // raw profile name as written in the katalog
	Mixed        bool   // true when profile is set alongside an explicit hard map
}

type resourceQuotaProfiled interface {
	getResourceQuotaProfile() string
	resourceQuotaProfileMixed() bool
}

func (t ResourceQuotaTemplateSource) getResourceQuotaProfile() string { return t.Profile }
func (t ResourceQuotaTemplateSource) resourceQuotaProfileMixed() bool { return len(t.Hard) > 0 }

// CollectResourceQuotaProfileEntries returns all profile references declared for
// this CRD's resourceQuotas across OnCreate, OnReconcile, and OnDelete.
// Only entries with a non-empty Profile string are returned.
func (c *CRDEntry) CollectResourceQuotaProfileEntries() []ResourceQuotaProfileEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []ResourceQuotaProfileEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			rp, ok := res.(resourceQuotaProfiled)
			if !ok {
				return
			}
			profile := rp.getResourceQuotaProfile()
			if profile == "" {
				return
			}

			var rname string
			if n, ok := res.(namer); ok {
				rname = n.GetName()
			}

			out = append(out, ResourceQuotaProfileEntry{
				Phase:        phase,
				ResourceName: rname,
				Profile:      profile,
				Mixed:        rp.resourceQuotaProfileMixed(),
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
