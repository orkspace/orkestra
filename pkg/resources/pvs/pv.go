// pkg/resources/pvs/pv.go
package pvs

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
	"github.com/orkspace/orkestra/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// Create creates a PersistentVolume if it does not already exist.
// PVs are cluster-scoped — owner references are set as labels only.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedPVSpec) error {
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}
	_, err := kube.Clientset().CoreV1().PersistentVolumes().Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("pv.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().Str("pv", spec.Name).Msg("pv already exists — skipping create")
		return nil
	}

	pv := buildPV(owner, spec)
	_, err = kube.Clientset().CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("pv.Create: creating %q: %w", spec.Name, err)
	}

	logger.Info().Str("pv", spec.Name).Str("owner", owner.GetName()).Msg("pv created")
	return nil
}

// Apply creates or updates a PersistentVolume using Server-Side Apply.
// PVs are cluster-scoped — no namespace arg. Sends only fields Orkestra owns.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedPVSpec) error {
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	pv := buildPV(owner, spec)
	pv.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolume"}

	body, err := json.Marshal(pv)
	if err != nil {
		return fmt.Errorf("pv.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().CoreV1().PersistentVolumes().Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("pv.Apply: %w", err)
	}

	logger.Debug().
		Str("pv", spec.Name).
		Str("owner", owner.GetName()).
		Msg("pv applied")

	return nil
}

// Update applies the PV via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedPVSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the PV if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, name string) error {
	err := kube.Clientset().CoreV1().PersistentVolumes().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("pv.Delete: %w", err)
	}
	logger.Info().Str("pv", name).Msg("pv deleted")
	return nil
}

// DeleteIfOwned deletes the PV only if the owner label matches.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface, owner domain.Object, name string) error {
	existing, err := kube.Clientset().CoreV1().PersistentVolumes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().CoreV1().PersistentVolumes().Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedPVSpec from a PVTemplateSource.
func Resolve(src orktypes.PVTemplateSource, ownerName string) ResolvedPVSpec {
	spec := ResolvedPVSpec{
		Name:             src.Name,
		StorageClassName: src.StorageClassName,
		Capacity:         src.Capacity,
		AccessModes:      src.AccessModes,
		ReclaimPolicy:    src.ReclaimPolicy,
		HostPath:         src.HostPath,
		CSIDriver:        src.CSIDriver,
		CSIVolumeHandle:  src.CSIVolumeHandle,
		Labels:           make(map[string]string),
		Sleep:            src.Sleep,
		ForceConflict:    src.ForceConflict,
	}

	if len(spec.AccessModes) == 0 {
		spec.AccessModes = []string{"ReadWriteOnce"}
	}
	if spec.ReclaimPolicy == "" {
		spec.ReclaimPolicy = "Retain"
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildPV(owner domain.Object, spec ResolvedPVSpec) *corev1.PersistentVolume {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	capacityQty := resource.MustParse(spec.Capacity)

	var accessModes []corev1.PersistentVolumeAccessMode
	for _, m := range spec.AccessModes {
		accessModes = append(accessModes, corev1.PersistentVolumeAccessMode(m))
	}

	reclaimPolicy := corev1.PersistentVolumeReclaimRetain
	switch spec.ReclaimPolicy {
	case "Delete":
		reclaimPolicy = corev1.PersistentVolumeReclaimDelete
	case "Recycle":
		reclaimPolicy = corev1.PersistentVolumeReclaimRecycle
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:   spec.Name,
			Labels: spec.Labels,
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: capacityQty,
			},
			AccessModes:                   accessModes,
			PersistentVolumeReclaimPolicy: reclaimPolicy,
			StorageClassName:              spec.StorageClassName,
		},
	}

	if spec.HostPath != "" {
		pv.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: spec.HostPath},
		}
	} else if spec.CSIDriver != "" {
		pv.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{
			CSI: &corev1.CSIPersistentVolumeSource{
				Driver:       spec.CSIDriver,
				VolumeHandle: spec.CSIVolumeHandle,
			},
		}
	}

	if owner.GetNamespace() == "" {
		pv.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         owner.GetObjectKind().GroupVersionKind().GroupVersion().String(),
				Kind:               owner.GetObjectKind().GroupVersionKind().Kind,
				Name:               owner.GetName(),
				UID:                owner.GetUID(),
				Controller:         utils.BoolPtr(true),
				BlockOwnerDeletion: utils.BoolPtr(true),
			},
		}
	}

	return pv
}
