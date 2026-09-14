package types

// NetworkPolicyProfileEntry describes a single profile reference found in a
// NetworkPolicyTemplateSource. Used by katalog validation to fail fast on unknown
// profiles and to enforce mutual exclusivity with explicit ingress/egress/policyTypes.
type NetworkPolicyProfileEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	ResourceName string // NetworkPolicy name template (may be Empty()
	Profile      string // raw profile name as written in the katalog
	Mixed        bool   // true when profile is set alongside explicit ingress/egress/policyTypes
}

type networkPolicyProfiled interface {
	getNetworkPolicyProfile() string
	networkPolicyProfileMixed() bool
}

func (t NetworkPolicyTemplateSource) getNetworkPolicyProfile() string { return t.Profile }
func (t NetworkPolicyTemplateSource) networkPolicyProfileMixed() bool {
	return len(t.Ingress) > 0 || len(t.Egress) > 0 || len(t.PolicyTypes) > 0
}

// CollectNetworkPolicyProfileEntries returns all profile references declared for
// this CRD's networkPolicies across OnCreate, OnReconcile, and OnDelete.
// Only entries with a non-empty Profile string are returned.
func (c *CRDEntry) CollectNetworkPolicyProfileEntries() []NetworkPolicyProfileEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []NetworkPolicyProfileEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			pp, ok := res.(networkPolicyProfiled)
			if !ok {
				return
			}
			profile := pp.getNetworkPolicyProfile()
			if profile == "" {
				return
			}

			var rname string
			if n, ok := res.(namer); ok {
				rname = n.GetName()
			}

			out = append(out, NetworkPolicyProfileEntry{
				Phase:        phase,
				ResourceName: rname,
				Profile:      profile,
				Mixed:        pp.networkPolicyProfileMixed(),
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
