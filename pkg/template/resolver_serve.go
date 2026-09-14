// pkg/template/resolver_serve.go
//
// Resolver extensions for the serve layer — intent payload and field
// translation context injection.
package template

// WithRequest returns a new Resolver with the raw intent payload injected under
// the "request" key. Used in two places in the serve layer:
//
//  1. Field translation — before evaluating value/values expressions in
//     BuildCRFromTarget. Expressions reference the submitted payload via:
//
//     .value           — the current field's raw submitted value (injected
//     per-field via WithFieldValue, not by this method)
//     .request.<field> — any field from the raw intent (cross-field reads)
//
//  2. Admission evaluation — before running validation.rules in the webhook.
//     Enables intent-level gates that fire on what the caller submitted,
//     before any field transformation has occurred:
//
//     expr: cronValid .request.schedule
//     expr: 'hasPrefix .request.image "ghcr.io/myorg/"'
func (r *Resolver) WithRequest(request map[string]interface{}) *Resolver {
	if len(request) == 0 {
		return r
	}
	newData := r.shallowCopy()
	newData["request"] = request
	return r.copyWith(newData)
}

// WithFieldValue returns a new Resolver with the current field's raw submitted
// value injected as ".value". Used per-field in routeFields when evaluating
// value and values expressions — the expression author writes {{ .value }} to
// refer to whatever the caller submitted for that specific field.
func (r *Resolver) WithFieldValue(v interface{}) *Resolver {
	newData := r.shallowCopy()
	newData["value"] = v
	return r.copyWith(newData)
}
