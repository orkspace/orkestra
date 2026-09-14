package children

import (
	"reflect"

	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ExpandForEachCustomResources expands any CustomResource entries that declare a ForEach.
// It returns a new slice with expanded items. Items without ForEach are copied verbatim.
func ExpandForEachCustomResources(resolver *orktmpl.Resolver, srcs []orktypes.CustomResourceTemplateSource) []orktypes.CustomResourceTemplateSource {
	// Fast path: nothing to do
	if !anyHasForEach(len(srcs), func(i int) *orktypes.ForEachSpec { return srcs[i].ForEach }) {
		return srcs
	}

	var result []orktypes.CustomResourceTemplateSource
	for _, src := range srcs {
		// If no ForEach on this item, keep as-is
		if src.ForEach == nil {
			result = append(result, src)
			continue
		}

		// Resolve the items for this ForEach (returns []any or []map[string]any depending on implementation)
		items := resolveForEachItems(resolver.Data(), src.ForEach.Field)
		for i, item := range items {
			ir := itemResolver(resolver, item, src.ForEach.As, i)

			// Start with a shallow copy of src and clear ForEach on the expanded copy
			expanded := src
			expanded.ForEach = nil

			// Resolve simple string fields
			if v, _ := ir.Resolve(src.APIVersion); v != "" {
				expanded.APIVersion = v
			}
			if v, _ := ir.Resolve(src.Kind); v != "" {
				expanded.Kind = v
			}

			// Metadata — resolve all fields into meta before assigning
			meta := src.Metadata // copy
			if v, _ := ir.Resolve(meta.Name); v != "" {
				meta.Name = v
			}
			if v, _ := ir.Resolve(meta.Namespace); v != "" {
				meta.Namespace = v
			}

			// Resolve Labels into meta directly so the assignment below picks them up
			meta.Labels = resolveMap(ir, meta.Labels)

			// Resolve Annotations into meta directly
			meta.Annotations = resolveMap(ir, meta.Annotations)

			expanded.Metadata = meta

			// Resolve spec/status/other maps by walking and resolving string values
			if src.Spec != nil {
				expanded.Spec = resolveMapTemplates(ir, src.Spec)
			}
			if src.Status != nil {
				expanded.Status = resolveMapTemplates(ir, src.Status)
			}
			if src.Other != nil {
				expanded.Other = resolveMapTemplates(ir, src.Other)
			}

			// Reconcile is a bool; if the user templated it as a string, they should have used spec fields.
			// Sleep may be templated string
			if v, _ := ir.Resolve(src.Sleep); v != "" {
				expanded.Sleep = v
			}

			// HasStatus pointer: keep original pointer, but if nil and user provided a templated
			// string in some other field to indicate status, leave as-is. (No automatic conversion.)
			expanded.HasStatus = src.HasStatus

			// Append expanded item
			result = append(result, expanded)
		}
	}

	return result
}

// resolveMapTemplates walks a map[string]any and returns a new map with any string
// values passed through the item resolver. It recurses into nested maps and slices.
func resolveMapTemplates(ir *orktmpl.Resolver, in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = resolveValueTemplates(ir, v)
	}
	return out
}

// resolveValueTemplates resolves a single value. If it's a string, run through resolver.
// If it's a map[string]any, recurse. If it's a slice, resolve each element. Otherwise return as-is.
func resolveValueTemplates(ir *orktmpl.Resolver, v any) any {
	switch vv := v.(type) {
	case string:
		rv, _ := ir.Resolve(vv)
		if rv == "" {
			return vv
		}
		// Same coercion as the non-forEach custom resource path
		// (resolve_customresources.go) — without it, a forEach-expanded
		// custom resource's numeric/boolean/JSON fields would be submitted
		// as literal strings instead of native types.
		if orktypes.IsTemplate(vv) {
			return orktypes.TryCoerceString(rv)
		}
		return rv
	case map[string]any:
		return resolveMapTemplates(ir, vv)
	case []any:
		out := make([]any, 0, len(vv))
		for _, e := range vv {
			out = append(out, resolveValueTemplates(ir, e))
		}
		return out
	default:
		// preserve numbers, bools, structs, etc.
		// If it's a typed map (map[string]string) convert to map[string]any and recurse
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Map {
			// attempt to convert map[string]string -> map[string]any
			if rv.Type().Key().Kind() == reflect.String && rv.Type().Elem().Kind() == reflect.String {
				out := make(map[string]any, rv.Len())
				for _, key := range rv.MapKeys() {
					k := key.String()
					val := rv.MapIndex(key).String()
					if resolved, _ := ir.Resolve(val); resolved != "" {
						out[k] = resolved
					} else {
						out[k] = val
					}
				}
				return out
			}
		}
		return v
	}
}

// copyStringMap returns a shallow copy of a map[string]string
func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
