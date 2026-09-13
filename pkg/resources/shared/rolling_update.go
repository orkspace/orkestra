// pkg/resources/shared/rolling_update.go
package shared

import (
	"strconv"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// BuildDeploymentRollingUpdateStrategy converts a RollingUpdateBehavior into an
// appsv1.DeploymentStrategy. Both MaxSurge and MaxUnavailable are applied.
func BuildDeploymentRollingUpdateStrategy(r *orktypes.RollingUpdateBehavior) appsv1.DeploymentStrategy {
	strategy := appsv1.DeploymentStrategy{
		Type:          appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{},
	}
	if r.MaxSurge != "" {
		v := ParseIntOrPercent(r.MaxSurge)
		strategy.RollingUpdate.MaxSurge = &v
	}
	if r.MaxUnavailable != "" {
		v := ParseIntOrPercent(r.MaxUnavailable)
		strategy.RollingUpdate.MaxUnavailable = &v
	}
	return strategy
}

// BuildStatefulSetUpdateStrategy converts a RollingUpdateBehavior into an
// appsv1.StatefulSetUpdateStrategy. Only MaxUnavailable applies — StatefulSets
// do not support MaxSurge.
func BuildStatefulSetUpdateStrategy(r *orktypes.RollingUpdateBehavior) appsv1.StatefulSetUpdateStrategy {
	strategy := appsv1.StatefulSetUpdateStrategy{
		Type:          appsv1.RollingUpdateStatefulSetStrategyType,
		RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{},
	}
	if r.MaxUnavailable != "" {
		v := ParseIntOrPercent(r.MaxUnavailable)
		strategy.RollingUpdate.MaxUnavailable = &v
	}
	return strategy
}

// ParseIntOrPercent converts a string to intstr.IntOrString.
// Strings ending in "%" are treated as percentage strings; others as integers.
func ParseIntOrPercent(s string) intstr.IntOrString {
	if strings.HasSuffix(s, "%") {
		return intstr.FromString(s)
	}
	if n, err := strconv.Atoi(s); err == nil {
		return intstr.FromInt32(int32(n))
	}
	return intstr.FromString(s)
}
