// pkg/resources/pvcs/pvc.go
package pvcs

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Create creates a PVC owned by the CR if it does not already exist.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedPVCSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().CoreV1().PersistentVolumeClaims(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("pvc.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().Str("pvc", spec.Name).Str("namespace", namespace).Msg("pvc already exists — skipping create")
		return nil
	}

	pvc := buildPVC(owner, spec, namespace)
	_, err = kube.Clientset().CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("pvc.Create: creating %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().Str("pvc", spec.Name).Str("namespace", namespace).Str("owner", owner.GetName()).Msg("pvc created")
	return nil
}

// Update reconciles a PVC. PVC spec is largely immutable after creation;
// only labels are patched on drift.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedPVCSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	existing, err := kube.Clientset().CoreV1().PersistentVolumeClaims(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return Create(ctx, kube, owner, spec)
		}
		return fmt.Errorf("pvc.Update: getting %q: %w", spec.Name, err)
	}

	updated := existing.DeepCopy()
	drifted := false

	for k, v := range spec.Labels {
		if updated.Labels[k] != v {
			updated.Labels[k] = v
			drifted = true
		}
	}

	if !drifted {
		logger.Debug().Str("pvc", spec.Name).Msg("pvc in sync — no update needed")
		return nil
	}

	_, err = kube.Clientset().CoreV1().PersistentVolumeClaims(namespace).Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("pvc.Update: updating %q: %w", spec.Name, err)
	}

	logger.Info().Str("pvc", spec.Name).Str("namespace", namespace).Msg("pvc updated")
	return nil
}

// Delete deletes the PVC if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedPVCSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("pvc.Delete: %w", err)
	}
	logger.Info().Str("pvc", spec.Name).Str("owner", owner.GetName()).Msg("pvc deleted")
	return nil
}

// DeleteIfOwned deletes the PVC only if it is owned by the given CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface, owner domain.Object, name, namespace string) error {
	existing, err := kube.Clientset().CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedPVCSpec from a PVCTemplateSource.
func Resolve(src orktypes.PVCTemplateSource, ownerName string) ResolvedPVCSpec {
	spec := ResolvedPVCSpec{
		Name:             src.Name,
		Namespace:        src.Namespace,
		StorageClassName: src.StorageClassName,
		AccessModes:      src.AccessModes,
		Storage:          src.Storage,
		VolumeMode:       src.VolumeMode,
		VolumeName:       src.VolumeName,
		Labels:           make(map[string]string),
		Sleep:            src.Sleep,
		ForceConflict:    src.ForceConflict,
	}

	if len(spec.AccessModes) == 0 {
		spec.AccessModes = []string{"ReadWriteOnce"}
	}
	if spec.VolumeMode == "" {
		spec.VolumeMode = "Filesystem"
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildPVC(owner domain.Object, spec ResolvedPVCSpec, ns string) *corev1.PersistentVolumeClaim {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())

	storageQty := resource.MustParse(spec.Storage)

	var accessModes []corev1.PersistentVolumeAccessMode
	for _, m := range spec.AccessModes {
		accessModes = append(accessModes, corev1.PersistentVolumeAccessMode(m))
	}

	volumeMode := corev1.PersistentVolumeFilesystem
	if spec.VolumeMode == "Block" {
		volumeMode = corev1.PersistentVolumeBlock
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       ns,
			Labels:          spec.Labels,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			VolumeMode:  &volumeMode,
			VolumeName:  spec.VolumeName,
			// DataSourceRef: ,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageQty,
				},
			},
		},
	}

	if spec.StorageClassName != "" {
		pvc.Spec.StorageClassName = &spec.StorageClassName
	}

	return pvc
}
