package validate

import (
	"fmt"
	"strings"
	"text/template"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateServeFieldTranslation validates the value and values blocks on
// serve.fields entries:
//
//   - field names must not contain hyphens (breaks Go template identifier syntax)
//   - path and values are mutually exclusive
//   - value and values are mutually exclusive
//   - values must have at least one entry
//   - values keys must not be empty and must be valid path format
//   - all template expressions (value, values) must compile
//   - expressions must not be empty strings
func (e *executor) validateServeFieldTranslation() error {
	funcMap := buildFuncMapForValidation(e.k.Notes)

	for crdName, crd := range e.k.EnabledCRDs() {
		if !crd.ServeEnabled() || !crd.HasServeFields() {
			continue
		}
		for fieldName, config := range crd.Serve.Fields {
			if err := validateFieldTranslation(crdName, fieldName, config, funcMap); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateFieldTranslation(
	crdName, fieldName string,
	config orktypes.ServeFieldConfig,
	funcMap template.FuncMap,
) error {
	if strings.Contains(fieldName, "-") {
		return fmt.Errorf(`
──────────────────────────────────────────────
%s CRD %q: serve.fields %q: field name must not contain hyphens

Hyphenated names cannot be used as Go template identifiers — {{ .request.my-field }}
is invalid template syntax. Use camelCase or underscores instead: %q
──────────────────────────────────────────────`, failureMark(), crdName, fieldName, strings.ReplaceAll(fieldName, "-", "_"))
	}

	hasVal := config.HasValue()
	hasVals := config.HasValues()
	hasPath := config.Path != ""

	if hasPath && hasVals {
		return fmt.Errorf(`
──────────────────────────────────────────────
%s CRD %q: serve.fields %q: path and values are mutually exclusive

path sets a single destination; values fans out to multiple destinations.
Remove path and write the full spec paths directly in values instead.
──────────────────────────────────────────────`, failureMark(), crdName, fieldName)
	}

	if hasVal && hasVals {
		return fmt.Errorf(`
──────────────────────────────────────────────
%s serve.fields %q: value and values are mutually exclusive
   CRD: %s

Use value for a single destination, values for fanout to multiple paths.
──────────────────────────────────────────────`, failureMark(), fieldName, crdName)
	}

	if hasVal {
		if err := validateTemplate("serve.fields", crdName, fieldName, "value", config.Value, funcMap); err != nil {
			return err
		}
	}

	if hasVals {
		if len(config.Values) == 0 {
			return fmt.Errorf("%s CRD %q: serve.fields %q: values must have at least one entry", failureMark(), crdName, fieldName)
		}
		for specPath, expr := range config.Values {
			if specPath == "" {
				return fmt.Errorf("%s CRD %q: serve.fields %q: values path key must not be empty", failureMark(), crdName, fieldName)
			}
			if err := validatePathFormat(specPath); err != nil {
				return fmt.Errorf("%s CRD %q: serve.fields %q: values key %q is not a valid path: %s", failureMark(), crdName, fieldName, specPath, err.Error())
			}
			if expr == "" {
				return fmt.Errorf("%s CRD %q: serve.fields %q: values[%q] expression must not be empty", failureMark(), crdName, fieldName, specPath)
			}
			if err := validateTemplate("serve.fields", crdName, fieldName, fmt.Sprintf("values[%q]", specPath), expr, funcMap); err != nil {
				return err
			}
		}
	}

	return nil
}
