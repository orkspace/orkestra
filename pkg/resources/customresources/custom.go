// pkg/resources/customresources/customresources.go
package customresources

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	orklabels "github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ResolvedCustomResourceSpec is the fully resolved Custom Resource specification.
type ResolvedCustomResourceSpec struct {
	// APIVersion is required and must be a group/version string (e.g. "foo.io/v1").
	// This field is used to derive the GroupVersionKind for REST mapping.
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`

	// Kind is required and must be a valid Kubernetes Kind (e.g. "Bar").
	// Used together with APIVersion to resolve the GVR for dynamic client calls.
	Kind string `json:"kind" yaml:"kind"`

	// Metadata mirrors the subset of metav1.ObjectMeta Orkestra needs.
	// Implementations must ensure metadata.Name is present after templating.
	// Namespace is required for namespaced CRDs; for cluster-scoped CRDs the
	// namespace field should be empty. Whether a CRD is namespaced is determined
	// by discovery/validation and not by this struct alone.
	Metadata orktypes.CustomResourceMetadata `json:"metadata" yaml:"metadata"`

	// Spec is the conventional spec block for CRDs. It is schema-agnostic and
	// may contain templated values. Only template syntax is validated by
	// Orkestra; structural/schema validation is deferred to the API server.
	Spec map[string]any `json:"spec,omitempty" yaml:"spec,omitempty"`

	// Status is allowed in the declaration for convenience (for example when
	// bootstrapping resources that expect an initial status). Orkestra will
	// only attempt to write status if HasStatus() returns true.
	// Users should prefer letting the controller that owns the CR populate status.
	Status map[string]any `json:"status,omitempty" yaml:"status,omitempty"`

	// Other captures any top-level fields that are not spec/status/metadata.
	// This supports CRDs that place configuration at the top level instead of
	// under spec. This field is inlined during YAML/JSON unmarshalling.
	Other map[string]any `json:"-" yaml:",inline"`

	// HasStatus is an explicit hint about whether the CRD exposes a status
	// subresource. Three states are useful:
	//   - nil: auto-detect via discovery at runtime
	//   - true: force status writes (patches)
	//   - false: never attempt status writes
	// Use this to avoid API errors for CRDs that do not support status.
	HasStatus *bool `json:"hasStatus,omitempty" yaml:"hasStatus,omitempty"`

	Reconcile bool `yaml:"reconcile" json:"reconcile,omitempty"`

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string `json:"sleep,omitempty" yaml:"sleep,omitempty"`

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}

// Create creates the custom resource described by spec if it does not already exist.
// Idempotent — skips if resource exists. Owner reference set for cascade deletion.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedCustomResourceSpec, labelMgr *orklabels.Manager, shouldProtect bool) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("custom.Create: %w", err)
	}

	name := spec.Metadata.Name
	// Resolve namespace (owner may provide defaulting)
	namespace := shared.ResolveNamespace(owner, spec.Metadata.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	// Build GVR from APIVersion/Kind
	gvk, err := buildGVK(spec.APIVersion, spec.Kind)
	if err != nil {
		return fmt.Errorf("custom.Create: invalid GVK: %w", err)
	}

	// Resolve GVR via the registry's RESTMapper (kubeclient exposes Mapper())
	mapper := kube.RESTMapper()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("custom.Create: resolving GVR for %s: %w", gvk.String(), err)
	}
	gvr := mapping.Resource

	dyn := kube.DynamicClient()
	namespaceable := dyn.Resource(gvr)

	var resourceIfc dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		resourceIfc = namespaceable.Namespace(namespace)
	} else {
		resourceIfc = namespaceable
	}

	// Check existence
	_, err = resourceIfc.Get(ctx, name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("custom.Create: checking existence of %q: %w", name, err)
	}
	if err == nil {
		logger.Debug().
			Str("custom", name).
			Str("namespace", namespace).
			Msg("custom resource already exists — skipping create")
		return nil
	}

	// Build unstructured object
	obj := buildUnstructured(spec, owner, gvk, namespace)
	if labelMgr != nil {
		labelMgr.EnsureDeletionProtectionLabel(obj, shouldProtect)
	}

	// Create
	_, err = resourceIfc.Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("custom.Create: creating %q in %q: %w", name, namespace, err)
	}

	logger.Info().
		Str("custom", name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("custom resource created")

	return nil
}

// Update reconciles an existing custom resource to match the resolved spec.
// If the resource does not exist, it will be created.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedCustomResourceSpec, labelMgr *orklabels.Manager, shouldProtect bool) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("custom.Update: %w", err)
	}
	name := spec.Metadata.Name

	namespace := shared.ResolveNamespace(owner, spec.Metadata.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	gvk, err := buildGVK(spec.APIVersion, spec.Kind)
	if err != nil {
		return fmt.Errorf("custom.Update: invalid GVK: %w", err)
	}

	mapper := kube.RESTMapper()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("custom.Update: resolving GVR for %s: %w", gvk.String(), err)
	}
	gvr := mapping.Resource

	dyn := kube.DynamicClient()
	namespaceable := dyn.Resource(gvr)

	var resourceIfc dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		resourceIfc = namespaceable.Namespace(namespace)
	} else {
		resourceIfc = namespaceable
	}

	existing, err := resourceIfc.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info().
				Str("custom", name).
				Str("namespace", namespace).
				Msg("custom resource not found during reconcile — recreating")
			return Create(ctx, kube, owner, spec, labelMgr, shouldProtect)
		}
		return fmt.Errorf("custom.Update: getting %q: %w", name, err)
	}

	// Build desired object
	desired := buildUnstructured(spec, owner, gvk, namespace)

	// Compare spec and top-level Other fields for drift.
	existingSpec, _, _ := unstructured.NestedFieldNoCopy(existing.Object, "spec")
	desiredSpec, _, _ := unstructured.NestedFieldNoCopy(desired.Object, "spec")

	needUpdate := !reflect.DeepEqual(existingSpec, desiredSpec)

	// Also check other top-level fields (non-spec/status/metadata) for drift.
	if !needUpdate {
		for k, v := range desired.Object {
			if k == "apiVersion" || k == "kind" || k == "metadata" || k == "spec" || k == "status" {
				continue
			}
			if !reflect.DeepEqual(existing.Object[k], v) {
				needUpdate = true
				break
			}
		}
	}

	if !needUpdate {
		logger.Debug().
			Str("custom", name).
			Str("namespace", namespace).
			Msg("custom resource in sync — no update needed")
	} else {
		// Prepare updated object: merge desired spec into existing object.
		// Use direct map assignment (not SetNestedField) to avoid DeepCopyJSONValue
		// panicking on non-JSON-safe types (int, int64) produced by YAML unmarshalling.
		updated := existing.DeepCopy()
		if labelMgr != nil {
			labelMgr.EnsureDeletionProtectionLabel(updated, shouldProtect)
		}
		if desired.Object["spec"] != nil {
			updated.Object["spec"] = orktmpl.ToJSONSafe(desired.Object["spec"])
		}
		// Merge other top-level fields from desired (conservative: overwrite)
		for k, v := range desired.Object {
			if k == "apiVersion" || k == "kind" || k == "metadata" || k == "spec" || k == "status" {
				continue
			}
			updated.Object[k] = orktmpl.ToJSONSafe(v)
		}

		_, err = resourceIfc.Update(ctx, updated, metav1.UpdateOptions{})
		if errors.IsConflict(err) {
			// Another controller updated the object between our Get and Update.
			// Re-fetch and retry once — the next reconcile cycle will catch any
			// remaining drift.
			latest, getErr := resourceIfc.Get(ctx, name, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("custom.Update: refreshing %q after conflict: %w", name, getErr)
			}
			updated = latest.DeepCopy()
			if desired.Object["spec"] != nil {
				updated.Object["spec"] = orktmpl.ToJSONSafe(desired.Object["spec"])
			}
			for k, v := range desired.Object {
				if k == "apiVersion" || k == "kind" || k == "metadata" || k == "spec" || k == "status" {
					continue
				}
				updated.Object[k] = orktmpl.ToJSONSafe(v)
			}
			_, err = resourceIfc.Update(ctx, updated, metav1.UpdateOptions{})
		}
		if err != nil {
			return fmt.Errorf("custom.Update: updating %q: %w", name, err)
		}

		logger.Info().
			Str("custom", name).
			Str("namespace", namespace).
			Msg("custom resource updated")
	}

	return nil
}

// DeleteIfOwned deletes the custom resource if it exists and is owned by the given owner.
// Skips deletion if the resource was created by orkdoctor or if Orkestra is not the owner.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface, owner domain.Object, name, namespace, apiVersion, kind string) error {
	// Build GVK and resolve GVR
	gvk, err := buildGVK(apiVersion, kind)
	if err != nil {
		return fmt.Errorf("custom.DeleteIfOwned: invalid GVK: %w", err)
	}

	mapper := kube.RESTMapper()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("custom.DeleteIfOwned: resolving GVR for %s: %w", gvk.String(), err)
	}
	gvr := mapping.Resource

	dyn := kube.DynamicClient()
	namespaceable := dyn.Resource(gvr)

	var resourceIfc dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		resourceIfc = namespaceable.Namespace(namespace)
	} else {
		resourceIfc = namespaceable
	}
	existing, err := resourceIfc.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("custom.DeleteIfOwned: getting %q: %w", name, err)
	}

	// Skip deletion if created by orkdoctor
	labelsMap := existing.GetLabels()
	if labelsMap[orklabels.LabelCreatedBy] == orklabels.CreatedByOrkDoctor {
		return nil
	}

	// Only delete if we own it
	if labelsMap[orklabels.OrkestraOwner] != orklabels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}

	if err := resourceIfc.Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("custom.DeleteIfOwned: deleting %q: %w", name, err)
	}

	logger.Info().
		Str("custom", name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("custom resource deleted")

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func buildUnstructured(spec ResolvedCustomResourceSpec, owner domain.Object, gvk schema.GroupVersionKind, namespace string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(spec.APIVersion)
	u.SetKind(spec.Kind)
	u.SetName(spec.Metadata.Name)
	if namespace != "" {
		u.SetNamespace(namespace)
	}

	// Labels: merge user-declared + Orkestra managed orklabels. Copy rather than
	// mutate spec.Metadata.Labels directly — that map may be shared/reused
	// (e.g. across forEach expansions of the same source).
	lbls := make(map[string]string, len(spec.Metadata.Labels)+3)
	for k, v := range spec.Metadata.Labels {
		lbls[k] = v
	}
	lbls = orklabels.StampOrkestraLabels(lbls, owner.GetName(), owner.GetAnnotations())
	u.SetLabels(lbls)

	// Annotations: copy for the same reason as Labels above.
	if len(spec.Metadata.Annotations) > 0 {
		annotations := make(map[string]string, len(spec.Metadata.Annotations))
		for k, v := range spec.Metadata.Annotations {
			annotations[k] = v
		}
		u.SetAnnotations(annotations)
	}

	// Owner reference — lets Kubernetes garbage-collect the resource when the
	// owner CR is deleted. Skip when the owner is namespaced and the child is
	// in a different namespace: Kubernetes rejects cross-namespace owner references
	// and immediately GC-s the child. Cluster-scoped owners (namespace == "") can
	// own resources in any namespace, so they are always allowed.
	ownerGVK := owner.GetObjectKind().GroupVersionKind()
	ownerIsNamespaced := owner.GetNamespace() != ""
	sameNamespace := !ownerIsNamespaced || namespace == "" || namespace == owner.GetNamespace()
	if !ownerGVK.Empty() && sameNamespace {
		u.SetOwnerReferences(shared.ResolveOwnerReferences(owner))
	}

	// Other top-level fields (non-core) from the spec declaration.
	for k, v := range spec.Other {
		if k == "apiVersion" || k == "kind" || k == "metadata" || k == "spec" || k == "status" {
			continue
		}
		u.Object[k] = v
	}

	// Spec and Status.
	if spec.Spec != nil {
		u.Object["spec"] = spec.Spec
	}
	if spec.Status != nil {
		u.Object["status"] = spec.Status
	}

	// yaml.v3 decodes integer literals as Go int, but k8s fake client's DeepCopy
	// only handles JSON-native types (float64 for numbers, not int). A JSON
	// round-trip normalises the entire object map before it reaches the client.
	if raw, err := json.Marshal(u.Object); err == nil {
		var normalised map[string]any
		if json.Unmarshal(raw, &normalised) == nil {
			u.Object = normalised
		}
	}

	return u
}

func buildGVK(apiVersion, kind string) (schema.GroupVersionKind, error) {
	if apiVersion == "" || kind == "" {
		return schema.GroupVersionKind{}, fmt.Errorf("apiVersion and kind are required")
	}
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	return gv.WithKind(kind), nil
}

// Resolve builds a ResolvedCustomResourceSpec from a CustomResource.
// Template expressions must already be evaluated by template.Resolver before calling.
func Resolve(src orktypes.CustomResourceTemplateSource, ownerName string) ResolvedCustomResourceSpec {
	spec := ResolvedCustomResourceSpec{
		APIVersion:    src.APIVersion,
		Kind:          src.Kind,
		Metadata:      src.Metadata,
		Spec:          src.Spec,
		Status:        src.Status,
		Other:         src.Other,
		HasStatus:     src.HasStatus,
		Reconcile:     src.Reconcile,
		Sleep:         src.Sleep,
		ForceConflict: src.ForceConflict,
	}

	if spec.Metadata.Name == "" {
		spec.Metadata.Name = ownerName + "-custom"
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func validateSpec(spec ResolvedCustomResourceSpec) error {
	if spec.Metadata.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
