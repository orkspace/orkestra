// pkg/resources/configmaps/configmap.go
package configmaps

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// ResolvedConfigMapSpec is the fully resolved ConfigMap specification.
type ResolvedConfigMapSpec struct {
	// Name — ConfigMap name. Required.
	Name string

	// Namespace — target namespace. Required.
	Namespace string

	// Data — key-value configuration entries.
	Data map[string]string

	// FromConfigMap — name of a source ConfigMap to copy from.
	// When set, copies all keys from the source into the target.
	// Individual Data entries merge on top — override specific keys if needed.
	FromConfigMap string

	// FromNamespace — namespace where FromConfigMap lives.
	// Default: same namespace as the CR.
	FromNamespace string

	// Labels — applied to ConfigMap metadata.
	Labels map[string]string

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}

// Create creates a ConfigMap if it does not already exist.
// Idempotent — skips if ConfigMap exists.
// Owner reference set for cascade deletion.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedConfigMapSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("configmap.Create: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().CoreV1().ConfigMaps(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("configmap.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("configmap", spec.Name).
			Str("namespace", namespace).
			Msg("configmap already exists — skipping create")
		return nil
	}

	data, err := resolveData(ctx, kube, spec, owner)
	if err != nil {
		return fmt.Errorf("configmap.Create: resolving data: %w", err)
	}

	cm := buildConfigMap(owner, spec, namespace, data)

	_, err = kube.Clientset().CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("configmap.Create: creating %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("configmap", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("configmap created")

	return nil
}

// Apply creates or updates a ConfigMap using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedConfigMapSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("configmap.Apply: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	data, err := resolveData(ctx, kube, spec, owner)
	if err != nil {
		return fmt.Errorf("configmap.Apply: resolving data: %w", err)
	}

	cm := buildConfigMap(owner, spec, namespace, data)
	cm.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}

	body, err := json.Marshal(cm)
	if err != nil {
		return fmt.Errorf("configmap.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().CoreV1().ConfigMaps(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("configmap.Apply: %w", err)
	}

	logger.Debug().
		Str("configmap", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("configmap applied")

	return nil
}

// Update applies the ConfigMap via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedConfigMapSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the ConfigMap if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedConfigMapSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().CoreV1().ConfigMaps(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug().
				Str("configmap", spec.Name).
				Str("namespace", namespace).
				Msg("configmap already deleted — skipping")
			return nil
		}
		return fmt.Errorf("configmap.Delete: deleting %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("configmap", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("configmap deleted")

	return nil
}

// CopyToNamespaces copies a ConfigMap to multiple target namespaces.
// Reads the source once and creates copies in each namespace.
// Idempotent — skips namespaces where the ConfigMap already exists.
//
// Example Katalog declaration:
//
//	onCreate:
//	  configMaps:
//	    - name: app-config
//	      fromConfigMap: base-app-config
//	      fromNamespace: platform
//	      toNamespaces:
//	        - "{{ .metadata.namespace }}"
//	        - staging
//	        - production
func CopyToNamespaces(
	ctx context.Context,
	kube kubeclient.Interface,
	owner domain.Object,
	spec ResolvedConfigMapSpec,
	toNamespaces []string,
) error {
	// Fetch source once — reuse for all target namespaces
	data, err := resolveData(ctx, kube, spec, owner)
	if err != nil {
		return fmt.Errorf("configmap.CopyToNamespaces: reading source: %w", err)
	}

	for _, ns := range toNamespaces {
		if ns == "" {
			continue
		}

		_, err := kube.Clientset().CoreV1().ConfigMaps(ns).Get(ctx, spec.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("configmap.CopyToNamespaces: checking %q in %q: %w", spec.Name, ns, err)
		}
		if err == nil {
			logger.Debug().
				Str("configmap", spec.Name).
				Str("namespace", ns).
				Msg("configmap already exists in namespace — skipping")
			continue
		}

		nsSpec := spec
		nsSpec.Namespace = ns
		cm := buildConfigMap(owner, nsSpec, ns, data)

		_, err = kube.Clientset().CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("configmap.CopyToNamespaces: creating %q in %q: %w", spec.Name, ns, err)
		}

		logger.Info().
			Str("configmap", spec.Name).
			Str("namespace", ns).
			Str("owner", owner.GetName()).
			Msg("configmap copied to namespace")
	}

	return nil
}

// DeleteIfOwned ConfigMaps the Job if it exists and is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().CoreV1().ConfigMaps(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	// Skip deletion if created by orkdoctor
	if existing.Labels[labels.LabelCreatedBy] == labels.CreatedByOrkDoctor {
		return nil
	}

	// Only delete if we own it
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().CoreV1().ConfigMaps(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedConfigMapSpec from a ConfigMapTemplateSource.
// Template expressions must already be evaluated by template.Resolver before calling.
func Resolve(src orktypes.ConfigMapTemplateSource, ownerName string) ResolvedConfigMapSpec {
	spec := ResolvedConfigMapSpec{
		Name:          src.Name,
		Namespace:     src.Namespace,
		Data:          src.Data,
		FromConfigMap: src.FromConfigMap,
		FromNamespace: src.FromNamespace,
		Labels:        make(map[string]string),
		Sleep:         src.Sleep,
		ForceConflict: src.ForceConflict,
	}

	if spec.Name == "" {
		spec.Name = ownerName + "-config"
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// resolveData merges FromConfigMap source data with declared Data.
// Declared Data keys win over source keys — allows targeted overrides.
func resolveData(
	ctx context.Context,
	kube kubeclient.Interface,
	spec ResolvedConfigMapSpec,
	owner domain.Object,
) (map[string]string, error) {
	merged := make(map[string]string)

	// Base: copy from source ConfigMap if declared
	if spec.FromConfigMap != "" {
		fromNS := spec.FromNamespace
		if fromNS == "" {
			fromNS = owner.GetNamespace()
		}
		if fromNS == "" {
			fromNS = "default"
		}

		source, err := kube.Clientset().CoreV1().ConfigMaps(fromNS).Get(ctx, spec.FromConfigMap, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("reading source configmap %q from %q: %w", spec.FromConfigMap, fromNS, err)
		}

		for k, v := range source.Data {
			merged[k] = v
		}

		logger.Debug().
			Str("source", spec.FromConfigMap).
			Str("sourceNamespace", fromNS).
			Int("keys", len(source.Data)).
			Msg("copied configmap data from source")
	}

	// Override: declared Data keys win over source keys
	for k, v := range spec.Data {
		merged[k] = v
	}

	return merged, nil
}

func buildConfigMap(owner domain.Object, spec ResolvedConfigMapSpec, namespace string, data map[string]string) *corev1.ConfigMap {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Data: data,
	}
}

func validateSpec(spec ResolvedConfigMapSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
