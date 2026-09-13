// pkg/resources/replicasets/replicaset.go
package replicasets

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// Create creates a ReplicaSet owned by the CR if it does not already exist.
// Idempotent — if the ReplicaSet exists, does nothing and returns nil.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedReplicaSetSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("replicaset.Create: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().AppsV1().ReplicaSets(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("replicaset.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("replicaset", spec.Name).
			Str("namespace", namespace).
			Msg("replicaset already exists — skipping create")
		return nil
	}

	rs := buildReplicaSet(owner, spec, namespace)

	_, err = kube.Clientset().AppsV1().ReplicaSets(namespace).Create(ctx, rs, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("replicaset.Create: creating replicaset %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("replicaset", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("replicaset created")

	return nil
}

// Apply creates or updates a ReplicaSet using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedReplicaSetSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("replicaset.Apply: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	rs := buildReplicaSet(owner, spec, namespace)
	rs.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSet"}

	body, err := json.Marshal(rs)
	if err != nil {
		return fmt.Errorf("replicaset.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().AppsV1().ReplicaSets(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("replicaset.Apply: %w", err)
	}

	logger.Debug().
		Str("replicaset", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("replicaset applied")

	return nil
}

// Update applies the ReplicaSet via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedReplicaSetSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the ReplicaSet if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedReplicaSetSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().AppsV1().ReplicaSets(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug().
				Str("replicaset", spec.Name).
				Str("namespace", namespace).
				Msg("replicaset already deleted — skipping")
			return nil
		}
		return fmt.Errorf("replicaset.Delete: deleting replicaset %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("replicaset", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("replicaset deleted")

	return nil
}

// DeleteIfOwned deletes the ReplicaSet only if it is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().AppsV1().ReplicaSets(namespace).
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

	return kube.Clientset().AppsV1().ReplicaSets(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedReplicaSetSpec from a ReplicaSetTemplateSource.
func Resolve(src orktypes.ReplicaSetTemplateSource, ownerName string, reg orktypes.ProfileRegistry) ResolvedReplicaSetSpec {
	spec := ResolvedReplicaSetSpec{
		Name:            src.Name,
		Image:           src.Image,
		Namespace:       src.Namespace,
		Resources:       shared.ResolveResources(src.Resources, reg),
		Labels:          make(map[string]string),
		Annotations:     make(map[string]string),
		EnvFrom:         src.EnvFrom,
		Probes:          src.Probes,
		Profiles:        reg,
		SecurityContext: shared.ResolveContainerSecurityContext(src.SecurityContext, reg),
		PodSecurity:     shared.ResolvePodSecurityContext(src.PodSecurity, reg),
		Volumes:         src.Volumes,
		VolumeMounts:    src.VolumeMounts,
		Sleep:           src.Sleep,
		ForceConflict:   src.ForceConflict,
	}

	if spec.Name == "" {
		spec.Name = ownerName + "-replicaset"
	}

	spec.Replicas = shared.ParseReplicas(src.Replicas)
	spec.HasAutoscale = src.Autoscale != nil

	if src.Port != "" {
		if p, err := strconv.ParseInt(src.Port, 10, 32); err == nil {
			spec.Port = int32(p)
		}
	}
	spec.Protocol = shared.ParseProtocol(src.Protocol)

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}
	for k, v := range src.Annotations {
		spec.Annotations[k] = v
	}
	for _, a := range src.NodeSelector {
		spec.NodeSelector[a] = a
	}

	spec.Env = []orktypes.EnvVar(src.Env)

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

func buildReplicaSet(owner domain.Object, spec ResolvedReplicaSetSpec, namespace string) *appsv1.ReplicaSet {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	logger.Debug().
		Interface("env", spec.Env).
		Interface("envFrom", spec.EnvFrom).
		Msg("replicaset.buildReplicaSet")

	replicas := spec.Replicas
	var pullSecrets []corev1.LocalObjectReference
	for _, name := range spec.ImagePullSecrets {
		pullSecrets = append(pullSecrets, corev1.LocalObjectReference{
			Name: name,
		})
	}

	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			Annotations:     spec.Annotations,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"orkestra-owner": owner.GetName(),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: spec.Labels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets:   pullSecrets,
					ServiceAccountName: spec.ServiceAccountName,
					NodeSelector:       spec.NodeSelector,
					Containers: []corev1.Container{
						{
							Name:  spec.Name,
							Image: spec.Image,
						},
					},
				},
			},
		},
	}

	if spec.Port > 0 {
		rs.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
			{ContainerPort: spec.Port, Protocol: spec.Protocol},
		}
	}

	if spec.Resources != nil {
		rs.Spec.Template.Spec.Containers[0].Resources = shared.BuildResourceRequirements(spec.Resources)
	}

	shared.ApplyProbes(&rs.Spec.Template.Spec.Containers[0], spec.Probes, spec.Port, spec.Profiles)

	// Security
	shared.ApplySecurityContext(&rs.Spec.Template.Spec.Containers[0], &rs.Spec.Template.Spec, spec.SecurityContext, spec.PodSecurity)

	if len(spec.Env) > 0 {
		rs.Spec.Template.Spec.Containers[0].Env = make([]corev1.EnvVar, 0, len(spec.Env))
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
			rs.Spec.Template.Spec.Containers[0].Env = append(rs.Spec.Template.Spec.Containers[0].Env, kev)
		}
	}

	envFrom, extraEnv := shared.ExpandEnvFrom(spec.EnvFrom)
	rs.Spec.Template.Spec.Containers[0].EnvFrom = envFrom
	rs.Spec.Template.Spec.Containers[0].Env = append(rs.Spec.Template.Spec.Containers[0].Env, extraEnv...)

	// Volumes / VolumeMounts
	if vols := shared.BuildVolumes(spec.Volumes); len(vols) > 0 {
		rs.Spec.Template.Spec.Volumes = vols
	}
	if mounts := shared.BuildVolumeMounts(spec.VolumeMounts); len(mounts) > 0 {
		rs.Spec.Template.Spec.Containers[0].VolumeMounts = mounts
	}

	return rs
}

func validateSpec(spec ResolvedReplicaSetSpec) error {
	var missing []string
	if spec.Name == "" {
		missing = append(missing, "name")
	}
	if spec.Image == "" {
		missing = append(missing, "image")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %v", missing)
	}
	return nil
}
