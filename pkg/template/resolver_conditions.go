package template

import (
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// evaluateConditions evaluates a when: block against the resolver's data map.
// All conditions AND together. Returns false on first failure.
// Called by ResolveStatusFields (same package).
// The resolver is passed so template expressions in when: fields are evaluated
// through the full note FuncMap (e.g. replicasReady, hasCrashingPod).
func evaluateConditions(r *Resolver, conditions []orktypes.Condition) bool {
	for _, cond := range conditions {
		if !orktypes.EvaluateOneCond(r.data, cond, r.TemplateEvaluator()) {
			return false
		}
	}
	return true
}
