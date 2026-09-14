package katalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/orkspace/orkestra/pkg/konfig"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// DependencyDisplay holds pre-computed dependency graph data for ork validate --full.
type DependencyDisplay struct {
	StartupOrder []string                     // CRD names in deterministic startup order
	Conditions   map[string]map[string]string // CRD name → dep name → condition string
}

// DependencyDisplayData builds dependency display data for ork validate --full.
// Returns nil when no enabled CRD declares any dependsOn — callers should skip
// the section entirely in that case.
func (k *Katalog) DependencyDisplayData() *DependencyDisplay {
	hasDeps := false
	for _, crd := range k.enabledCRDs {
		if len(crd.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}
	if !hasDeps {
		return nil
	}

	dg := NewDependencyGraph(k)
	conditions := make(map[string]map[string]string)
	for name, crd := range k.enabledCRDs {
		if len(crd.DependsOn) == 0 {
			continue
		}
		m := make(map[string]string, len(crd.DependsOn))
		for dep, c := range crd.DependsOn {
			m[dep] = c.Condition
		}
		conditions[name] = m
	}

	return &DependencyDisplay{
		StartupOrder: dg.StartupOrder(),
		Conditions:   conditions,
	}
}

// SortedDependencyNames returns the dependency names for a CRD in sorted order,
// along with their conditions. Used for deterministic display.
func (dd *DependencyDisplay) SortedDeps(crdName string) []string {
	deps := dd.Conditions[crdName]
	if len(deps) == 0 {
		return nil
	}
	names := make([]string, 0, len(deps))
	for dep := range deps {
		names = append(names, dep)
	}
	sort.Strings(names)
	return names
}

func (k *Katalog) List() map[string]orktypes.CRDEntry {
	return k.Spec.CRDs
}

// All returns every CRD in the katalog, including disabled ones.
func (k *Katalog) All() map[string]orktypes.CRDEntry {
	return k.Spec.CRDs
}

// Useful Metadata
func (k *Katalog) Meta() orktypes.KatalogMeta {
	return k.metadata
}

// ProjectInfo returns the project information for use by control center.
// Populated by ork-doctor at generation time; empty in operator-authored Katalogs.
func (k *Katalog) ProjectInfo() interface{} {
	if k.projectInfo == nil {
		return nil
	}
	return k.projectInfo
}

// Projects returns the map of all project infos from the katalog metadata.
// Populated by the developer path (createdBy: orkdoctor) via ork doctor deploy.
func (k *Katalog) Projects() map[string]interface{} {
	return k.metadata.Projects
}

// Exists returns true if a CRD with the given name exists in the katalog.
func (k *Katalog) Exists(name string) bool {
	_, ok := k.Spec.CRDs[name]
	return ok
}

// Describe returns a human‑readable summary of a CRD.
func (k *Katalog) Describe(name string) (string, error) {
	crd, err := k.Get(name)
	if err != nil {
		return "", err
	}

	b := &strings.Builder{}
	fmt.Fprintf(b, "Name:        %s\n", crd.Name)
	fmt.Fprintf(b, "Group:       %s\n", crd.APITypes.Group)
	fmt.Fprintf(b, "Version:     %s\n", crd.APITypes.Version)
	fmt.Fprintf(b, "Kind:        %s\n", crd.APITypes.Kind)
	fmt.Fprintf(b, "Plural:      %s\n", crd.APITypes.Plural)
	fmt.Fprintf(b, "Namespaced:  %v\n", crd.Namespaced)
	if crd.IsNamespaced() {
		fmt.Fprintf(b, "Namespace:   %s\n", crd.Namespace)
	}
	fmt.Fprintf(b, "Workers:     %d\n", crd.OperatorBox.Reconciler.Workers)
	fmt.Fprintf(b, "Resync:      %s\n", crd.OperatorBox.Reconciler.Resync.String())
	fmt.Fprintf(b, "Enabled:     %v\n", crd.Enabled)

	deps := crd.DependsOn.Names()
	if len(deps) > 0 {
		fmt.Fprintf(b, "Dependencies: %v\n", strings.Join(deps, " "))
	} else {
		fmt.Fprint(b, "Dependencies: None")
	}

	fmt.Fprintf(b, "Description:\n%s\n", crd.Description)

	return b.String(), nil
}

// Explain returns a technical explanation of how Orkestra handles this CRD.
func (k *Katalog) Explain(name string) (string, error) {
	crd, err := k.Get(name)
	if err != nil {
		return "", err
	}

	b := &strings.Builder{}
	fmt.Fprintf(b, "%s (%s/%s)\n", crd.APITypes.Kind, crd.APITypes.Group, crd.APITypes.Version)
	fmt.Fprintf(b, "----------------------------------------\n")
	fmt.Fprintf(b, "API Path:     %s\n", crd.APITypes.APIPath)
	fmt.Fprintf(b, "GVK:          %s\n", crd.GroupVersionKind.String())
	fmt.Fprint(b, "List Type:    runtime.Object")
	fmt.Fprint(b, "Object Type:  runtime.Object")
	if crd.DefaultReconcile() {
		fmt.Fprint(b, "operatorBox:   Default\n")
	} else {
		fmt.Fprintf(b, "operatorBox:   %T\n", crd.OperatorBox.Constructor)
	}
	fmt.Fprintf(b, "Informer:     LIST, WATCH\n")

	deps := crd.DependsOn.Names()
	if len(deps) > 0 {
		fmt.Fprintf(b, "Dependencies: %v\n", strings.Join(deps, " "))
	} else {
		fmt.Fprint(b, "Dependencies: None")
	}

	return b.String(), nil
}

// Graph returns a map of CRD name → dependency names.
func (k *Katalog) Graph() map[string][]string {
	graph := make(map[string][]string, k.Len())
	for name, crd := range k.enabledCRDs {
		graph[name] = crd.DependsOn.Names()
	}
	return graph
}

// Order returns CRDs in dependency‑safe order (topological sort).
func (k *Katalog) Order() []string {
	depGraph := NewDependencyGraph(k)
	return depGraph.ShutdownOrder()
}

// Controllers returns a list of CRDs that have reconcilers.
func (k *Katalog) Controllers() []string {
	var out []string
	for _, crd := range k.enabledCRDs {
		if crd.OperatorBox.Constructor != nil && crd.DefaultReconcile() {
			out = append(out, crd.Name)
		}
	}
	return out
}

// CRDNames returns the names of all enabled CRDs.
func (k *Katalog) CRDNames() []string {
	names := make([]string, 0, k.Len())
	for name := range k.enabledCRDs {
		names = append(names, name)
	}
	return names
}

// Depends returns true if crdName depends on target.
func (k *Katalog) Depends(crdName, target string) bool {
	crd, err := k.Get(crdName)
	if err != nil {
		return false
	}
	_, ok := crd.DependsOn[target]
	return ok
}

// Dependents returns all CRDs that depend on the given CRD.
func (k *Katalog) Dependents(name string) []string {
	var out []string
	for _, crd := range k.enabledCRDs {
		if _, ok := crd.DependsOn[name]; ok {
			out = append(out, crd.Name)
		}
	}
	return out
}

// Enabled returns only the enabled CRDs in the katalog.
func (k *Katalog) Enabled() map[string]orktypes.CRDEntry {
	if k == nil {
		return nil
	}
	return k.enabledCRDs
}

// Get returns an enabled CRD by name.
func (k *Katalog) Get(name string) (*orktypes.CRDEntry, error) {
	crd, ok := k.enabledCRDs[name]
	if !ok {
		return nil, fmt.Errorf("crd not found in katalog")
	}
	return &crd, nil
}

// ToUI returns a UI-friendly representation of the merged Katalog.
// This method extracts only the fields needed for display in the Control Center:
//   - API version and kind (always "Katalog" at runtime)
//   - Metadata (name, description, version, author, license)
//   - All merged CRD definitions
//
// Internal fields (Scheme, GroupVersionKind, etc.) are excluded because they
// have `yaml:"-" json:"-"` tags and won't be serialized to JSON.
//
// This method is used by the /katalog/raw endpoint to provide a clean,
// readable view of the Katalog that created the current operator.
func (k *Katalog) ToUI() *orktypes.KatalogForUI {

	return &orktypes.KatalogForUI{
		APIVersion: k.APIVersion,
		Kind:       konfig.KatalogKind(),
		Metadata:   k.metadata,
		Spec: orktypes.KatalogSpecForUI{
			CRDs: k.Spec.CRDs,
		},
	}
}
