package motif

import (
	"fmt"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

func expandIncludes(m *orktypes.Motif, dir string) error {
	if err := orktypes.ExpandNotesInclude(&m.Notes, dir); err != nil {
		return err
	}
	if err := orktypes.ExpandProfileInclude(&m.Profiles, dir); err != nil {
		return err
	}
	if err := orktypes.ExpandStatusInclude(m.Status, dir); err != nil {
		return err
	}
	if m.Admission != nil {
		if err := orktypes.ExpandValidationInclude(m.Admission.Validation, dir); err != nil {
			return err
		}
		if err := orktypes.ExpandMutationInclude(m.Admission.Mutation, dir); err != nil {
			return err
		}
		if m.Admission.HasValidationExternal() {
			var err error
			m.Admission.Validation.External, err = orktypes.ExpandExternalCalls(m.Admission.Validation.External, dir)
			if err != nil {
				return fmt.Errorf("admission.validation.external: %w", err)
			}
		}
		if m.Admission.HasMutationExternal() {
			var err error
			m.Admission.Mutation.External, err = orktypes.ExpandExternalCalls(m.Admission.Mutation.External, dir)
			if err != nil {
				return fmt.Errorf("admission.mutation.external: %w", err)
			}
		}
	}
	return nil
}
