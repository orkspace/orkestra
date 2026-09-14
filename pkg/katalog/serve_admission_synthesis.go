package katalog

import orktypes "github.com/orkspace/orkestra/pkg/types"

// applyServeAdmissionSynthesis synthesizes implicit admission rules from serve field declarations.
// Fields marked required: true get an exists validation rule; type: enum fields get an in rule.
// Fields with default: or override: get mutation rules via ServeFieldMutationRules.
// Runs last, after all explicit rules (inline, include, motif) have settled. Synthesized
// validation rules are prepended so a missing field surfaces as the headline denial reason.
func (k *Katalog) applyServeAdmissionSynthesis() {
	for name, entry := range k.enabledCRDs {
		synthesizedVal := append(entry.RequiredServeFieldRules(), entry.EnumServeFieldRules()...)
		synthesizedMut := entry.ServeFieldMutationRules()

		hasSynthVal := len(synthesizedVal) > 0
		hasSynthMut := len(synthesizedMut) > 0

		if !hasSynthVal && !hasSynthMut {
			continue
		}

		if hasSynthVal {
			if entry.Validation == nil {
				entry.Validation = &orktypes.ValidationConfig{}
			}
		}
		if hasSynthMut {
			if entry.Mutation == nil {
				entry.Mutation = &orktypes.MutationConfig{}
			}
		}
		entry.DeduplicateSynthesizedServeRules(orktypes.SynthDedup{
			HasValidation:   hasSynthVal,
			HasMutation:     hasSynthMut,
			ValidationRules: synthesizedVal,
			MutationRules:   synthesizedMut,
		})
		k.enabledCRDs[name] = entry
	}
}
