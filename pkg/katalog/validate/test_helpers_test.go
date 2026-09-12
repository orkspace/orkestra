package validate

import (
	"github.com/orkspace/orkestra/pkg/katalog"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// newExec wraps a *katalog.Katalog in an executor for use in white-box tests.
func newExec(k *katalog.Katalog) *executor {
	return &executor{k: k}
}

// newKatalogExec builds an executor with pre-set CRDs for tests.
func newKatalogExec(crds map[string]orktypes.CRDEntry) *executor {
	return newExec(katalog.NewKatalogForTest(crds))
}

// crd returns a test CRDEntry by name.
func (e *executor) crd(name string) *orktypes.CRDEntry {
	crd, ok := e.k.Enabled()[name]
	if !ok {
		return nil
	}
	return &crd
}

// serveEntry builds a minimal serve-enabled CRDEntry for tests.
func serveEntry(kind, targetName string) orktypes.CRDEntry {
	tv := orktypes.ServeTargetValue{}
	if targetName != "" {
		tv.Entries = map[string]*orktypes.ServeTargetConfig{
			targetName: {Primary: true},
		}
	}
	return orktypes.CRDEntry{
		APITypes: orktypes.APITypes{Kind: kind},
		Serve: &orktypes.ServeConfig{
			Enabled: true,
			Target:  tv,
		},
	}
}

// withAliases merges alias entries into a CRDEntry's ServeTarget.
func withAliases(e orktypes.CRDEntry, aliases map[string]*orktypes.ServeTargetConfig) orktypes.CRDEntry {
	if e.Serve.Target.Entries == nil {
		e.Serve.Target.Entries = make(map[string]*orktypes.ServeTargetConfig)
	}
	for name, cfg := range aliases {
		e.Serve.Target.Entries[name] = cfg
	}
	return e
}
