// pkg/resources/deployments/deployment.go
package deployments

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

// Create creates a Deployment owned by the CR if it does not already exist.
// Idempotent — if the Deployment exists, does nothing and returns nil.
// Sets owner reference so the Deployment is garbage collected when the CR is deleted.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedDeploymentSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("deployment.Create: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().AppsV1().Deployments(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("deployment.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("deployment", spec.Name).
			Str("namespace", namespace).
			Msg("deployment already exists — skipping create")
		return nil
	}

	deployment := buildDeployment(owner, spec, namespace)

	_, err = kube.Clientset().AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("deployment.Create: creating deployment %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("deployment", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("deployment created")

	return nil
}

// Apply creates or updates a Deployment using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedDeploymentSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("deployment.Apply: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	d := buildDeployment(owner, spec, namespace)
	d.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}

	body, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("deployment.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().AppsV1().Deployments(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("deployment.Apply: %w", err)
	}

	logger.Debug().
		Str("deployment", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Bool("force", *shared.ResolveForceConflict(kube, spec.ForceConflict)).
		Msg("deployment applied")

	return nil
}

// Update applies the Deployment via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedDeploymentSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the Deployment if it exists.
// For most cases owner references handle cascade deletion — use this only
// for explicit cleanup declared in onDelete templates.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedDeploymentSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().AppsV1().Deployments(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug().
				Str("deployment", spec.Name).
				Str("namespace", namespace).
				Msg("deployment already deleted — skipping")
			return nil
		}
		return fmt.Errorf("deployment.Delete: deleting deployment %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("deployment", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("deployment deleted")

	return nil
}

// DeleteIfOwned deletes the Deployment if it exists and is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().AppsV1().Deployments(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	// Only delete if we own it
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().AppsV1().Deployments(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedDeploymentSpec from a DeploymentTemplateSource.
// Fields with template expressions must already be evaluated before calling Resolve.
// Use pkg/orkestra-registry/template.Resolver to evaluate expressions first.
//
// The resolver already evaluated template expressions — here we just merge.
func Resolve(src orktypes.DeploymentTemplateSource, ownerName string, reg orktypes.ProfileRegistry) ResolvedDeploymentSpec {
	spec := ResolvedDeploymentSpec{
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
		spec.Name = ownerName + "-deployment"
	}

	spec.Replicas = shared.ParseReplicas(src.Replicas)
	spec.HasAutoscale = src.Autoscale != nil

	// Port — prefer dynamic resolved string, fall back to static int
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

	// Orkestra system labels — always added

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildDeployment(owner domain.Object, spec ResolvedDeploymentSpec, namespace string) *appsv1.Deployment {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	// Debug line
	logger.Debug().
		Interface("env", spec.Env).
		Interface("envFrom", spec.EnvFrom).
		Msg("deployment.buildDeployment")

	replicas := spec.Replicas

	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			Annotations:     spec.Annotations,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"orkestra-owner": owner.GetName(),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podTemplateLabels(spec.Labels, owner.GetName()),
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets:   shared.ToPullSecrets(spec.ImagePullSecrets),
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

	// Port — Protocol is resolved in Resolve() and defaults to TCP when not declared.
	if spec.Port > 0 {
		d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
			{ContainerPort: spec.Port, Protocol: spec.Protocol},
		}
	}

	// Resources
	if spec.Resources != nil {
		d.Spec.Template.Spec.Containers[0].Resources = shared.BuildResourceRequirements(spec.Resources)
	}

	// Probes
	shared.ApplyProbes(&d.Spec.Template.Spec.Containers[0], spec.Probes, spec.Port, spec.Profiles)

	// Rolling update strategy
	if spec.RollingUpdate != nil {
		d.Spec.Strategy = shared.BuildDeploymentRollingUpdateStrategy(spec.RollingUpdate)
	}

	// Security
	shared.ApplySecurityContext(&d.Spec.Template.Spec.Containers[0], &d.Spec.Template.Spec, spec.SecurityContext, spec.PodSecurity)

	// Env
	if len(spec.Env) > 0 {
		d.Spec.Template.Spec.Containers[0].Env = make([]corev1.EnvVar, 0, len(spec.Env))
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
			d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, kev)
		}
	}

	// EnvFrom
	envFrom, extraEnv := shared.ExpandEnvFrom(spec.EnvFrom)
	d.Spec.Template.Spec.Containers[0].EnvFrom = envFrom
	d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, extraEnv...)

	// Volumes / VolumeMounts
	if vols := shared.BuildVolumes(spec.Volumes); len(vols) > 0 {
		d.Spec.Template.Spec.Volumes = vols
	}
	if mounts := shared.BuildVolumeMounts(spec.VolumeMounts); len(mounts) > 0 {
		d.Spec.Template.Spec.Containers[0].VolumeMounts = mounts
	}

	return d
}

// podTemplateLabels merges user-supplied labels with the selector label so the
// pod template always satisfies the Deployment's matchLabels constraint.
func podTemplateLabels(userLabels map[string]string, ownerName string) map[string]string {
	out := make(map[string]string, len(userLabels)+1)
	for k, v := range userLabels {
		out[k] = v
	}
	out["orkestra-owner"] = ownerName
	return out
}

func validateSpec(spec ResolvedDeploymentSpec) error {
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
