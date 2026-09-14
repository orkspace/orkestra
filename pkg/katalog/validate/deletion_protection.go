package validate

// validateDeletionProtection checks per‑CRD deletion protection overrides
// for logical consistency and prints user‑friendly warnings.
//
// Warns when:
//   - protectCRD = false but protectCRs = true for a custom resource (non‑built‑in).
//     This combination is not useful because if the CRD type definition is not protected,
//     it can be deleted, which would remove all instances anyway – making instance
//     protection meaningless.
//
// The check is skipped for built‑in resource types (e.g., ConfigMap, Deployment, Namespace)
// because they are not CRDs and do not have a `customresourcedefinitions` object to protect.
//
// These warnings are non‑fatal – the Katalog remains valid, but the runtime behaviour
// will ignore protectCRs in such cases (effectively treating it as false).
func (e *executor) validateDeletionProtection() error {
	for name, crd := range e.k.EnabledCRDs() {
		if !crd.HasDeletionProtectionOverride() {
			continue
		}
		// Skip built‑ins – they have no CRD to protect
		if crd.IsBuiltInType() {
			continue
		}

		protectCRD := crd.ShouldProtectCRD()
		protectCRs := crd.ShouldProtectCRs()

		// protectCRD=false + protectCRs=true is not a useful combination
		if !protectCRD && protectCRs {
			warning := "protectCRs=true is ineffective once the CRD is deleted.\n" +
				"Consider setting protectCRD=true if you intend to protect instances permanently."

			crd.Warnings.AddWarning(warning)
			e.k.EnabledCRDs()[name] = crd

		}
	}
	return nil
}
