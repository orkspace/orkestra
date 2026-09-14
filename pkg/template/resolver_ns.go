// pkg/template/resolver_ns.go
//
// Namespace resolution helper.
//
// Every run_*.go function needs the resolved namespace before calling
// ResolveXTemplate — for two reasons:
//   1. The namespace guard check (restricted/allowed)
//   2. DeleteIfOwned when conditions fail (needs name + namespace)
//
// ResolveXTemplate already handles namespace internally, so calling this
// helper first means namespace is resolved twice per resource. This is
// intentional and cheap — Resolve() is a text/template execution against
// a small map, under 1μs for a simple "{{ .metadata.namespace }}" expression.
// The alternative (changing all ResolveXTemplate signatures) creates more
// complexity than the double-resolution costs.
//
// Usage in run_*.go:
//
//   name := resolver.ResolveName(src.Name)
//   ns   := resolver.ResolveNamespace(src.Namespace, owner.GetNamespace())
//
//   if guard != nil && !guard(ctx, owner, ns) { continue }
//   if !conditionPassed { DeleteIfOwned(ctx, kube, owner, name, ns); continue }
//
//   resolved, err := resolver.ResolveXTemplate(src) // resolves ns again — fine

package template

// ResolveNamespace resolves a namespace template expression and falls back
// to ownerNamespace when the result is empty.
//
// This is called before ResolveXTemplate to get the actual target namespace
// for guard checks and conditional cleanup. ResolveXTemplate will resolve
// namespace again — the double resolution is intentional and cheap.
func (r *Resolver) ResolveNamespace(tmpl, ownerNamespace string) string {
	if tmpl == "" {
		return ownerNamespace
	}
	ns, err := r.Resolve(tmpl)
	if err != nil || ns == "" {
		return ownerNamespace
	}
	return ns
}

// ResolveName resolves a name template expression.
// Returns empty string on error — callers use it for DeleteIfOwned and
// guard checks where an empty name is handled gracefully downstream.
func (r *Resolver) ResolveName(tmpl string) string {
	if tmpl == "" {
		return ""
	}
	name, _ := r.Resolve(tmpl)
	return name
}
