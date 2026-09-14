// pkg/resources/statefulsets/statefulset.go
package statefulsets

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/profiles"
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// Create creates a StatefulSet owned by the CR if it does not already exist.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedStatefulSetSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().AppsV1().StatefulSets(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("statefulset.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("statefulset", spec.Name).
			Str("namespace", namespace).
			Msg("statefulset already exists — skipping create")
		return nil
	}

	sts := buildStatefulSet(owner, spec, namespace)
	_, err = kube.Clientset().AppsV1().StatefulSets(namespace).Create(ctx, sts, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("statefulset.Create: creating %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("statefulset", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("statefulset created")
	return nil
}

// Apply creates or updates a StatefulSet using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedStatefulSetSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	sts := buildStatefulSet(owner, spec, namespace)
	sts.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"}

	body, err := json.Marshal(sts)
	if err != nil {
		return fmt.Errorf("statefulset.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().AppsV1().StatefulSets(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("statefulset.Apply: %w", err)
	}

	logger.Debug().
		Str("statefulset", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("statefulset applied")

	return nil
}

// Update applies the StatefulSet via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedStatefulSetSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the StatefulSet if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedStatefulSetSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().AppsV1().StatefulSets(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("statefulset.Delete: %w", err)
	}
	logger.Info().Str("statefulset", spec.Name).Str("owner", owner.GetName()).Msg("statefulset deleted")
	return nil
}

// DeleteIfOwned deletes the StatefulSet only if it is owned by the given CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface, owner domain.Object, name, namespace string) error {
	existing, err := kube.Clientset().AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().AppsV1().StatefulSets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedStatefulSetSpec from a StatefulSetTemplateSource.
func Resolve(src orktypes.StatefulSetTemplateSource, ownerName string, reg orktypes.ProfileRegistry) ResolvedStatefulSetSpec {
	spec := ResolvedStatefulSetSpec{
		Name:            src.Name,
		Namespace:       src.Namespace,
		Image:           src.Image,
		ServiceName:     src.ServiceName,
		Replicas:        shared.ParseReplicas(src.Replicas),
		HasAutoscale:    src.Autoscale != nil,
		Labels:          make(map[string]string),
		Annotations:     make(map[string]string),
		Env:             src.Env,
		EnvFrom:         src.EnvFrom,
		Resources:       shared.ResolveResources(src.Resources, reg),
		Probes:          src.Probes,
		Profiles:        reg,
		SecurityContext: shared.ResolveContainerSecurityContext(src.SecurityContext, reg),
		PodSecurity:     shared.ResolvePodSecurityContext(src.PodSecurity, reg),
		Volumes:         src.Volumes,
		VolumeMounts:    src.VolumeMounts,
		Sleep:           src.Sleep,
		ForceConflict:   src.ForceConflict,
	}

	for _, vct := range src.VolumeClaimTemplates {
		resolved := ResolvedVolumeClaimTemplate{
			Name:         vct.Name,
			StorageClass: vct.StorageClass,
			StorageSize:  vct.StorageSize,
			MountPath:    vct.MountPath,
			AccessModes:  vct.AccessModes,
		}
		if resolved.Name == "" {
			resolved.Name = "data"
		}
		if resolved.MountPath == "" {
			resolved.MountPath = "/data"
		}
		spec.VolumeClaimTemplates = append(spec.VolumeClaimTemplates, resolved)
	}

	if spec.Name == "" {
		spec.Name = ownerName
	}
	if spec.ServiceName == "" {
		spec.ServiceName = spec.Name
	}

	if src.Tag != "" {
		spec.Image = src.Image + ":" + src.Tag
	}
	if p, err := strconv.ParseInt(src.Port, 10, 32); err == nil {
		spec.Port = int32(p)
	}
	spec.Protocol = shared.ParseProtocol(src.Protocol)

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}
	for k, v := range src.Annotations {
		spec.Annotations[k] = v
	}

	if src.RollingUpdate != nil && src.RollingUpdate.Profile != "" {
		expansion, err := profiles.ApplyRollingUpdateProfile(src.RollingUpdate.Profile, reg)
		if err != nil {
			logger.Warn().Str("profile", src.RollingUpdate.Profile).Err(err).Msg("unknown rolling update profile — skipping")
		} else {
			spec.RollingUpdate = &orktypes.RollingUpdateBehavior{
				MaxSurge:       expansion.MaxSurge,
				MaxUnavailable: expansion.MaxUnavailable,
			}
		}
	} else if src.RollingUpdate != nil {
		r := *src.RollingUpdate
		spec.RollingUpdate = &r
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func resolveAccessModes(modes []string) []corev1.PersistentVolumeAccessMode {
	if len(modes) == 0 {
		return []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}
	out := make([]corev1.PersistentVolumeAccessMode, 0, len(modes))
	for _, m := range modes {
		switch m {
		case "ReadWriteMany":
			out = append(out, corev1.ReadWriteMany)
		case "ReadOnlyMany":
			out = append(out, corev1.ReadOnlyMany)
		case "ReadWriteOncePod":
			out = append(out, corev1.ReadWriteOncePod)
		default:
			out = append(out, corev1.ReadWriteOnce)
		}
	}
	return out
}

func buildStatefulSet(owner domain.Object, spec ResolvedStatefulSetSpec, ns string) *appsv1.StatefulSet {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())

	replicas := spec.Replicas
	container := corev1.Container{
		Name:  spec.Name,
		Image: spec.Image,
	}

	if spec.Port > 0 {
		container.Ports = []corev1.ContainerPort{{ContainerPort: spec.Port, Protocol: spec.Protocol}}
	}

	if spec.Resources != nil {
		container.Resources = shared.BuildResourceRequirements(spec.Resources)
	}

	shared.ApplyProbes(&container, spec.Probes, spec.Port, spec.Profiles)

	for _, ev := range spec.Env {
		kev := corev1.EnvVar{Name: ev.Name}
		if ev.ValueFrom != nil {
			kev.ValueFrom = &corev1.EnvVarSource{}
			if ev.ValueFrom.SecretKeyRef != nil {
				kev.ValueFrom.SecretKeyRef = &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ev.ValueFrom.SecretKeyRef.Name},
					Key:                  ev.ValueFrom.SecretKeyRef.Key,
					Optional:             ev.ValueFrom.SecretKeyRef.Optional,
				}
			}
			if ev.ValueFrom.ConfigMapKeyRef != nil {
				kev.ValueFrom.ConfigMapKeyRef = &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ev.ValueFrom.ConfigMapKeyRef.Name},
					Key:                  ev.ValueFrom.ConfigMapKeyRef.Key,
					Optional:             ev.ValueFrom.ConfigMapKeyRef.Optional,
				}
			}
		} else {
			kev.Value = ev.Value
		}
		container.Env = append(container.Env, kev)
	}

	envFrom, extraEnv := shared.ExpandEnvFrom(spec.EnvFrom)
	container.EnvFrom = envFrom
	container.Env = append(container.Env, extraEnv...)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       ns,
			Labels:          spec.Labels,
			Annotations:     spec.Annotations,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: spec.ServiceName,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					labels.OrkestraOwner: owner.GetName(),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: spec.Labels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets:   shared.ToPullSecrets(spec.ImagePullSecrets),
					ServiceAccountName: spec.ServiceAccountName,
					NodeSelector:       spec.NodeSelector,
					Containers:         []corev1.Container{container},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{},
			PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: appsv1.PersistentVolumeClaimRetentionPolicyType(spec.VolumeClaimRetentionPolicy.WhenDeleted),
				WhenScaled:  appsv1.PersistentVolumeClaimRetentionPolicyType(spec.VolumeClaimRetentionPolicy.WhenScaled),
			},
			UpdateStrategy: func() appsv1.StatefulSetUpdateStrategy {
				if spec.RollingUpdate != nil {
					return shared.BuildStatefulSetUpdateStrategy(spec.RollingUpdate)
				}
				return appsv1.StatefulSetUpdateStrategy{Type: appsv1.OnDeleteStatefulSetStrategyType}
			}(),
			PodManagementPolicy: appsv1.ParallelPodManagement,
		},
	}

	// Security
	shared.ApplySecurityContext(&sts.Spec.Template.Spec.Containers[0], &sts.Spec.Template.Spec, spec.SecurityContext, spec.PodSecurity)

	for _, vct := range spec.VolumeClaimTemplates {
		storageQty := resource.MustParse(vct.StorageSize)
		name := vct.Name
		if name == "" {
			name = "data"
		}
		mountPath := vct.MountPath
		if mountPath == "" {
			mountPath = "/data"
		}
		accessModes := resolveAccessModes(vct.AccessModes)
		sts.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			sts.Spec.Template.Spec.Containers[0].VolumeMounts,
			corev1.VolumeMount{Name: name, MountPath: mountPath},
		)
		sts.Spec.VolumeClaimTemplates = append(sts.Spec.VolumeClaimTemplates, corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      accessModes,
				StorageClassName: &vct.StorageClass,
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: storageQty,
					},
				},
			},
		})
	}

	// Volumes / VolumeMounts (generic, in addition to VolumeClaimTemplates)
	if vols := shared.BuildVolumes(spec.Volumes); len(vols) > 0 {
		sts.Spec.Template.Spec.Volumes = vols
	}
	if mounts := shared.BuildVolumeMounts(spec.VolumeMounts); len(mounts) > 0 {
		sts.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			sts.Spec.Template.Spec.Containers[0].VolumeMounts, mounts...,
		)
	}

	return sts
}
