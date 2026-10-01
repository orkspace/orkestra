//go:build !runtime && !gateway

package cli

import (
	"path/filepath"
	"sort"

	"github.com/orkspace/orkestra/examples"
)

// packPaths maps CLI pack names to their paths inside the embedded FS.
// Packs map to top-level directories in the examples/ tree.
type Pack struct {
	Name        string
	Description string
	Path        string
	Order       int // for sorting in --list
}

var Packs = map[string]Pack{
	"beginner": {
		Name:        "beginner",
		Description: "Start here. Simple CRDs, Deployments, Services.",
		Path:        "beginner",
		Order:       1,
	},
	"intermediate": {
		Name:        "intermediate",
		Description: "Multi-resource patterns, when/or, Komposer basics.",
		Path:        "intermediate",
		Order:       2,
	},
	"advanced": {
		Name:        "advanced",
		Description: "Hooks, constructors, validation/mutation, registries.",
		Path:        "advanced",
		Order:       3,
	},
	"security": {
		Name:        "security",
		Description: "Deletion protection, namespace protection, admission webhooks.",
		Path:        "security",
		Order:       4,
	},
	"resilience": {
		Name:        "resilience",
		Description: "Operators that stay running — panic recovery, degraded state, isolation.",
		Path:        "resilience",
		Order:       5,
	},
	"use-cases": {
		Name:        "use-cases",
		Description: "Full-stack, cross-CRD, external gates, once-secrets.",
		Path:        "use-cases",
		Order:       6,
	},
	"registry-guide": {
		Name:        "registry-guide",
		Description: "End-to-end registry workflow: consume, build motifs, publish, compose, upgrade, deprecate, typed operators, CI.",
		Path:        "registry-guide",
		Order:       7,
	},
	"ecosystem-composition": {
		Name:        "ecosystem-composition",
		Description: "Wrap ArgoCD, cert-manager, Prometheus Operator, and Crossplane behind internal CRDs.",
		Path:        "ecosystem-composition",
		Order:       8,
	},
	"from-controller-runtime": {
		Name:        "from-controller-runtime",
		Description: "Migrate an existing controller-runtime operator to Orkestra: declarative, hooks, or constructor.",
		Path:        "from-controller-runtime",
		Order:       9,
	},
}

func GetPack(name string) (Pack, bool) {
	if p, ok := Packs[name]; ok {
		return p, true
	}
	// Sub-path fallback: any valid directory in the embedded FS works as a pack.
	// ork init my-project --pack use-cases/multi-tenancy extracts into my-project/multi-tenancy.
	if f, err := examples.FS.Open(name); err == nil {
		f.Close()
		return Pack{Name: filepath.Base(name), Path: name}, true
	}
	return Pack{}, false
}

func ListPacks() []Pack {
	out := make([]Pack, 0, len(Packs))
	for _, p := range Packs {
		out = append(out, p)
	}

	// Sort
	sort.Slice(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}

// Helpers
func (p Pack) isBeginnerPack() bool              { return p.Name == "beginner" }
func (p Pack) isCanonicalPack() bool             { return p.Name == "." }
func (p Pack) isIntermediatePack() bool          { return p.Name == "intermediate" }
func (p Pack) isAdvancedPack() bool              { return p.Name == "advanced" }
func (p Pack) isSecurityPack() bool              { return p.Name == "security" }
func (p Pack) isUseCasesPack() bool              { return p.Name == "use-cases" }
func (p Pack) isDeveloperPack() bool             { return p.Name == "developer" }
func (p Pack) isRegistryGuidePack() bool         { return p.Name == "registry-guide" }
func (p Pack) isFromControllerRuntimePack() bool { return p.Name == "from-controller-runtime" }
func (p Pack) isEcosystemCompositionPack() bool  { return p.Name == "ecosystem-composition" }
func (p Pack) isResiliencePack() bool            { return p.Name == "resilience" }

func (p Pack) firstExample() string {
	switch {
	case p.isBeginnerPack(), p.isCanonicalPack():
		return "01-hello-website"
	case p.isIntermediatePack():
		return "04-multi-resource"
	case p.isAdvancedPack():
		return "07-validation-mutation"
	case p.isSecurityPack():
		return "admission"
	case p.isUseCasesPack():
		return "full-stack-app"
	case p.isDeveloperPack():
		return "01-one-project"
	case p.isRegistryGuidePack():
		return "00-consume"
	case p.isFromControllerRuntimePack():
		return "00-controller-runtime-baseline"
	case p.isEcosystemCompositionPack():
		return "00-argocd"
	case p.isResiliencePack():
		return "safe-reconcile"
	default:
		return ""
	}
}

func (p Pack) String() string {
	ex := p.firstExample()
	if ex == "" {
		return p.Name + " — " + p.Description
	}
	return p.Name + " — " + p.Description + " (start with: examples/" + p.Path + "/" + ex + ")"
}
