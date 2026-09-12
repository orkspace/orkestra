package validate

import (
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/domain"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateEventEntries validates operatorBox.observe.events across all enabled CRDs.
//
// Event entries reuse WatchEntry routing semantics for primary-key resolution
// and enqueue admission. Event-specific fields are validated separately.
//
// Enforces:
//  1. Each Event entry must declare at least one matching or routing field.
//  2. regarding and related use ManagedResource matching semantics.
//  3. keyFrom follows the same validation rules as watch entries.
//  4. on: values are valid observe event types.
func (e *executor) validateEventEntries() error {
	for crdName, crd := range e.k.Enabled() {
		if err := validateCRDEventEntries(crdName, crd); err != nil {
			return err
		}
	}
	return nil
}

func validateCRDEventEntries(crdName string, crd orktypes.CRDEntry) error {
	entries := crd.EventEntries()
	if len(entries) == 0 {
		return nil
	}

	for name, event := range entries {
		if err := validateEventEntry(crdName, name, event); err != nil {
			return err
		}

		watch := event.ToWatchEntry(crd)

		obs := crd.OperatorBox.Observe

		if invalid := obs.InvalidOnValues(event.On); len(invalid) > 0 {
			return fmt.Errorf("%s crd %q: events[%q] %s/%s: unknown on: value(s) [%s] — valid values: %s",
				failureMark(), crdName, name, watch.APIVersion, watch.Kind,
				strings.Join(invalid, ", "), strings.Join(orktypes.ValidObserveEvents(), ", "))
		}

		if err := validateWatchKeyFrom(crdName, name, watch); err != nil {
			return err
		}
	}

	return nil
}

func validateEventEntry(crdName, name string, event orktypes.EventEntry) error {
	if err := validResolverName(name); err != nil {
		return fmt.Errorf("%s crd %q: events[%q]: %w",
			failureMark(), crdName, name, err)
	}
	if !hasEventMatcher(event) {
		return fmt.Errorf("%s crd %q: events[%q]: event entry must declare at least one matching or routing field",
			failureMark(), crdName, name)
	}

	return nil
}

func hasEventMatcher(event orktypes.EventEntry) bool {
	return event.Reason != "" ||
		event.Action != "" ||
		event.Type != "" ||
		event.ReportingController != "" ||
		event.ReportingInstance != "" ||
		event.Regarding != nil ||
		event.Related != nil ||
		event.Namespace != "" ||
		event.KeyFrom != nil
}

func managedResourceKey(r *domain.ManagedResource) string {
	if r == nil {
		return ""
	}

	return fmt.Sprintf("%s|%s|%s|%s|%s|%s",
		r.APIVersion,
		r.Kind,
		r.Group,
		r.Version,
		r.Plural,
		r.Namespace,
	)
}
