// Package pipeline owns the top-level Katalog build sequence:
// merge → expand motifs → validate → wire runtime objects.
// Callers that need a fully ready *katalog.Katalog should use this package.
package pipeline

import (
	"fmt"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/validate"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/merger"
	ork_runtime "github.com/orkspace/orkestra/pkg/typeregistry"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
)

// NewKatalog returns a fully built and validated Katalog.
// Exits the process on any error
func NewKatalog(kfg *konfig.Konfig, m *merger.Merger) *katalog.Katalog {
	k := &katalog.Katalog{}
	k.SetKonfig(kfg)

	paths := kfg.Katalog().Paths()

	ork_runtime.RegisterRuntimeObjects()

	entries, err := k.KomposeRuntimeKatalog(kfg, m, paths...)
	if err != nil {
		utils.Exit(err)
	}

	if len(entries) == 0 && !k.IsStandaloneGateway() {
		utils.Exit(fmt.Errorf("validation error: katalog empty"))
	}

	for _, crd := range entries {
		if len(orktypes.ObjectRegistry) == 0 && !crd.IsDynamic() {
			utils.Exit(fmt.Errorf(
				"ObjectRegistry is empty — run 'ork generate registry --file <my-katalog.yaml>' first",
			))
		}
	}

	if err := validate.Execute(k, kfg); err != nil {
		utils.Exit(err)
	}

	if err := k.CheckDeprecationPolicy(); err != nil {
		utils.Exit(err)
	}

	k, err = k.UpdateResourceMapAndReturn()
	if err != nil {
		utils.Exit(err)
	}

	return k
}

// BuildExpanded is the canonical pipeline for CLI commands that need a fully
// ready Katalog: merge → expand motifs → validate.
func BuildExpanded(kfg *konfig.Konfig, m *merger.Merger) (*katalog.Katalog, error) {
	k := &katalog.Katalog{}
	if _, err := k.KomposeRuntimeKatalog(kfg, m); err != nil {
		return nil, err
	}
	if err := validate.Execute(k, kfg); err != nil {
		return nil, fmt.Errorf("katalog validate: %w", err)
	}
	return k, nil
}
