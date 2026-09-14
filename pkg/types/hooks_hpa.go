package types

import (
	"fmt"
	"strings"
)

// HPAProfileEntry describes a single behavior.profile reference found in an
// HPATemplateSource. Used by katalog validation to fail fast on unknown profiles
// and to enforce mutual exclusivity with explicit scaleUp/scaleDown fields.
type HPAProfileEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	ResourceName string // HPA name template (may be Empty()
	Profile      string // raw profile name as written in the katalog
	Mixed        bool   // true when profile is set alongside explicit scaleUp or scaleDown
}

// hpaProfiled is satisfied by HPATemplateSource.
type hpaProfiled interface {
	GetHPABehavior() *HPABehavior
}

func (t HPATemplateSource) GetHPABehavior() *HPABehavior { return t.Behavior }

// CollectHPAProfileEntries returns all behavior.profile references declared for
// this CRD across OnCreate, OnReconcile, and OnDelete.
// Only entries with a non-empty Profile string are returned.
func (c *CRDEntry) CollectHPAProfileEntries() []HPAProfileEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []HPAProfileEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			hp, ok := res.(hpaProfiled)
			if !ok {
				return
			}
			b := hp.GetHPABehavior()
			if b == nil || b.Profile == "" {
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
			_ = rtype

			mixed := b.ScaleUp != nil || b.ScaleDown != nil

			out = append(out, HPAProfileEntry{
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
