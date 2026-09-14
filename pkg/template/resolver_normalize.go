// pkg/template/resolver_normalize.go
package template

import orktypes "github.com/orkspace/orkestra/pkg/types"

// WithNormalizeChanges returns a new Resolver that includes normalize audit
// data under the "_normalizeChanges" key. Status field templates can reference
// this when normalize.audit: true is declared:
//
//   - path: normalizeChanges
//     value: "{{ toJson ._normalizeChanges }}"
//
// When changes is empty, the original resolver is returned unchanged.
func (r *Resolver) WithNormalizeChanges(changes []orktypes.NormalizeChange) *Resolver {
	if len(changes) == 0 {
		return r
	}

	raw := make([]interface{}, len(changes))
	for i, c := range changes {
		raw[i] = map[string]interface{}{
			"field": c.Field,
			"from":  c.From,
			"to":    c.To,
		}
	}

	newData := r.shallowCopy()
	newData["_normalizeChanges"] = raw
	return r.copyWith(newData)
}
