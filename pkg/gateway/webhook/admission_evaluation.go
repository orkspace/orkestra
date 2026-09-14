// webhook/admission_evaluation.go — validation and mutation rule evaluation.
package webhook

import (
	"context"
	"fmt"

	orkexternal "github.com/orkspace/orkestra/pkg/external"
	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils/common/query"
)

// ── Validation evaluation ─────────────────────────────────────────────────────

type validationViolation struct {
	Field    string
	Message  string
	Got      string
	RuleType string
	Action   orktypes.ValidationAction
}

func (ws *WebhookServer) evaluateValidationRules(
	ctx context.Context,
	obj map[string]interface{},
	cfg *orktypes.ValidationConfig,
	kindName string,
) (denials []validationViolation, warnings []validationViolation) {
	if cfg == nil || len(cfg.Rules) == 0 {
		return nil, nil
	}

	resolver := orktmpl.NewResolverFromMap(obj)
	kat := ws.katalog
	var notes orktypes.NoteRegistry
	if kat != nil {
		notes = kat.UserNotes()
		resolver = resolver.WithUserNotes(notes)
	}
	// Inject the raw intent payload as .request so validation rules can gate on
	// intent-vocabulary fields (e.g. request.schedule) before field translation.
	// Only present when the CR was submitted through the Gateway API in target mode.
	if intent := orktarget.ResolveIntentFromObject(obj); intent != nil {
		resolver = resolver.WithRequest(intent)
	}
	if calls := cfg.AdmissionExternal(); len(calls) > 0 {
		var err error
		resolver, err = orkexternal.Run(ctx, kindName, resolver, calls, ws.kubeClient)
		if err != nil {
			logger.FromContext(ctx).Warn().Err(err).Str("kind", kindName).Msg("admission/validate: external call failed")
		}
	}
	// Runtime data — fetched via HTTP from the running operator.
	// Each call is gated on whether any rule actually references it,
	// so CRDs with no unique/health/metrics rules pay zero HTTP cost.
	// Note bodies are scanned so a rule like {{ inBusinessHours }} correctly
	// triggers a fetch when inBusinessHours references .health.* or .metrics.*.
	if kat != nil && ws.konfig != nil && (cfg.HasUniqueRule() || cfg.HasHealthField(notes) || cfg.HasMetricsField(notes)) {
		result := ws.katalog.LookupByKind(kindName)
		if result.Entry() != nil {
			crdName := result.Entry().Name
			q := query.NewRuntimeQuery(ctx, ws.runtimeEndpoint(), crdName)
			if cfg.HasUniqueRule() {
				resolver = resolver.WithUniquenessChecker(q)
			}
			if cfg.HasHealthField(notes) {
				resolver = resolver.WithHealth(q.ForHealth())
			}
			if cfg.HasMetricsField(notes) {
				resolver = resolver.WithMetrics(q.ForMetrics())
			}
		}
	}
	data := resolver.Data()
	for _, rule := range cfg.Rules {
		if !orktypes.EvaluateConditions(data, rule.When, rule.Or, resolver.TemplateEvaluator()) {
			continue
		}
		rv := orktypes.EvaluateValidationRule(data, resolver, rule)
		if rv == nil {
			continue
		}
		v := &validationViolation{
			Field:    rv.Field,
			Message:  rv.Message,
			Got:      rv.Value,
			RuleType: rv.Rule,
			Action:   rule.Action,
		}
		switch orktypes.EffectiveAction(rule.Action) {
		case orktypes.ValidationActionDeny:
			denials = append(denials, *v)
		case orktypes.ValidationActionWarn:
			warnings = append(warnings, *v)
		}
	}
	return
}

// ── Mutation evaluation ───────────────────────────────────────────────────────

type fieldChange struct {
	Field      string
	OldValue   string
	NewValue   string      // for logging only
	TypedValue interface{} // for JSON patch (preserves type)
	ChangeType string
}

func (ws *WebhookServer) applyMutationRules(
	ctx context.Context,
	obj map[string]interface{},
	cfg *orktypes.MutationConfig,
	kindName string,
) ([]fieldChange, error) {
	if cfg == nil || len(cfg.Rules) == 0 {
		return nil, nil
	}

	resolver := orktmpl.NewResolverFromMap(obj)
	kat := ws.katalog
	var notes orktypes.NoteRegistry
	if kat != nil {
		notes = kat.UserNotes()
		resolver = resolver.WithUserNotes(notes)
	}

	// Inject the raw intent payload as .request so mutation rules can default/override on
	// intent-vocabulary fields (e.g. request.schedule) before field translation.
	// Only present when the CR was submitted through the Gateway API in target mode.
	if intent := orktarget.ResolveIntentFromObject(obj); intent != nil {
		resolver = resolver.WithRequest(intent)
	}
	if calls := cfg.AdmissionExternal(); len(calls) > 0 {
		var err error
		resolver, err = orkexternal.Run(ctx, kindName, resolver, calls, ws.kubeClient)
		if err != nil {
			logger.FromContext(ctx).Warn().Err(err).Str("kind", kindName).Msg("admission/mutate: external call failed")
		}
	}
	if kat != nil && ws.konfig != nil && (cfg.HasUniqueRule() || cfg.HasHealthField(notes) || cfg.HasMetricsField(notes)) {
		result := kat.LookupByKind(kindName)
		if result.Entry() != nil {
			crdName := result.Entry().Name
			q := query.NewRuntimeQuery(ctx, ws.runtimeEndpoint(), crdName)
			if cfg.HasUniqueRule() {
				resolver = resolver.WithUniquenessChecker(q)
			}
			if cfg.HasHealthField(notes) {
				resolver = resolver.WithHealth(q.ForHealth())
			}
			if cfg.HasMetricsField(notes) {
				resolver = resolver.WithMetrics(q.ForMetrics())
			}
		}
	}
	var changes []fieldChange

	mdata := resolver.Data()
	for _, rule := range cfg.Rules {
		if !orktypes.EvaluateConditions(mdata, rule.When, rule.Or, resolver.TemplateEvaluator()) {
			continue
		}

		// Resolve template expression in the field path.
		targetField := rule.Field
		if orktypes.IsTemplate(targetField) {
			if resolved, err := resolver.Resolve(targetField); err == nil {
				targetField = resolved
			}
		}

		// Resolve raw value (string from template or static value) first
		var rawResolved string
		var changeType string
		var err error

		switch {
		case rule.IsOverrideChangeType():
			raw, err := resolver.Resolve(scalarToString(rule.Override))
			if err != nil {
				return nil, fmt.Errorf("mutation rule override for field %q: %w", targetField, err)
			}
			rawResolved = raw
			changeType = orktypes.OverrideMutationChangeType.String()

		case rule.IsDefaultChangeType():
			currentVal, found := resolveScalar(obj, targetField)
			if found && currentVal != "" {
				continue // already set, skip default
			}
			raw, err := resolver.Resolve(scalarToString(rule.Default))
			if err != nil {
				return nil, fmt.Errorf("mutation rule default for field %q: %w", targetField, err)
			}
			rawResolved = raw
			changeType = orktypes.DefaultMutationChangeType.String()

		default:
			continue
		}

		// Convert to the target type based on valueType
		typedVal, err := convertToType(rawResolved, rule.ValueType)
		if err != nil {
			logger.Error().Err(err).Str("field", rule.Field).Str("valueType", rule.ValueType).Msg("admission/mutate: type conversion failed")
			continue // skip this field instead of failing the whole admission
		}

		// Compare with current value (as string for simplicity)
		currentVal, _ := resolveScalar(obj, targetField)
		if fmt.Sprintf("%v", typedVal) == currentVal {
			continue // unchanged
		}

		// Apply the typed value to the object
		setFieldPath(obj, targetField, typedVal)

		changes = append(changes, fieldChange{
			Field:      targetField,
			OldValue:   currentVal,
			NewValue:   fmt.Sprintf("%v", typedVal),
			TypedValue: typedVal,
			ChangeType: changeType,
		})

		logger.Debug().
			Str("kind", kindName).
			Str("field", targetField).
			Str("was", currentVal).
			Str("now", fmt.Sprintf("%v", typedVal)).
			Str("type", changeType).
			Msg("admission/mutate: rule applied")
	}

	return changes, nil
}
