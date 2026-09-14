// pkg/resources/hpas/hpa.go
package hpas

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
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// Create creates an HPA owned by the CR if it does not already exist.
// Idempotent — if the HPA exists, does nothing and returns nil.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedHPASpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("hpa.Create: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("hpa.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("hpa", spec.Name).
			Str("namespace", namespace).
			Msg("hpa already exists — skipping create")
		return nil
	}

	hpa := buildHPA(owner, spec, namespace)

	_, err = kube.Clientset().AutoscalingV2().HorizontalPodAutoscalers(namespace).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("hpa.Create: creating hpa %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("hpa", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("hpa created")

	return nil
}

// Apply creates or updates an HPA using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedHPASpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("hpa.Apply: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	hpa := buildHPA(owner, spec, namespace)
	hpa.TypeMeta = metav1.TypeMeta{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler"}

	body, err := json.Marshal(hpa)
	if err != nil {
		return fmt.Errorf("hpa.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().AutoscalingV2().HorizontalPodAutoscalers(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("hpa.Apply: %w", err)
	}

	logger.Debug().
		Str("hpa", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("hpa applied")

	return nil
}

// Update applies the HPA via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedHPASpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the HPA if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedHPASpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug().
				Str("hpa", spec.Name).
				Str("namespace", namespace).
				Msg("hpa already deleted — skipping")
			return nil
		}
		return fmt.Errorf("hpa.Delete: deleting hpa %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("hpa", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("hpa deleted")

	return nil
}

// DeleteIfOwned deletes the HPA if it exists and is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().AutoscalingV2().HorizontalPodAutoscalers(namespace).
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
	return kube.Clientset().AutoscalingV2().HorizontalPodAutoscalers(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedHPASpec from an HPATemplateSource.
// All template expressions must be evaluated before calling here.
func Resolve(src orktypes.HPATemplateSource, ownerName string, reg orktypes.ProfileRegistry) ResolvedHPASpec {
	spec := ResolvedHPASpec{
		Name:           src.Name,
		Namespace:      src.Namespace,
		ScaleTargetRef: src.ScaleTargetRef,
		MinReplicas:    1,
		MaxReplicas:    1,
		Labels:         make(map[string]string),
		Sleep:          src.Sleep,
		ForceConflict:  src.ForceConflict,
	}

	if spec.Name == "" {
		spec.Name = ownerName + "-hpa"
	}
	if spec.ScaleTargetRef.Name == "" {
		spec.ScaleTargetRef.Name = ownerName
	}

	if src.MinReplicas != "" {
		if v, err := strconv.ParseInt(src.MinReplicas, 10, 32); err == nil {
			spec.MinReplicas = int32(v)
		}
	}
	if src.MaxReplicas != "" {
		if v, err := strconv.ParseInt(src.MaxReplicas, 10, 32); err == nil {
			spec.MaxReplicas = int32(v)
		}
	}
	if src.TargetCPUUtilizationPercentage != "" {
		if v, err := strconv.ParseInt(src.TargetCPUUtilizationPercentage, 10, 32); err == nil {
			spec.TargetCPUUtilizationPercentage = int32(v)
		}
	}

	if src.Behavior != nil && src.Behavior.Profile != "" {
		expansion, err := profiles.ApplyHPAProfile(src.Behavior.Profile, reg)
		if err != nil {
			logger.Warn().Str("profile", src.Behavior.Profile).Err(err).Msg("unknown hpa behavior profile — skipping")
		} else {
			spec.Behavior = &expansion.Behavior
			if spec.TargetCPUUtilizationPercentage == 0 {
				spec.TargetCPUUtilizationPercentage = expansion.CPUTarget
			}
		}
	} else if src.Behavior != nil {
		b := *src.Behavior
		spec.Behavior = &b
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	// System labels

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildHPA(owner domain.Object, spec ResolvedHPASpec, namespace string) *autoscalingv2.HorizontalPodAutoscaler {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	minR := spec.MinReplicas

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: spec.ScaleTargetRef.APIVersion,
				Kind:       spec.ScaleTargetRef.Kind,
				Name:       spec.ScaleTargetRef.Name,
			},
			MinReplicas: &minR,
			MaxReplicas: spec.MaxReplicas,
		},
	}

	if spec.TargetCPUUtilizationPercentage > 0 {
		cpuTarget := spec.TargetCPUUtilizationPercentage
		hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
			{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: &cpuTarget,
					},
				},
			},
		}
	}

	if spec.Behavior != nil {
		hpa.Spec.Behavior = buildK8sBehavior(spec.Behavior)
	}

	return hpa
}

func buildK8sBehavior(b *orktypes.HPABehavior) *autoscalingv2.HorizontalPodAutoscalerBehavior {
	k8s := &autoscalingv2.HorizontalPodAutoscalerBehavior{}

	if b.ScaleUp != nil {
		k8s.ScaleUp = buildK8sScalingRules(b.ScaleUp)
	}
	if b.ScaleDown != nil {
		k8s.ScaleDown = buildK8sScalingRules(b.ScaleDown)
	}
	return k8s
}

func buildK8sScalingRules(r *orktypes.HPAScalingRules) *autoscalingv2.HPAScalingRules {
	k8s := &autoscalingv2.HPAScalingRules{}

	sw := r.StabilizationWindowSeconds
	k8s.StabilizationWindowSeconds = &sw

	if r.SelectPolicy != "" {
		sp := autoscalingv2.ScalingPolicySelect(r.SelectPolicy)
		k8s.SelectPolicy = &sp
	}

	for _, p := range r.Policies {
		k8s.Policies = append(k8s.Policies, autoscalingv2.HPAScalingPolicy{
			Type:          autoscalingv2.HPAScalingPolicyType(p.Type),
			Value:         p.Value,
			PeriodSeconds: p.PeriodSeconds,
		})
	}
	return k8s
}

func behaviorEqual(a, b *autoscalingv2.HorizontalPodAutoscalerBehavior) bool {
	if a == nil && b == nil {
		return true
	}
	if b == nil {
		// No desired behavior — nothing to enforce, not a drift.
		return true
	}
	if a == nil {
		return false
	}
	// Only compare a side if we have an explicit desired spec for it.
	// Kubernetes injects defaults for the unset side; ignore those.
	if b.ScaleUp != nil && !scalingRulesEqual(a.ScaleUp, b.ScaleUp) {
		return false
	}
	if b.ScaleDown != nil && !scalingRulesEqual(a.ScaleDown, b.ScaleDown) {
		return false
	}
	return true
}

func scalingRulesEqual(a, b *autoscalingv2.HPAScalingRules) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	swA := int32(0)
	if a.StabilizationWindowSeconds != nil {
		swA = *a.StabilizationWindowSeconds
	}
	swB := int32(0)
	if b.StabilizationWindowSeconds != nil {
		swB = *b.StabilizationWindowSeconds
	}
	if swA != swB {
		return false
	}
	// Only compare SelectPolicy when we explicitly set one — Kubernetes injects
	// a default ("Max") when the field is omitted, which would otherwise loop.
	if b.SelectPolicy != nil {
		spA := autoscalingv2.ScalingPolicySelect("")
		if a.SelectPolicy != nil {
			spA = *a.SelectPolicy
		}
		if spA != *b.SelectPolicy {
			return false
		}
	}
	// Only compare Policies when we declared any — same default-injection risk.
	if len(b.Policies) > 0 {
		if len(a.Policies) != len(b.Policies) {
			return false
		}
		for i := range b.Policies {
			if a.Policies[i] != b.Policies[i] {
				return false
			}
		}
	}
	return true
}

func validateSpec(spec ResolvedHPASpec) error {
	if spec.Name == "" {
		return fmt.Errorf("missing required field: name")
	}
	return nil
}
