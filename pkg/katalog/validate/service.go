package validate

import (
	"fmt"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateService runs all service-related validations.
func (e *executor) validateService() error {
	if err := validateServiceType(e); err != nil {
		return err
	}
	if err := validateServiceProtocol(e); err != nil {
		return err
	}
	return nil
}

//
// ─────────────────────────────────────────────────────────────
// Validation logic
// ─────────────────────────────────────────────────────────────
//

// validateServiceType validates the Service.Type field for all CRDs.
func validateServiceType(e *executor) error {
	for _, crd := range e.k.EnabledCRDs() {
		if !crd.HasAnyServices() {
			continue
		}

		// onCreate
		if crd.HasOnCreate() {
			for _, s := range crd.OperatorBox.OnCreate.Services {
				if !orktypes.IsValidServiceType(s.Type) {
					return errInvalidServiceType(s.Type)
				}
			}
		}

		// onReconcile
		if crd.HasOnReconcile() {
			for _, s := range crd.OperatorBox.OnReconcile.Services {
				if !orktypes.IsValidServiceType(s.Type) {
					return errInvalidServiceType(s.Type)
				}
			}
		}
	}

	return nil
}

// validateServiceProtocol validates the Service.Protocol field for all CRDs.
func validateServiceProtocol(e *executor) error {
	for _, crd := range e.k.EnabledCRDs() {
		if !crd.HasAnyServices() {
			continue
		}

		// onCreate
		if crd.HasOnCreate() {
			for _, s := range crd.OperatorBox.OnCreate.Services {
				if !orktypes.IsValidProtocol(s.Protocol) {
					return errInvalidProtocol(s.Protocol)
				}
			}
		}

		// onReconcile
		if crd.HasOnReconcile() {
			for _, s := range crd.OperatorBox.OnReconcile.Services {
				if !orktypes.IsValidProtocol(s.Protocol) {
					return errInvalidProtocol(s.Protocol)
				}
			}
		}
	}

	return nil
}

//
// ─────────────────────────────────────────────────────────────
// Reusable error helpers
// ─────────────────────────────────────────────────────────────
//

func errInvalidServiceType(t string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s Invalid Service type: %q

Allowed values:
  • ClusterIP
  • NodePort
  • LoadBalancer

Docs: https://kubernetes.io/docs/concepts/services-networking/service/
──────────────────────────────────────────────`, failureMark(), t)
}

func errInvalidProtocol(p string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s Invalid protocol: %q

Allowed values:
  • TCP
  • UDP
  • SCTP

Docs: https://kubernetes.io/docs/concepts/services-networking/service/#protocol
──────────────────────────────────────────────`, failureMark(), p)
}
