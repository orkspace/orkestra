package runners

import (
	"context"
	"sync"
	"time"

	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkloadKind identifies the resource type the autoscaler targets.
type WorkloadKind string

const (
	WorkloadKindDeployment  WorkloadKind = "Deployment"
	WorkloadKindStatefulSet WorkloadKind = "StatefulSet"
	WorkloadKindReplicaSet  WorkloadKind = "ReplicaSet"
)

// cooldownState tracks the last scale event time per resource.
// Keyed by "namespace/crName/resourceName".
var cooldownState sync.Map

func cooldownKey(ns, crName, name string) string {
	return ns + "/" + crName + "/" + name
}

// EvaluateWorkloadAutoscaleDeployment checks scale-up and scale-down conditions for one
// Deployment and patches spec.replicas when conditions pass and cooldown has elapsed.
// Called on every reconcile — the resync period is the evaluation tick.
func EvaluateWorkloadAutoscaleDeployment(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	crName, ns, resourceName string,
	cfg *orktypes.WorkloadAutoscale,
) error {
	return evaluateWorkloadAutoscaleForKind(ctx, kube, resolver, crName, ns, resourceName, cfg, WorkloadKindDeployment)
}

// EvaluateWorkloadAutoscaleStatefulSet is EvaluateWorkloadAutoscale for StatefulSets.
func EvaluateWorkloadAutoscaleStatefulSet(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	crName, ns, resourceName string,
	cfg *orktypes.WorkloadAutoscale,
) error {
	return evaluateWorkloadAutoscaleForKind(ctx, kube, resolver, crName, ns, resourceName, cfg, WorkloadKindStatefulSet)
}

// EvaluateWorkloadAutoscaleReplicaSet is EvaluateWorkloadAutoscale for ReplicaSets.
func EvaluateWorkloadAutoscaleReplicaSet(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	crName, ns, resourceName string,
	cfg *orktypes.WorkloadAutoscale,
) error {
	return evaluateWorkloadAutoscaleForKind(ctx, kube, resolver, crName, ns, resourceName, cfg, WorkloadKindReplicaSet)
}

func evaluateWorkloadAutoscaleForKind(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	crName, ns, resourceName string,
	cfg *orktypes.WorkloadAutoscale,
	kind WorkloadKind,
) error {
	if cfg == nil {
		return nil
	}

	log := logger.FromContext(ctx).With().
		Str("resource", string(kind)+"WorkloadAutoscale").
		Str("name", resourceName).
		Str("namespace", ns).
		Logger()

	// ── Cooldown check ────────────────────────────────────────────────────────
	// sync.Map is the fast path; the annotation on the workload is the durable
	// fallback that survives restarts. Both are checked; the most recent wins.
	key := cooldownKey(ns, crName, resourceName)
	cooldown := cfg.EffectiveCooldown().Duration

	var lastScale time.Time
	if v, ok := cooldownState.Load(key); ok {
		if t, ok := v.(time.Time); ok {
			lastScale = t
		}
	}
	if annotationTime, err := readCooldownAnnotation(ctx, kube, ns, resourceName, kind); err == nil {
		if annotationTime.After(lastScale) {
			lastScale = annotationTime
		}
	}
	if !lastScale.IsZero() && time.Since(lastScale) < cooldown {
		log.Debug().Dur("remaining", cooldown-time.Since(lastScale)).Msg("autoscale: in cooldown")
		return nil
	}

	// ── Fetch current replicas ────────────────────────────────────────────────
	current, err := getCurrentReplicas(ctx, kube, ns, resourceName, kind)
	if err != nil {
		return nil // resource may not exist yet — skip silently
	}

	// ── Resolve min ──────────────────────────────────────────────────────────
	min := current // default floor is the current declared value
	if cfg.Min != nil {
		min = *cfg.Min
	}
	max := cfg.Max

	data := resolver.Data()
	eval := resolver.TemplateEvaluator()

	// ── Evaluate scale-up ────────────────────────────────────────────────────
	if cfg.ScaleUp != nil && current < max {
		c := cfg.ScaleUp
		if orktypes.EvaluateConditions(data, c.Conditions.When, c.Conditions.Or, eval) {
			target := resolveTarget(current, max, c, true)
			if target != current {
				log.Info().Int32("from", current).Int32("to", target).Msg("autoscale: scaling up")
				if err := patchReplicas(ctx, kube, ns, resourceName, target, kind); err != nil {
					return err
				}
				now := time.Now()
				cooldownState.Store(key, now)
				_ = writeCooldownAnnotation(ctx, kube, ns, resourceName, kind, now)
				return nil
			}
		}
	}

	// ── Evaluate scale-down ──────────────────────────────────────────────────
	if cfg.ScaleDown != nil && current > min {
		c := cfg.ScaleDown
		if orktypes.EvaluateConditions(data, c.Conditions.When, c.Conditions.Or, eval) {
			target := resolveTarget(current, min, c, false)
			if target != current {
				log.Info().Int32("from", current).Int32("to", target).Msg("autoscale: scaling down")
				if err := patchReplicas(ctx, kube, ns, resourceName, target, kind); err != nil {
					return err
				}
				now := time.Now()
				cooldownState.Store(key, now)
				_ = writeCooldownAnnotation(ctx, kube, ns, resourceName, kind, now)
			}
		}
	}

	return nil
}

// resolveTarget computes the desired replica count from a scale direction.
// scaleUp=true: clamps to max. scaleUp=false: clamps to min (floor).
func resolveTarget(current, bound int32, dir *orktypes.WorkloadScaleDirection, scaleUp bool) int32 {
	if dir.Target != nil {
		t := *dir.Target
		if scaleUp && t > bound {
			return bound
		}
		if !scaleUp && t < bound {
			return bound
		}
		return t
	}
	if scaleUp && dir.Increment != nil {
		t := current + *dir.Increment
		if t > bound {
			return bound
		}
		return t
	}
	if !scaleUp && dir.Decrement != nil {
		t := current - *dir.Decrement
		if t < bound {
			return bound
		}
		return t
	}
	return current
}

const cooldownAnnotation = "orkestra.orkspace.io/last-scale-event"

// readCooldownAnnotation reads the last scale event time from the workload annotation.
// Returns zero time if the annotation is absent or unparseable.
func readCooldownAnnotation(ctx context.Context, kube kubeclient.Interface, ns, name string, kind WorkloadKind) (time.Time, error) {
	var annotations map[string]string
	switch kind {
	case WorkloadKindStatefulSet:
		obj, err := kube.Clientset().AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return time.Time{}, err
		}
		annotations = obj.Annotations
	case WorkloadKindReplicaSet:
		obj, err := kube.Clientset().AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return time.Time{}, err
		}
		annotations = obj.Annotations
	default:
		obj, err := kube.Clientset().AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return time.Time{}, err
		}
		annotations = obj.Annotations
	}
	v, ok := annotations[cooldownAnnotation]
	if !ok {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, v)
}

// writeCooldownAnnotation stamps the last scale event time onto the workload annotation.
// Errors are non-fatal — the sync.Map remains the authoritative in-process state.
func writeCooldownAnnotation(ctx context.Context, kube kubeclient.Interface, ns, name string, kind WorkloadKind, t time.Time) error {
	v := t.UTC().Format(time.RFC3339)
	switch kind {
	case WorkloadKindStatefulSet:
		obj, err := kube.Clientset().AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if obj.Annotations == nil {
			obj.Annotations = make(map[string]string)
		}
		obj.Annotations[cooldownAnnotation] = v
		_, err = kube.Clientset().AppsV1().StatefulSets(ns).Update(ctx, obj, metav1.UpdateOptions{})
		return err
	case WorkloadKindReplicaSet:
		obj, err := kube.Clientset().AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if obj.Annotations == nil {
			obj.Annotations = make(map[string]string)
		}
		obj.Annotations[cooldownAnnotation] = v
		_, err = kube.Clientset().AppsV1().ReplicaSets(ns).Update(ctx, obj, metav1.UpdateOptions{})
		return err
	default:
		obj, err := kube.Clientset().AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if obj.Annotations == nil {
			obj.Annotations = make(map[string]string)
		}
		obj.Annotations[cooldownAnnotation] = v
		_, err = kube.Clientset().AppsV1().Deployments(ns).Update(ctx, obj, metav1.UpdateOptions{})
		return err
	}
}

func getCurrentReplicas(ctx context.Context, kube kubeclient.Interface, ns, name string, kind WorkloadKind) (int32, error) {
	switch kind {
	case WorkloadKindStatefulSet:
		sts, err := kube.Clientset().AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return 0, err
		}
		if sts.Spec.Replicas != nil {
			return *sts.Spec.Replicas, nil
		}
		return 1, nil
	case WorkloadKindReplicaSet:
		rs, err := kube.Clientset().AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return 0, err
		}
		if rs.Spec.Replicas != nil {
			return *rs.Spec.Replicas, nil
		}
		return 1, nil
	default: // Deployment
		dep, err := kube.Clientset().AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return 0, err
		}
		if dep.Spec.Replicas != nil {
			return *dep.Spec.Replicas, nil
		}
		return 1, nil
	}
}

func patchReplicas(ctx context.Context, kube kubeclient.Interface, ns, name string, replicas int32, kind WorkloadKind) error {
	switch kind {
	case WorkloadKindStatefulSet:
		sts, err := kube.Clientset().AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		sts.Spec.Replicas = &replicas
		_, err = kube.Clientset().AppsV1().StatefulSets(ns).Update(ctx, sts, metav1.UpdateOptions{})
		return err
	case WorkloadKindReplicaSet:
		rs, err := kube.Clientset().AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		rs.Spec.Replicas = &replicas
		_, err = kube.Clientset().AppsV1().ReplicaSets(ns).Update(ctx, rs, metav1.UpdateOptions{})
		return err
	default: // Deployment
		dep, err := kube.Clientset().AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		dep.Spec.Replicas = &replicas
		_, err = kube.Clientset().AppsV1().Deployments(ns).Update(ctx, dep, metav1.UpdateOptions{})
		return err
	}
}
