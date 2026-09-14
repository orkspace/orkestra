// pkg/template/resolver_children.go
package template

// WithChildren returns a new Resolver that includes child resource state
// in the template context under the "children" key.
//
// This is the Layer 3 extension. The resolver is rebuilt with the children
// map so that status field expressions can reference child resource status:
//
//	{{ .children.deployment.status.readyReplicas }}
//	{{ .children.service.status.loadBalancer.ingress }}
//	{{ (index .children.deployments "my-site-api").status.readyReplicas }}
//
// The original resolver's data map is copied — the original is not modified.
// The returned resolver is used only for status field resolution.
//
// children is the map returned by ReadChildren — a nested structure of
// resource type → name → full object map.
func (r *Resolver) WithChildren(children map[string]interface{}) *Resolver {
	if len(children) == 0 {
		return r
	}

	// Shallow copy the data map so the original resolver is unchanged.
	// The copy shares the same nested maps — children is added as a new
	// top-level key only.
	newData := r.shallowCopy()
	newData["children"] = children
	return r.copyWith(newData)
}
