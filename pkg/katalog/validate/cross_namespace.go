package validate

import (
	"fmt"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateCrossNamespaceOps checks that every resource using the cross-namespace
// copy pattern has both fromNamespace and toNamespaces set together.
//
// Background: the copy pattern requires a source object (fromNamespace) and at
// least one destination (toNamespaces). Declaring only one of the two produces a
// silently broken resource — the runner skips the copy or reads from an
// unintended location. validate catches the mismatch early so users see a clear
// error at katalog load time rather than a confusing runtime failure.
//
// This validation is resource-type-agnostic: any *TemplateSource that introduces
// fromNamespace / toNamespaces must add its slice to checkCrossNamespaceItems
// below so the check is inherited automatically.
func (e *executor) validateCrossNamespaceOps() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		for _, phase := range []struct {
			name string
			ht   *orktypes.HookTemplates
		}{
			{"onCreate", crd.OperatorBox.OnCreate},
			{"onReconcile", crd.OperatorBox.OnReconcile},
			{"onDelete", crd.OperatorBox.OnDelete},
		} {
			if phase.ht == nil {
				continue
			}
			if err := checkCrossNamespaceHooks(crdName, phase.name, *phase.ht); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkCrossNamespaceHooks(crdName, phase string, ht orktypes.HookTemplates) error {
	if err := checkCrossNamespaceItems(crdName, phase, ht.Secrets); err != nil {
		return err
	}
	if err := checkCrossNamespaceItems(crdName, phase, ht.ConfigMaps); err != nil {
		return err
	}
	if err := checkCrossNamespaceItems(crdName, phase, ht.NetworkPolicies); err != nil {
		return err
	}
	if err := checkCrossNamespaceItems(crdName, phase, ht.ResourceQuotas); err != nil {
		return err
	}
	return checkCrossNamespaceItems(crdName, phase, ht.LimitRanges)
}

func checkCrossNamespaceItems[T orktypes.CrossNamespaceChecker](crdName, phase string, items []T) error {
	for _, item := range items {
		hasFrom := item.GetFromNamespace() != ""
		hasTo := len(item.GetToNamespaces()) > 0
		if hasFrom == hasTo {
			continue
		}
		if hasFrom {
			return fmt.Errorf("%s crd %q: %s/%s (phase %s) sets fromNamespace but not toNamespaces — both must be set together",
				failureMark(), crdName, item.GetKind(), item.GetName(), phase)
		}
		return fmt.Errorf("%s crd %q: %s/%s (phase %s) sets toNamespaces but not fromNamespace — both must be set together",
			failureMark(), crdName, item.GetKind(), item.GetName(), phase)
	}
	return nil
}
