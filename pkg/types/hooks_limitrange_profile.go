package types

// LimitRangeProfileEntry describes a single profile reference found in a
// LimitRangeTemplateSource. Used by katalog validation to fail fast on unknown
// profiles and to enforce mutual exclusivity with explicit limits.
type LimitRangeProfileEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	ResourceName string // LimitRange name template (may be Empty()
	Profile      string // raw profile name as written in the katalog
	Mixed        bool   // true when profile is set alongside an explicit Limits list
}

type limitRangeProfiled interface {
	getLimitRangeProfile() string
	limitRangeProfileMixed() bool
}

func (t LimitRangeTemplateSource) getLimitRangeProfile() string { return t.Profile }
func (t LimitRangeTemplateSource) limitRangeProfileMixed() bool { return len(t.Limits) > 0 }

// CollectLimitRangeProfileEntries returns all profile references declared for
// this CRD's limitRanges across OnCreate, OnReconcile, and OnDelete.
// Only entries with a non-empty Profile string are returned.
func (c *CRDEntry) CollectLimitRangeProfileEntries() []LimitRangeProfileEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []LimitRangeProfileEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			rp, ok := res.(limitRangeProfiled)
			if !ok {
				return
			}
			profile := rp.getLimitRangeProfile()
			if profile == "" {
				return
			}

			var rname string
			if n, ok := res.(namer); ok {
				rname = n.GetName()
			}

			out = append(out, LimitRangeProfileEntry{
				Phase:        phase,
				ResourceName: rname,
				Profile:      profile,
				Mixed:        rp.limitRangeProfileMixed(),
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
