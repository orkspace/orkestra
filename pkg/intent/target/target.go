package target

import (
	"fmt"
	"strings"

	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	// "github.com/orkspace/orkestra/pkg/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// IsTargetRequest reports whether raw is a target-mode request.
//
// Detection rule: presence of "target" key, regardless of whether
// "apiVersion" is also present. This lets callers migrate incrementally
// by adding "target" without immediately removing Kubernetes fields.
func IsTargetRequest(raw map[string]interface{}) bool {
	_, ok := raw["target"]
	return ok
}

// BuildCRFromTarget constructs a full Kubernetes CR from a flat field map
// submitted in target mode.
//
// Field routing — determined by where each field name is declared in the
// Katalog's serve config:
//
//	serve.fields                    → spec.<field>
//	serve.labels       				→ metadata.labels[<field>]
//	serve.annotations  				→ metadata.annotations[<field>]
//	not declared anywhere           → silently ignored
//
// The resolver receives the flat field map so serve.name and serve.namespace
// expressions reference submitted values directly:
//
//	serve.name:      '{{ repoSlug .repository }}'  → .repository from the request
//	serve.namespace: '{{ .team }}-{{ .environment }}' → .team and .environment
//
// When serve.name isn't declared, the caller's own "name" field is used as-is,
// matching full CR mode's metadata.name behavior for the same case.
//
// Returns an error only when serve.name or serve.namespace are declared but
// cannot be resolved to a non-empty string — SSA would reject the CR anyway,
// so we surface the error here with a clearer message.
func BuildCRFromTarget(
	raw map[string]interface{},
	crd *orktypes.CRDEntry,
	notes orktypes.NoteRegistry,
) (*unstructured.Unstructured, error) {
	// 1. Build the CR skeleton
	obj := newCRSkeleton(crd)

	// 2. Route fields to their destinations
	if err := routeFields(raw, crd, notes, obj); err != nil {
		return nil, err
	}

	// 3. Resolve serve.name and serve.namespace
	if err := resolveServeIdentity(raw, crd, notes, obj); err != nil {
		return nil, err
	}

	return obj, nil
}

// newCRSkeleton creates a blank CR with the correct apiVersion, kind,
// and empty metadata/spec structures.
func newCRSkeleton(crd *orktypes.CRDEntry) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": crd.APIVersion(),
			"kind":       crd.Kind(),
			"metadata": map[string]interface{}{
				"labels":      map[string]interface{}{},
				"annotations": map[string]interface{}{},
			},
			"spec": map[string]interface{}{},
		},
	}
}

// routeFields routes each submitted field to its declared destination:
//   - serve.fields → spec (supports nested dot-paths via 'path' field;
//     value/values expressions transform the submitted value before writing)
//   - serve.labels → metadata.labels
//   - serve.annotations → metadata.annotations
//   - unknown fields → silently ignored
//
// Fields declared in serve.fields with no path/value/values use the field name
// as the spec key (flat assignment). The full intent payload is available as
// .request in value/values expressions for cross-field reads.
func routeFields(
	raw map[string]interface{},
	crd *orktypes.CRDEntry,
	notes orktypes.NoteRegistry,
	obj *unstructured.Unstructured,
) error {
	meta := obj.Object["metadata"].(map[string]interface{})
	labels := meta["labels"].(map[string]interface{})
	annotations := meta["annotations"].(map[string]interface{})
	spec := obj.Object["spec"].(map[string]interface{})

	labelFields := crd.ServeLabels()
	annotationFields := crd.ServeAnnotations()

	// Base resolver for value/values expression evaluation.
	// WithRequest injects the full intent as .request.<field> for cross-field reads.
	// WithFieldValue is called per-field to inject .value for the current field.
	baseResolver := orktmpl.NewResolverFromMap(raw).WithUserNotes(notes).WithRequest(raw)

	var errs []string

	// Route each submitted value to its declared destination.
	for key, submitted := range raw {
		if key == "target" {
			continue
		}

		// ─── Spec fields ─────────────────────────────────────────────────
		if config, ok := crd.Serve.Fields[key]; ok {
			// Fanout: one submitted field → multiple CR spec paths.
			if config.HasValues() {
				r := baseResolver.WithFieldValue(submitted)
				for specPath, expr := range config.Values {
					result, err := r.Resolve(expr)
					if err != nil {
						errs = append(errs, fmt.Sprintf("field %q: values[%q]: %s", key, specPath, err.Error()))
						continue
					}
					if err := setSpecValue(spec, specPath, result); err != nil {
						errs = append(errs, fmt.Sprintf("field %q: values[%q]: %s", key, specPath, err.Error()))
					}
				}
				continue
			}

			// Single transform: evaluate expression, write result to spec path.
			if config.HasValue() {
				r := baseResolver.WithFieldValue(submitted)
				result, err := r.Resolve(config.Value)
				if err != nil {
					errs = append(errs, fmt.Sprintf("field %q: value: %s", key, err.Error()))
					continue
				}
				if err := setSpecValue(spec, config.SpecPath(key), result); err != nil {
					errs = append(errs, fmt.Sprintf("field %q: value: %s", key, err.Error()))
				}
				continue
			}

			// Plain field: write submitted value as-is to spec path.
			if err := setSpecValue(spec, config.SpecPath(key), submitted); err != nil {
				errs = append(errs, fmt.Sprintf("field %q: %s", key, err.Error()))
			}
			continue
		}

		// ─── Labels ──────────────────────────────────────────────────────
		if mapContains(labelFields, key) {
			labels[key] = fmt.Sprintf("%v", submitted)
			continue
		}

		// ─── Annotations ─────────────────────────────────────────────────
		if mapContains(annotationFields, key) {
			annotations[key] = fmt.Sprintf("%v", submitted)
			continue
		}

		// Unknown field — silently ignored.
	}

	if len(errs) > 0 {
		return fmt.Errorf("field translation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// setSpecValue writes a value to a dot-notation spec path, creating intermediate
// maps as needed. Flat paths (no dot) are assigned directly.
func setSpecValue(spec map[string]interface{}, path string, value interface{}) error {
	if isNestedPath(path) {
		return setNestedPath(spec, path, value)
	}
	spec[path] = value
	return nil
}

// resolveServeIdentity resolves serve.name and serve.namespace using the flat field map.
// Notes are available so expressions like `{{ repoSlug .repository }}` work.
func resolveServeIdentity(
	raw map[string]interface{},
	crd *orktypes.CRDEntry,
	notes orktypes.NoteRegistry,
	obj *unstructured.Unstructured,
) error {
	// Build a resolver data map that exposes both:
	//   .repository  (flat, from the request — target mode)
	//   .spec.repository (nested — for expressions written the Kubernetes way)
	data := make(map[string]interface{}, len(raw)+1)
	for k, v := range raw {
		data[k] = v
	}
	// Merge the built CR so .spec.*, .metadata.* work too
	for k, v := range obj.Object {
		if _, exists := data[k]; !exists {
			data[k] = v
		}
	}

	resolver := orktmpl.NewResolverFromMap(data).WithUserNotes(notes)

	// Resolve serve.name and serve.namespace. Every field the expression
	// references must be present — ResolveStrict reports a missing field
	// even when the composite result is non-empty (e.g. "{{ .team }}-{{ .environment }}"
	// with both fields absent still renders "-", which a plain empty-string
	// check wouldn't catch).
	if crd.HasServeName() {
		name, missing, err := resolver.ResolveStrict(crd.Serve.Name)
		if err != nil || missing || strings.TrimSpace(name) == "" {
			return fmt.Errorf(
				"serve.name expression %q could not be resolved — "+
					"check that the required fields are present in the request: %w",
				crd.Serve.Name, err,
			)
		}
		// Ensure serve.name is a valid kubernetes name
		name = strings.TrimSpace(name)
		if err := validateK8sName(name); err != nil {
			return fmt.Errorf(
				"serve.name %q is invalid: %w",
				name, err,
			)
		}
		obj.SetName(strings.TrimSpace(name))
	} else if name, ok := raw["name"].(string); ok && strings.TrimSpace(name) != "" {
		// serve.name not declared — use the caller-supplied name, matching
		// full CR mode's behavior for the same case.
		name = strings.TrimSpace(name)
		if err := validateK8sName(name); err != nil {
			return fmt.Errorf(
				"name %q is invalid: %w",
				name, err,
			)
		}
		obj.SetName(name)
	}

	if crd.HasServeNamespace() {
		ns, missing, err := resolver.ResolveStrict(crd.Serve.Namespace)
		if err != nil || missing || strings.TrimSpace(ns) == "" {
			return fmt.Errorf(
				"serve.namespace expression %q could not be resolved — "+
					"check that the required fields are present in the request: %w",
				crd.Serve.Namespace, err,
			)
		}
		// Ensure serve.namespace is a valid kubernetes namespace
		ns = strings.TrimSpace(ns)
		if err := validateK8sName(ns); err != nil {
			return fmt.Errorf(
				"serve.namespace %q is invalid: %w",
				ns, err,
			)
		}
		obj.SetNamespace(ns)
	}

	return nil
}
