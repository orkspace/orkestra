// pkg/resources/hpas/types.go
package hpas

import (
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ResolvedHPASpec is the fully resolved HorizontalPodAutoscaler specification.
// All template expressions have been evaluated before this struct is populated.
// Passed directly to Create, Update, and Delete.
type ResolvedHPASpec struct {
	// Name — HPA resource name. Required.
	Name string

	// Namespace — target namespace.
	Namespace string

	// ScaleTargetRef — the target workload this HPA scales.
	// Supports Deployment, ReplicaSet, StatefulSet, or any scalable resource.
	ScaleTargetRef orktypes.ScaleTargetRef

	// MinReplicas — minimum pod replica count. Default: 1.
	MinReplicas int32

	// MaxReplicas — maximum pod replica count. Required.
	MaxReplicas int32

	// TargetCPUUtilizationPercentage — CPU utilization target (0-100).
	// When 0, no CPU metric is configured.
	TargetCPUUtilizationPercentage int32

	// Labels applied to HPA metadata.
	// Orkestra always adds: managed-by=orkestra, orkestra-owner=<cr-name>
	Labels map[string]string

	// Behavior — fully resolved scaling behavior. nil means use Kubernetes defaults.
	// When behavior.profile was declared, it is expanded here at resolve time.
	Behavior *orktypes.HPABehavior

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}
