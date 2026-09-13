// pkg/resources/cronjobs/cronjob.go
package cronjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// ResolvedCronJobSpec is the fully resolved CronJob specification.
// All template expressions have been evaluated before this struct is populated.
type ResolvedCronJobSpec struct {
	// Name — CronJob name. Required.
	Name string

	// Namespace — target namespace.
	Namespace string

	// Schedule — standard cron expression. Required.
	// e.g. "*/5 * * * *", "0 2 * * 1-5"
	Schedule string

	// Image — container image. Required.
	Image string

	// Command — container entrypoint override.
	Command []string

	// Args — container arguments.
	Args []string

	// Suspend — when true, all subsequent executions are suspended.
	// The currently running job is not affected.
	Suspend bool

	// ConcurrencyPolicy — how to treat concurrent executions.
	// One of: Allow (default), Forbid, Replace.
	ConcurrencyPolicy batchv1.ConcurrencyPolicy

	// StartingDeadlineSeconds — deadline for starting the job if it
	// misses its scheduled time. nil means no deadline.
	StartingDeadlineSeconds *int64

	// SuccessfulJobsHistoryLimit — number of successful finished jobs to keep.
	// nil means the Kubernetes default (3).
	SuccessfulJobsHistoryLimit *int32

	// FailedJobsHistoryLimit — number of failed finished jobs to keep.
	// nil means the Kubernetes default (1).
	FailedJobsHistoryLimit *int32

	// Labels — applied to CronJob and pod metadata.
	Labels map[string]string

	// Resources — CPU and memory requests/limits. nil means no limits set.
	Resources *orktypes.ResourceRequirements

	// ImagePullSecrets is an optional list of references to secrets in the same namespace to use
	// for pulling any of the images used by this PodSpec.
	// If specified, these secrets will be passed to individual puller implementations for them to use.
	ImagePullSecrets []string

	// SecurityContext — container-level security settings.
	SecurityContext *orktypes.ContainerSecurityContext

	// PodSecurity — pod-level security settings.
	PodSecurity *orktypes.PodSecurityContext

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}

// Create creates a CronJob if it does not already exist.
// Idempotent — skips creation if the CronJob already exists.
// Sets owner reference so the CronJob is garbage collected when the CR is deleted.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedCronJobSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("cronjob.Create: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().BatchV1().CronJobs(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("cronjob.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("cronjob", spec.Name).
			Str("namespace", namespace).
			Msg("cronjob already exists — skipping create")
		return nil
	}

	cj := buildCronJob(owner, spec, namespace)

	_, err = kube.Clientset().BatchV1().CronJobs(namespace).Create(ctx, cj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cronjob.Create: creating %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("cronjob", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("cronjob created")

	return nil
}

// Apply creates or updates a CronJob using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedCronJobSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("cronjob.Apply: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	cj := buildCronJob(owner, spec, namespace)
	cj.TypeMeta = metav1.TypeMeta{APIVersion: "batch/v1", Kind: "CronJob"}

	body, err := json.Marshal(cj)
	if err != nil {
		return fmt.Errorf("cronjob.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().BatchV1().CronJobs(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("cronjob.Apply: %w", err)
	}

	logger.Debug().
		Str("cronjob", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("cronjob applied")

	return nil
}

// Update applies the CronJob via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedCronJobSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the CronJob if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedCronJobSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().BatchV1().CronJobs(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug().
				Str("cronjob", spec.Name).
				Str("namespace", namespace).
				Msg("cronjob already deleted — skipping")
			return nil
		}
		return fmt.Errorf("cronjob.Delete: deleting %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("cronjob", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("cronjob deleted")

	return nil
}

// DeleteIfOwned deletes the CronJob only if it is labelled as owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().BatchV1().CronJobs(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("cronjob.DeleteIfOwned: getting %q: %w", name, err)
	}
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().BatchV1().CronJobs(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedCronJobSpec from a CronJobTemplateSource.
// All template expressions in src must already have been evaluated by
// template.Resolver — Resolve only performs type conversion and defaults.
func Resolve(src orktypes.CronJobTemplateSource, ownerName string, reg orktypes.ProfileRegistry) ResolvedCronJobSpec {
	spec := ResolvedCronJobSpec{
		Name:            src.Name,
		Namespace:       src.Namespace,
		Schedule:        src.Schedule,
		Image:           src.Image,
		Command:         src.Command,
		Args:            src.Args,
		Labels:          make(map[string]string),
		Resources:       shared.ResolveResources(src.Resources, reg),
		SecurityContext: shared.ResolveContainerSecurityContext(src.SecurityContext, reg),
		PodSecurity:     shared.ResolvePodSecurityContext(src.PodSecurity, reg),
		Sleep:           src.Sleep,
		ForceConflict:   src.ForceConflict,
	}

	if spec.Name == "" {
		spec.Name = ownerName + "-cronjob"
	}

	// ── Suspend ───────────────────────────────────────────────────────────
	if src.Suspend != "" {
		spec.Suspend = shared.ParseBool(src.Suspend)
	}

	// ── ConcurrencyPolicy ─────────────────────────────────────────────────
	switch strings.ToLower(src.ConcurrencyPolicy) {
	case "forbid":
		spec.ConcurrencyPolicy = batchv1.ForbidConcurrent
	case "replace":
		spec.ConcurrencyPolicy = batchv1.ReplaceConcurrent
	default:
		spec.ConcurrencyPolicy = batchv1.AllowConcurrent
	}

	// ── StartingDeadlineSeconds ───────────────────────────────────────────
	if src.StartingDeadlineSeconds != "" {
		if n, err := strconv.ParseInt(src.StartingDeadlineSeconds, 10, 64); err == nil && n > 0 {
			spec.StartingDeadlineSeconds = &n
		}
	}

	// ── SuccessfulJobsHistoryLimit ────────────────────────────────────────
	if src.SuccessfulJobsHistoryLimit != "" {
		if n, err := strconv.ParseInt(src.SuccessfulJobsHistoryLimit, 10, 32); err == nil {
			n32 := int32(n)
			spec.SuccessfulJobsHistoryLimit = &n32
		}
	} else {
		n := int32(3) // Kubernetes default
		spec.SuccessfulJobsHistoryLimit = &n
	}

	// ── FailedJobsHistoryLimit ────────────────────────────────────────────
	if src.FailedJobsHistoryLimit != "" {
		if n, err := strconv.ParseInt(src.FailedJobsHistoryLimit, 10, 32); err == nil {
			n32 := int32(n)
			spec.FailedJobsHistoryLimit = &n32
		}
	} else {
		n := int32(1) // Kubernetes default
		spec.FailedJobsHistoryLimit = &n
	}

	// ── Labels ────────────────────────────────────────────────────────────
	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildCronJob(owner domain.Object, spec ResolvedCronJobSpec, namespace string) *batchv1.CronJob {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   spec.Schedule,
			Suspend:                    utils.BoolPtr(spec.Suspend),
			ConcurrencyPolicy:          spec.ConcurrencyPolicy,
			StartingDeadlineSeconds:    spec.StartingDeadlineSeconds,
			SuccessfulJobsHistoryLimit: spec.SuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     spec.FailedJobsHistoryLimit,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: spec.Labels,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: spec.Labels,
						},
						Spec: corev1.PodSpec{
							ImagePullSecrets: shared.ToPullSecrets(spec.ImagePullSecrets),
							RestartPolicy:    corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{
								buildContainer(spec),
							},
						},
					},
				},
			},
		},
	}

	// Security
	shared.ApplySecurityContext(
		&cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0],
		&cj.Spec.JobTemplate.Spec.Template.Spec,
		spec.SecurityContext,
		spec.PodSecurity,
	)

	return cj
}

func buildContainer(spec ResolvedCronJobSpec) corev1.Container {
	c := corev1.Container{
		Name:    spec.Name,
		Image:   spec.Image,
		Command: spec.Command,
		Args:    spec.Args,
	}
	if spec.Resources != nil {
		c.Resources = shared.BuildResourceRequirements(spec.Resources)
	}
	return c
}

func validateSpec(spec ResolvedCronJobSpec) error {
	var missing []string
	if spec.Name == "" {
		missing = append(missing, "name")
	}
	if spec.Image == "" {
		missing = append(missing, "image")
	}
	if spec.Schedule == "" {
		missing = append(missing, "schedule")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %v", missing)
	}
	return nil
}
