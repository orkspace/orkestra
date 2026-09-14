package validate

import (
	"fmt"
	"strings"
)

// validateCRDEntryLabels validates the labels: block on each CRD entry.
//
// Enforces:
//  1. Label keys must be valid Kubernetes qualified names (static — no templates).
//  2. Label values must be valid Go templates (they are resolved at reconcile time
//     against the CR object, so {{ .metadata.name }} etc. are allowed).
func (e *executor) validateCRDEntryLabels() error {
	funcMap := buildFuncMapForValidation(e.k.Notes)
	for crdName, crd := range e.k.Enabled() {
		if !crd.HasUserLabels() {
			continue
		}
		for key, value := range crd.Labels {
			if isTemplate(key) {
				return fmt.Errorf("%s CRD %q: labels: key %q must be a static label key, not a template", failureMark(), crdName, key)
			}
			if errs := isValidLabelKey(key); len(errs) > 0 {
				return fmt.Errorf("%s CRD %q: labels: key %q is not a valid Kubernetes label key: %s", failureMark(), crdName, key, strings.Join(errs, "; "))
			}
			if err := validateTemplate("labels", crdName, key, "value", value, funcMap); err != nil {
				return err
			}
		}
	}
	return nil
}
