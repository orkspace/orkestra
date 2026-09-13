// pkg/resources/limitranges/limitrange.go
package limitranges

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/profiles"
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// ResolvedLimitRangeSpec is the fully resolved LimitRange specification.
type ResolvedLimitRangeSpec struct {
	Name           string
	Namespace      string
	Limits         []orktypes.LimitRangeItem
	FromLimitRange string
	FromNamespace  string
	Labels         map[string]string
	Sleep          string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}

// Create creates a LimitRange if it does not already exist.
// Idempotent — skips if it already exists.
// Owner reference set for cascade deletion.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedLimitRangeSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().CoreV1().LimitRanges(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("limitrange.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("limitrange", spec.Name).
			Str("namespace", namespace).
			Msg("limitrange already exists — skipping create")
		return nil
	}

	limits, err := resolveLimits(ctx, kube, spec, owner)
	if err != nil {
		return fmt.Errorf("limitrange.Create: resolving limits: %w", err)
	}

	lr := buildLimitRange(owner, spec, namespace, limits)

	_, err = kube.Clientset().CoreV1().LimitRanges(namespace).Create(ctx, lr, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("limitrange.Create: creating %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("limitrange", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("limitrange created")

	return nil
}

// Apply creates or updates a LimitRange using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedLimitRangeSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	limits, err := resolveLimits(ctx, kube, spec, owner)
	if err != nil {
		return fmt.Errorf("limitrange.Apply: resolving limits: %w", err)
	}

	lr := buildLimitRange(owner, spec, namespace, limits)
	lr.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "LimitRange"}

	body, err := json.Marshal(lr)
	if err != nil {
		return fmt.Errorf("limitrange.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().CoreV1().LimitRanges(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("limitrange.Apply: %w", err)
	}

	logger.Debug().
		Str("limitrange", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("limitrange applied")

	return nil
}

// Update applies the LimitRange via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedLimitRangeSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the LimitRange if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedLimitRangeSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().CoreV1().LimitRanges(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("limitrange.Delete: deleting %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("limitrange", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("limitrange deleted")

	return nil
}

// DeleteIfOwned deletes the LimitRange only if it is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().CoreV1().LimitRanges(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().CoreV1().LimitRanges(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// CopyToNamespaces copies a LimitRange to multiple target namespaces.
// Reads the source once and creates copies in each namespace.
// Idempotent — skips namespaces where the LimitRange already exists.
func CopyToNamespaces(
	ctx context.Context,
	kube kubeclient.Interface,
	owner domain.Object,
	spec ResolvedLimitRangeSpec,
	toNamespaces []string,
) error {
	limits, err := resolveLimits(ctx, kube, spec, owner)
	if err != nil {
		return fmt.Errorf("limitrange.CopyToNamespaces: reading source: %w", err)
	}

	for _, ns := range toNamespaces {
		if ns == "" {
			continue
		}

		_, err := kube.Clientset().CoreV1().LimitRanges(ns).Get(ctx, spec.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("limitrange.CopyToNamespaces: checking %q in %q: %w", spec.Name, ns, err)
		}
		if err == nil {
			logger.Debug().
				Str("limitrange", spec.Name).
				Str("namespace", ns).
				Msg("limitrange already exists in namespace — skipping")
			continue
		}

		nsSpec := spec
		nsSpec.Namespace = ns
		lr := buildLimitRange(owner, nsSpec, ns, limits)

		_, err = kube.Clientset().CoreV1().LimitRanges(ns).Create(ctx, lr, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("limitrange.CopyToNamespaces: creating %q in %q: %w", spec.Name, ns, err)
		}

		logger.Info().
			Str("limitrange", spec.Name).
			Str("namespace", ns).
			Str("owner", owner.GetName()).
			Msg("limitrange copied to namespace")
	}

	return nil
}

// Resolve builds a ResolvedLimitRangeSpec from a LimitRangeTemplateSource.
// Template expressions must already be evaluated by template.Resolver before calling.
func Resolve(src orktypes.LimitRangeTemplateSource, ownerName string, reg orktypes.ProfileRegistry) ResolvedLimitRangeSpec {
	limits := src.Limits
	if src.Profile != "" && len(limits) == 0 {
		if expanded, err := profiles.ApplyLimitRangeProfile(src.Profile, reg); err == nil {
			limits = expanded
		}
	}

	spec := ResolvedLimitRangeSpec{
		Name:           src.Name,
		Namespace:      src.Namespace,
		Limits:         limits,
		FromLimitRange: src.FromLimitRange,
		FromNamespace:  src.FromNamespace,
		Labels:         make(map[string]string),
		Sleep:          src.Sleep,
		ForceConflict:  src.ForceConflict,
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// resolveLimits returns the LimitRangeItems to apply.
// When FromLimitRange is set, copies limits from the source.
// Otherwise uses the declared Limits slice.
func resolveLimits(
	ctx context.Context,
	kube kubeclient.Interface,
	spec ResolvedLimitRangeSpec,
	owner domain.Object,
) ([]orktypes.LimitRangeItem, error) {
	if spec.FromLimitRange == "" {
		return spec.Limits, nil
	}

	fromNS := spec.FromNamespace
	if fromNS == "" {
		fromNS = owner.GetNamespace()
	}
	if fromNS == "" {
		fromNS = "default"
	}

	source, err := kube.Clientset().CoreV1().LimitRanges(fromNS).
		Get(ctx, spec.FromLimitRange, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("reading source limitrange %q from %q: %w",
			spec.FromLimitRange, fromNS, err)
	}

	items := make([]orktypes.LimitRangeItem, 0, len(source.Spec.Limits))
	for _, item := range source.Spec.Limits {
		items = append(items, orktypes.LimitRangeItem{
			Type:                 string(item.Type),
			Max:                  resourceListToMap(item.Max),
			Min:                  resourceListToMap(item.Min),
			Default:              resourceListToMap(item.Default),
			DefaultRequest:       resourceListToMap(item.DefaultRequest),
			MaxLimitRequestRatio: resourceListToMap(item.MaxLimitRequestRatio),
		})
	}
	return items, nil
}

func buildLimitRange(
	owner domain.Object,
	spec ResolvedLimitRangeSpec,
	namespace string,
	limits []orktypes.LimitRangeItem,
) *corev1.LimitRange {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	return &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: corev1.LimitRangeSpec{
			Limits: buildLimitRangeItems(limits),
		},
	}
}

func buildLimitRangeItems(items []orktypes.LimitRangeItem) []corev1.LimitRangeItem {
	out := make([]corev1.LimitRangeItem, 0, len(items))
	for _, item := range items {
		lri := corev1.LimitRangeItem{
			Type:                 corev1.LimitType(item.Type),
			Max:                  mapToResourceList(item.Max),
			Min:                  mapToResourceList(item.Min),
			Default:              mapToResourceList(item.Default),
			DefaultRequest:       mapToResourceList(item.DefaultRequest),
			MaxLimitRequestRatio: mapToResourceList(item.MaxLimitRequestRatio),
		}
		out = append(out, lri)
	}
	return out
}

func limitRangeItemsEqual(a, b []corev1.LimitRangeItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type {
			return false
		}
		if !resourceListEqual(a[i].Max, b[i].Max) ||
			!resourceListEqual(a[i].Min, b[i].Min) ||
			!resourceListEqual(a[i].Default, b[i].Default) ||
			!resourceListEqual(a[i].DefaultRequest, b[i].DefaultRequest) ||
			!resourceListEqual(a[i].MaxLimitRequestRatio, b[i].MaxLimitRequestRatio) {
			return false
		}
	}
	return true
}

func resourceListEqual(a, b corev1.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for k, qa := range a {
		qb, ok := b[k]
		if !ok {
			return false
		}
		if qa.Cmp(qb) != 0 {
			return false
		}
	}
	return true
}

func mapToResourceList(m map[string]string) corev1.ResourceList {
	if len(m) == 0 {
		return nil
	}
	rl := make(corev1.ResourceList, len(m))
	for k, v := range m {
		q, err := resource.ParseQuantity(v)
		if err != nil {
			continue
		}
		rl[corev1.ResourceName(k)] = q
	}
	return rl
}

func resourceListToMap(rl corev1.ResourceList) map[string]string {
	if len(rl) == 0 {
		return nil
	}
	m := make(map[string]string, len(rl))
	for k, v := range rl {
		m[string(k)] = v.String()
	}
	return m
}
