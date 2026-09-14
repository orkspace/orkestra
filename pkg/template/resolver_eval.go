package template

import orktypes "github.com/orkspace/orkestra/pkg/types"

// RenderString evaluates a single template expression against the resolver's
// current data map. Returns (result, true) on success; ("", false) on error.
// Used by the when: TemplateEvaluator so note functions are available in
// when: field expressions.
func (r *Resolver) RenderString(tmpl string) (string, bool) {
	result, err := r.Resolve(tmpl)
	if err != nil {
		return "", false
	}
	return result, true
}

// TemplateEvaluator returns a TemplateEvaluator bound to this resolver.
// Pass the returned func to types.EvaluateConditions at call sites where template
// expressions in when: fields should be evaluated against live CR data.
func (r *Resolver) TemplateEvaluator() orktypes.TemplateEvaluator {
	return func(tmpl string) (string, bool) {
		return r.RenderString(tmpl)
	}
}
