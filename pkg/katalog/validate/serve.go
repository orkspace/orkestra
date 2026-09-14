package validate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ValidateServe runs all Serve-related validations.
// This is the single entry point for Serve validation, keeping the main
// pipeline clean and grouping all Serve checks together.
func (e *executor) ValidateServe() error {
	// 1. Validate serve.modes: at least one mode enabled
	if err := e.validateServeModes(); err != nil {
		return err
	}

	// 2. Validate serve.labels and serve.annotations (key syntax, enum, uniqueness)
	if err := e.validateServeLabelAndAnnotationFields(); err != nil {
		return err
	}

	// 3. Validate serve.fields path configurations (uniqueness, format, nested)
	if err := e.validateServeFieldPaths(); err != nil {
		return err
	}

	// 4. Validate serve field order: values don't collide
	if err := e.validateServeFieldOrder(); err != nil {
		return err
	}

	// 5. Validate serve.namespace — required on namespaced+serve-enabled CRDs,
	//    rejected on cluster-scoped ones, incompatible with a pinned watch
	//    scope when templated
	if err := e.validateServeNamespace(); err != nil {
		return err
	}

	// 6. Validate Serve response config — payload template compilation and
	//    payload/exclude path conflicts (warnings, not errors)
	if err := e.validateServeResponseConfig(); err != nil {
		return err
	}

	// 7. Validate Serve tokens and namespace restrictions per CRD
	if err := e.validateServeTokenRestrictions(); err != nil {
		return err
	}

	// 8. Validate Serve targets per CRD; uniqueness across the katalog
	if err := e.validateServeTarget(); err != nil {
		return err
	}

	// 9. Validate Serve response config (depends on CRD)
	if err := e.validateServeResponseConfig(); err != nil {
		return err
	}

	// 10. Validate serve.aliases — name format, routing uniqueness, token references
	if err := e.validateServeAliases(); err != nil {
		return err
	}

	// 11. Validate serve.fields value/values — mutual exclusion, dot-notation keys,
	//     template compilation
	if err := e.validateServeFieldTranslation(); err != nil {
		return err
	}

	// 12. Validate serve target field selectors
	if err := e.validateServeFieldSelector(); err != nil {
		return err
	}

	return nil
}

// validateServeLabelAndAnnotationFields checks serve.labels/annotations
// keys are syntactically valid Kubernetes label/annotation keys, that
// type: enum fields declare a non-empty enum, and that no key collides with
// serve.fields or between the labels/annotations buckets themselves.
func (e *executor) validateServeLabelAndAnnotationFields() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		if !crd.HasServeLabelsOrAnnotations() {
			continue
		}
		labels := crd.ServeLabels()
		annotations := crd.ServeAnnotations()

		seen := make(map[string]string, len(crd.Serve.Fields)+len(labels)+len(annotations))
		for name := range crd.Serve.Fields {
			seen[name] = "fields"
		}

		if err := validateServeLabelAndAnnotationBucket(crdName, "labels", labels, seen); err != nil {
			return err
		}
		if err := validateServeLabelAndAnnotationBucket(crdName, "annotations", annotations, seen); err != nil {
			return err
		}
	}
	return nil
}

// validateServeFieldOrder rejects two serve.fields / serve.labels and serve.annotations
// entries on the same CRD sharing an explicit (non-zero) order: value.
// order: now decides synthesized validation-rule priority — see
// CRDEntry.DuplicateServeFieldOrders — not just form layout, so a collision
// is no longer purely cosmetic.
func (e *executor) validateServeFieldOrder() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		dups := crd.DuplicateServeFieldOrders()
		if len(dups) == 0 {
			continue
		}
		orders := make([]int, 0, len(dups))
		for order := range dups {
			orders = append(orders, order)
		}
		sort.Ints(orders)
		return errServeOrderCollision(crdName, orders[0], dups[orders[0]])
	}
	return nil
}

func errServeOrderCollision(crd string, order int, names []string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s serve field order collision: order: %s shared by %s
   CRD: %s

Each serve.fields / serve.labels and serve.annotations entry needs a distinct order: value
(0/unset doesn't count — any number of fields may leave it unset).
Order also decides which field's violation is reported when more than one fails at
once, not just where it renders on the form.
──────────────────────────────────────────────`, failureMark(), strconv.Itoa(order), strings.Join(names, ", "), crd)
}

// validateServeNamespace enforces serve.namespace's three structural rules:
//
//   - A namespaced CRD (the default) with serve.enabled: true must declare it
//     — otherwise the Control Center form and any Gateway API client have no
//     way to know which namespace a new CR belongs in, and there's no other
//     signal available (see ServeConfig.Namespace).
//   - A cluster-scoped CRD (namespaced: false) must NOT declare it — there
//     is no namespace to resolve into, so setting one is always a mistake.
//   - A templated (non-literal) serve.namespace is incompatible with a CRD
//     whose informer is pinned to watch one fixed namespace
//     (CRDEntry.PinnedToNamespace) — a CR resolved into any other namespace
//     would be created but never reconciled, silently, forever.
func (e *executor) validateServeNamespace() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		if !crd.ServeEnabled() {
			continue
		}
		hasNamespace := crd.HasServeNamespace()

		if crd.IsNamespaced() && !hasNamespace {
			return errServeNamespaceMissing(crdName)
		}
		if !crd.IsNamespaced() && hasNamespace {
			return errServeNamespaceOnClusterScoped(crdName, crd.Serve.Namespace)
		}
		if hasNamespace && orktypes.IsTemplate(crd.Serve.Namespace) && crd.PinnedToNamespace() {
			pinned := crd.SingleNamespace()
			if pinned == "" {
				pinned = crd.Namespace
			}
			return errServeNamespacePinnedConflict(crdName, crd.Serve.Namespace, pinned)
		}
	}
	return nil
}

func errServeNamespaceMissing(crd string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s serve.enabled: true with no serve.namespace
   CRD: %s

This CRD is namespaced (the default — set namespaced: false to opt out).
serve.namespace tells the Gateway API which namespace a new CR belongs in when
the caller (Control Center, curl, CI) doesn't say — without it, self-service
creation has no way to know where to place the CR.

Declare a literal namespace or a template expression, e.g.:
  serve:
    namespace: '{{ teamName }}'
──────────────────────────────────────────────`, failureMark(), crd)
}

func errServeNamespaceOnClusterScoped(crd, ns string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s serve.namespace set on a cluster-scoped CRD: %q
   CRD: %s

namespaced: false means this CRD has no namespace concept — serve.namespace
has nothing to resolve into. Remove it, or drop namespaced: false if this
CRD should actually be namespaced.
──────────────────────────────────────────────`, failureMark(), ns, crd)
}

func errServeNamespacePinnedConflict(crd, tmpl, pinned string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s serve.namespace is templated but the informer is pinned to one namespace
   CRD: %s
   serve.namespace: %s
   Pinned to: %q

A templated serve.namespace can resolve to a different namespace per
submission, but this CRD's informer only watches %q — anything created
outside it would exist in the cluster but never be reconciled, silently.
Either make serve.namespace a literal matching the pinned namespace, or widen
the CRD's namespace scope (allowedNamespaces with more than one entry, or
drop the legacy namespace: field) so the informer can see everywhere
serve.namespace might resolve to.
──────────────────────────────────────────────`, failureMark(), crd, tmpl, pinned, pinned)
}

func validateServeLabelAndAnnotationBucket(crdName, bucket string, fields map[string]orktypes.ServeFieldConfig, seen map[string]string) error {
	for key, cfg := range fields {
		// Validate that the key is a valid Kubernetes label/annotation key
		if errs := isValidLabelKey(key); len(errs) > 0 {
			return errInvalidServeKey(crdName, bucket, key, errs[0])
		}

		// Check for key collisions across serve.fields and other buckets
		if owner, ok := seen[key]; ok {
			return errServeKeyCollision(crdName, key, owner, bucket)
		}
		seen[key] = bucket

		// Validate that the type is one of the allowed types.
		if !orktypes.IsValidServeFieldType(cfg.Type) {
			return errInvalidServeType(crdName, bucket, key, cfg.Type)
		}

		// Special validation for enum type: must have non-empty enum values
		if cfg.Type == "enum" && len(cfg.Enum) == 0 {
			return errServeEnumEmpty(crdName, bucket, key)
		}
	}
	return nil
}

func errInvalidServeKey(crd, bucket, key, reason string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s Invalid serve.%s key: %q
   CRD: %s

%s

Kubernetes label/annotation keys are [prefix/]name — name must be an
alphanumeric string (max 63 chars, may contain '-', '_', '.') and, if
present, prefix must be a valid DNS subdomain (max 253 chars).
──────────────────────────────────────────────`, failureMark(), bucket, key, crd, reason)
}

func errServeKeyCollision(crd, key, firstBucket, secondBucket string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s serve field name collision: %q
   CRD: %s
   Declared in both: serve.%s and serve.%s

A field name must be unique across serve.fields,
serve.labels and serve.annotations bucket.
──────────────────────────────────────────────`, failureMark(), key, crd, firstBucket, secondBucket)
}

func errServeEnumEmpty(crd, bucket, key string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s serve.%s %q has type: enum but no enum: values
   CRD: %s

type: enum requires a non-empty enum: list.
──────────────────────────────────────────────`, failureMark(), bucket, key, crd)
}

// errInvalidServeType returns an error for invalid field types.
// This ensures that users only use the allowed FieldType values.
func errInvalidServeType(crd, bucket, key, invalidType string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s Invalid serve.%s %q type: %q
   CRD: %s

Valid types are: string, integer, number, boolean, enum
──────────────────────────────────────────────`, failureMark(), bucket, key, invalidType, crd)
}
