package validate

import (
	"fmt"
	"text/template"

	"github.com/orkspace/orkestra/pkg/note"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	"k8s.io/apimachinery/pkg/api/validate/content"
)

var (
	yellow      = utils.Yellow
	red         = utils.Red
	failureMark = utils.FailureMark
	warningMark = utils.WarningMark

	parseTimeDuration = utils.ParseTimeDuration

	toStringSet       = utils.ToStringSet
	isNestedPath      = utils.IsNestedPath
	isTemplate        = orktypes.IsTemplate
	isValidLabelKey   = content.IsLabelKey
	isValidLabelValue = content.IsLabelValue
	isValidK8sName    = utils.ValidKubernetesName
	validResolverName = orktmpl.ValidResolverName
)

func boolPtr(b bool) *bool { return &b }

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
