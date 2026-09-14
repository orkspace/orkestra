package validate

import "fmt"

// validateRequeue checks that requeue.after is either a template expression
// or a valid duration string accepted by utils.ParseTimeDuration.
// Empty string is allowed — it means no requeue.
func (e *executor) validateRequeue() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		rc := crd.OperatorBox.Reconciler
		if rc == nil || rc.Requeue == nil {
			continue
		}
		after := rc.Requeue.After
		if after == "" || isTemplate(after) {
			continue
		}
		if _, err := parseTimeDuration(after); err != nil {
			return fmt.Errorf("%s crd %q: requeue.after %q is not a valid duration or template expression. "+
				"Use a Go duration string (e.g. \"30s\", \"5m\") or a template (e.g. '{{ .spec.interval | default \"60s\" }}')",
				failureMark(), crdName, after)
		}
	}
	return nil
}
