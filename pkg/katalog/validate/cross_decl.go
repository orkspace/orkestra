package validate

import (
	"fmt"
	"strings"
)

// validateCrossDecl validates all cross: declarations in a katalog.
// Rules:
//   - No self referencing
//   - crd: and labels: blocks cannot be both declared in the same cross declaration
//   - Every crd without source block (same katalog) must exist in the katalog
//   - All "as:" fields must be unique across the entire katalog (not just per-CRD)
//   - When label is used, 'as' is required for cross reference. Nothing to default to
//   - Only allow known ONCOP protocols
//   - Only one of the optional token and SecretRef is allowed
//   - If cross.selector is used, only a pair of either name/namespace or matchLabels is used.
//   - If namespace is defined without name in selector, an error.
func (e *executor) validateCrossDecl() error {
	// seen tracks all alias (as:) values across the entire katalog — uniqueness
	// is a katalog-wide constraint, not per-CRD.
	seen := make(map[string]bool)

	for name, crd := range e.k.EnabledCRDs() {
		if !crd.HasCrossDecl() {
			continue
		}

		decl := crd.OperatorBox.Cross

		for i, cross := range decl {
			crsCRD := cross.CRD
			sel := cross.LabelSelector
			alias := cross.As

			// require either crd or labels
			if crsCRD == "" && sel == nil {
				return fmt.Errorf("%s CRD %q: cross[%d]: crd or labels required.", failureMark(), name, i)
			}

			// enforce only one mode is used
			if cross.HasCRDAndLabelDecl() {
				return fmt.Errorf("%s CRD %q: cross[%d]: only one of 'crd:' or 'labels:' is required.", failureMark(), name, i)
			}

			// self-reference
			if crsCRD == name {
				return fmt.Errorf("%s CRD %q: cross: a CRD cannot reference itself: cross[%d].crd: %s",
					failureMark(), name, i, crsCRD)
			}

			// crd not found in katalog
			if cross.IsCRDBased() {
				if result := e.k.LookupByName(crsCRD); result.Entry() == nil {
					return fmt.Errorf("%s CRD %q: cross[%d].crd: %q not found", failureMark(), name, i, crsCRD)
				}
			}

			// label-based: as is required, lookup must resolve
			if cross.IsLabelBased() {
				if alias == "" {
					return fmt.Errorf("%s CRD %q: cross[%d].labels: 'as' is required for cross referencing", failureMark(), name, i)
				}
				if result := e.k.LookupByLabel(sel); result.Entry() == nil {
					return fmt.Errorf("%s CRD %q: cross[%d].labels: %s not found in any CRD in the katalog", failureMark(), name, i, sel.String())
				}
			}

			// alias
			if alias != "" {
				if err := isValidK8sName(alias); err != nil {
					return fmt.Errorf("%s CRD %q: invalid 'as' name: cross[%d].as: %s",
						failureMark(), name, i, alias)
				}
				// uniqueness is katalog-wide
				if seen[alias] {
					return fmt.Errorf("%s CRD %q: cross: alias (as) %q is already used in another CRD — as: must be unique across the katalog",
						failureMark(), name, alias)
				}
				seen[alias] = true
			}

			// cross.source
			if cross.HasSource() {
				cs := cross.Source
				pr := cs.Protocol
				if pr != "" {
					if !cross.IsValid(pr.String()) {
						return fmt.Errorf("%s CRD %q: invalid ONCOP protocol: cross[%d].source.%s. Choose either of these: %s",
							failureMark(), name, i, pr, strings.Join(cross.ValidONCOProtocols(), ", "))
					}
				}

				if cs.HasAuth() {
					if cs.HasTokenAndSecretRef() {
						return fmt.Errorf("%s CRD %q: declared both token and secretRef. Only one is allowed: cross[%d].source",
							failureMark(), name, i)
					}

					if cs.HasSecretRef() {
						ref := cs.Auth.SecretRef
						if ref.Name == "" {
							return fmt.Errorf("%s CRD %q: name is required for secretRef: cross[%d].source.auth.secretRef",
								failureMark(), name, i)
						}
						if ref.Key == "" {
							return fmt.Errorf("%s CRD %q: key is required for secretRef: cross[%d].source.auth.secretRef",
								failureMark(), name, i)
						}
						if ref.Namespace == "" {
							crd.Warnings.AddWarning(fmt.Sprintf("%s CRD %q: namespace is empty for secretRef: cross[%d].source.auth.secretRef. Defaults to orkestra namespace.",
								failureMark(), name, i))
						}
					}
				}
			}
		}
		e.k.EnabledCRDs()[name] = crd
	}
	return nil
}
