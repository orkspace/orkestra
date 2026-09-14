package validate

import (
	"fmt"
)

// validatePreReconcile walks a preReconcile config and applies the following rules
//   - EnqueueGate should not declare 'eventAware'. Only makes sense in reconcileGate
func (e *executor) validatePreReconcile() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		pr := crd.OperatorBox.PreReconcile
		if pr == nil {
			continue
		}

		// validate eventAware
		eGate := pr.EnqueueGate
		if eGate != nil && eGate.IsEventAware() {
			return fmt.Errorf("%s CRD: %q: operatorBox.preReconcile.enqueueGate.eventAware 'event aware' is only valid in reconcileGate.", failureMark(), crdName)
		}
	}
	return nil
}
