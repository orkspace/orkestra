// pkg/resources/resourcequotas/resourcequota.go
package resourcequotas

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

// ResolvedResourceQuotaSpec is the fully resolved ResourceQuota specification.
type ResolvedResourceQuotaSpec struct {
	Name              string
	Namespace         string
	Hard              map[string]string
	FromResourceQuota string
	FromNamespace     string
	Labels            map[string]string
	Sleep             string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}

// Create creates a ResourceQuota if it does not already exist.
// Idempotent — skips if it already exists.
// Owner reference set for cascade deletion.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedResourceQuotaSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().CoreV1().ResourceQuotas(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("resourcequota.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("resourcequota", spec.Name).
			Str("namespace", namespace).
			Msg("resourcequota already exists — skipping create")
		return nil
	}

	hard, err := resolveHard(ctx, kube, spec, owner)
	if err != nil {
		return fmt.Errorf("resourcequota.Create: resolving hard limits: %w", err)
	}

	rq := buildResourceQuota(owner, spec, namespace, hard)

	_, err = kube.Clientset().CoreV1().ResourceQuotas(namespace).Create(ctx, rq, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("resourcequota.Create: creating %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("resourcequota", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("resourcequota created")

	return nil
}

// Apply creates or updates a ResourceQuota using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedResourceQuotaSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	hard, err := resolveHard(ctx, kube, spec, owner)
	if err != nil {
		return fmt.Errorf("resourcequota.Apply: resolving hard limits: %w", err)
	}

	rq := buildResourceQuota(owner, spec, namespace, hard)
	rq.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "ResourceQuota"}

	body, err := json.Marshal(rq)
	if err != nil {
		return fmt.Errorf("resourcequota.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().CoreV1().ResourceQuotas(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("resourcequota.Apply: %w", err)
	}

	logger.Debug().
		Str("resourcequota", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("resourcequota applied")

	return nil
}

// Update applies the ResourceQuota via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedResourceQuotaSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the ResourceQuota if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedResourceQuotaSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().CoreV1().ResourceQuotas(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("resourcequota.Delete: deleting %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("resourcequota", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("resourcequota deleted")

	return nil
}

// DeleteIfOwned deletes the ResourceQuota only if it is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().CoreV1().ResourceQuotas(namespace).
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
	return kube.Clientset().CoreV1().ResourceQuotas(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// CopyToNamespaces copies a ResourceQuota to multiple target namespaces.
// Reads the source quota once and creates copies in each namespace.
// Idempotent — skips namespaces where the quota already exists.
func CopyToNamespaces(
	ctx context.Context,
	kube kubeclient.Interface,
	owner domain.Object,
	spec ResolvedResourceQuotaSpec,
	toNamespaces []string,
) error {
	hard, err := resolveHard(ctx, kube, spec, owner)
	if err != nil {
		return fmt.Errorf("resourcequota.CopyToNamespaces: reading source: %w", err)
	}

	for _, ns := range toNamespaces {
		if ns == "" {
			continue
		}

		_, err := kube.Clientset().CoreV1().ResourceQuotas(ns).Get(ctx, spec.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("resourcequota.CopyToNamespaces: checking %q in %q: %w", spec.Name, ns, err)
		}
		if err == nil {
			logger.Debug().
				Str("resourcequota", spec.Name).
				Str("namespace", ns).
				Msg("resourcequota already exists in namespace — skipping")
			continue
		}

		nsSpec := spec
		nsSpec.Namespace = ns
		rq := buildResourceQuota(owner, nsSpec, ns, hard)

		_, err = kube.Clientset().CoreV1().ResourceQuotas(ns).Create(ctx, rq, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("resourcequota.CopyToNamespaces: creating %q in %q: %w", spec.Name, ns, err)
		}

		logger.Info().
			Str("resourcequota", spec.Name).
			Str("namespace", ns).
			Str("owner", owner.GetName()).
			Msg("resourcequota copied to namespace")
	}

	return nil
}

// Resolve builds a ResolvedResourceQuotaSpec from a ResourceQuotaTemplateSource.
// Template expressions must already be evaluated by template.Resolver before calling.
func Resolve(src orktypes.ResourceQuotaTemplateSource, ownerName string, reg orktypes.ProfileRegistry) ResolvedResourceQuotaSpec {
	hard := src.Hard
	if src.Profile != "" {
		if expanded, err := profiles.ApplyResourceQuotaProfile(src.Profile, reg); err != nil {
			logger.Warn().Str("profile", src.Profile).Err(err).Msg("unknown resourcequota profile — skipping")
		} else {
			hard = expanded.Hard
		}
	}

	spec := ResolvedResourceQuotaSpec{
		Name:              src.Name,
		Namespace:         src.Namespace,
		Hard:              hard,
		FromResourceQuota: src.FromResourceQuota,
		FromNamespace:     src.FromNamespace,
		Labels:            make(map[string]string),
		Sleep:             src.Sleep,
		ForceConflict:     src.ForceConflict,
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// resolveHard returns the hard limits to apply.
// When FromResourceQuota is set, copies limits from the source quota.
// Otherwise uses the declared Hard map.
func resolveHard(
	ctx context.Context,
	kube kubeclient.Interface,
	spec ResolvedResourceQuotaSpec,
	owner domain.Object,
) (map[string]string, error) {
	if spec.FromResourceQuota == "" {
		return spec.Hard, nil
	}

	fromNS := spec.FromNamespace
	if fromNS == "" {
		fromNS = owner.GetNamespace()
	}
	if fromNS == "" {
		fromNS = "default"
	}

	source, err := kube.Clientset().CoreV1().ResourceQuotas(fromNS).
		Get(ctx, spec.FromResourceQuota, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("reading source resourcequota %q from %q: %w",
			spec.FromResourceQuota, fromNS, err)
	}

	hard := make(map[string]string, len(source.Spec.Hard))
	for k, v := range source.Spec.Hard {
		hard[string(k)] = v.String()
	}
	return hard, nil
}

func buildResourceList(hard map[string]string) corev1.ResourceList {
	rl := make(corev1.ResourceList, len(hard))
	for k, v := range hard {
		q, err := resource.ParseQuantity(v)
		if err != nil {
			continue
		}
		rl[corev1.ResourceName(k)] = q
	}
	return rl
}

func buildResourceQuota(
	owner domain.Object,
	spec ResolvedResourceQuotaSpec,
	namespace string,
	hard map[string]string,
) *corev1.ResourceQuota {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: buildResourceList(hard),
		},
	}
}
