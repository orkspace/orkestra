package katalog

import (
	"fmt"
	"text/template"

	"github.com/orkspace/orkestra/pkg/note"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	"k8s.io/apimachinery/pkg/api/validate/content"
)

// ── utils aliases ────────────────────────────────────────────────────────────
// Import utils once here. All other files in this package use these names
// directly — no per-file utils import needed.

var (
	// colors / styles
	yellow = utils.Yellow
	red    = utils.Red

	// marks and icons
	failureMark = utils.FailureMark
	warningMark = utils.WarningMark

	// duration parsing
	parseTimeDuration = utils.ParseTimeDuration

	// helpers
	exit              = utils.Exit
	toStringSet       = utils.ToStringSet
	isNestedPath      = utils.IsNestedPath
	isTemplate        = orktypes.IsTemplate
	isValidLabelKey   = content.IsLabelKey
	isValidLabelValue = content.IsLabelValue
	isValidK8sName    = utils.ValidKubernetesName
	readLocal         = utils.ReadLocal
)

func boolPtr(b bool) *bool { return &b }

// buildFuncMapForValidation builds a stub FuncMap that includes all registered
// note functions (so cross-note references compile) and all built-in functions.
// User-defined notes from the Katalog are added as stubs — only parsing is
// checked, not execution.
func buildFuncMapForValidation(notes orktypes.NoteRegistry) template.FuncMap {
	builtins := note.Map()
	funcMap := make(template.FuncMap, len(builtins)+len(notes.Functions))
	for k, v := range builtins {
		funcMap[k] = v
	}
	for _, n := range notes.Functions {
		funcMap[n.Name] = func() interface{} { return "" }
	}
	return funcMap
}

func validateTemplate(caller, crdName, field, location, expr string, funcMap template.FuncMap) error {
	if _, err := template.New("").Funcs(funcMap).Parse(expr); err != nil {
		return fmt.Errorf("%s CRD %q: %s %q: %s: invalid template: %s", failureMark(), crdName, caller, field, location, err.Error())
	}
	return nil
}
