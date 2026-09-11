package validate

import (
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/domain"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateEventEntries validates operatorBox.events across all enabled CRDs.
//
// Event entries reuse WatchEntry routing semantics for primary-key resolution
// and enqueue admission. Event-specific fields are validated separately.
//
// Enforces:
//  1. Each Event entry must declare at least one matching or routing field.
//  2. regarding and related use ManagedResource matching semantics.
//  3. keyFrom follows the same validation rules as watch entries.
//  4. Duplicate Event entries are rejected.
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

	type key struct {
		reason              string
		action              string
		eventType           string
		reportingController string
		reportingInstance   string
		regarding           string
		related             string
		namespace           string
		name                string
	}

	seen := make(map[key]bool, len(entries))

	for i, event := range entries {
		if err := validateEventEntry(crdName, i, event); err != nil {
			return err
		}

		if event.Name == "" {
			return fmt.Errorf("%s crd %q: events[%d]: event name is required", failureMark(), crdName, i)
		}

		watch := event.ToWatchEntry(crd)

		if invalid := watch.InvalidOnValues(); len(invalid) > 0 {
			return fmt.Errorf("%s crd %q: event[%d] %s/%s: unknown on: value(s) [%s] — valid values: %s",
				failureMark(), crdName, i, watch.APIVersion, watch.Kind,
				strings.Join(invalid, ", "), strings.Join(orktypes.ValidObserveEvents(), ", "))
		}

		if err := validateWatchKeyFrom(crdName, i, watch); err != nil {
			return err
		}

		if err := validateWatchKeyFrom(crdName, i, watch); err != nil {
			return err
		}

		k := key{
			reason:              event.Reason,
			action:              event.Action,
			eventType:           event.Type,
			reportingController: event.ReportingController,
			reportingInstance:   event.ReportingInstance,
			regarding:           managedResourceKey(event.Regarding),
			related:             managedResourceKey(event.Related),
			namespace:           event.Namespace,
			name:                event.Name,
		}

		if seen[k] {
			return fmt.Errorf("%s crd %q: duplicate event entry at events[%d] — event entries must be unique",
				failureMark(), crdName, i)
		}
		seen[k] = true
	}

	return nil
}

func validateEventEntry(crdName string, idx int, event orktypes.EventEntry) error {
	if !hasEventMatcher(event) {
		return fmt.Errorf("%s crd %q: events[%d]: event entry must declare at least one matching or routing field",
			failureMark(), crdName, idx)
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
		event.Name != "" ||
		event.KeyFrom != nil
}

func managedResourceKey(r *domain.ManagedResource) string {
	if r == nil {
		return ""
	}

	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		r.APIVersion,
		r.Kind,
		r.Group,
		r.Version,
		r.Plural,
		r.Name,
		r.Namespace,
	)
}
