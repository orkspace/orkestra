package validate

import (
	"fmt"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateWorkloadAutoscale checks autoscale: blocks on all Deployment declarations.
//
// Rules:
//  1. max is required.
//  2. min < max when both are declared.
//  3. target and increment/decrement are mutually exclusive per direction.
//  4. scaleUp without scaleDown is allowed but emits a warning.
//  5. autoscale: conflicts with a sibling HPA that targets the same deployment.
func (e *executor) validateWorkloadAutoscale() error {
	for crdName, crd := range e.k.EnabledCRDs() {
		box := crd.OperatorBox

		// Collect all HPAs across hooks so we can check for conflicts.
		var allHPAs []orktypes.HPATemplateSource
		if box.OnCreate != nil {
			allHPAs = append(allHPAs, box.OnCreate.HorizontalPodAutoscalers...)
		}
		if box.OnReconcile != nil {
			allHPAs = append(allHPAs, box.OnReconcile.HorizontalPodAutoscalers...)
		}

		type workloadEntry struct {
			name      string
			autoscale *orktypes.WorkloadAutoscale
		}
		var workloads []workloadEntry

		if box.OnCreate != nil {
			for _, d := range box.OnCreate.Deployments {
				workloads = append(workloads, workloadEntry{d.Name, d.Autoscale})
			}
			for _, s := range box.OnCreate.StatefulSets {
				workloads = append(workloads, workloadEntry{s.Name, s.Autoscale})
			}
			for _, r := range box.OnCreate.ReplicaSets {
				workloads = append(workloads, workloadEntry{r.Name, r.Autoscale})
			}
		}
		if box.OnReconcile != nil {
			for _, d := range box.OnReconcile.Deployments {
				workloads = append(workloads, workloadEntry{d.Name, d.Autoscale})
			}
			for _, s := range box.OnReconcile.StatefulSets {
				workloads = append(workloads, workloadEntry{s.Name, s.Autoscale})
			}
			for _, r := range box.OnReconcile.ReplicaSets {
				workloads = append(workloads, workloadEntry{r.Name, r.Autoscale})
			}
		}

		for _, w := range workloads {
			if w.autoscale == nil {
				continue
			}
			if err := validateWorkloadAutoscaleSpec(crdName, w.name, w.autoscale); err != nil {
				return err
			}
			if err := validateAutoscaleHPAConflict(crdName, w.name, allHPAs); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateAutoscaleHPAConflict returns an error if any HPA in the same CRD targets
// the same workload name template. Orkestra's autoscaler and a Kubernetes HPA both
// patch spec.replicas — running both on the same workload causes a control loop fight.
// The check matches on raw name template strings; expressions that resolve to the same
// value at runtime but differ textually are not caught (accepted limitation).
// Kind matching covers Deployment, StatefulSet, and ReplicaSet — the three scalable
// workload types that support autoscale:. An empty kind defaults to Deployment.
func validateAutoscaleHPAConflict(crdName, depName string, hpas []orktypes.HPATemplateSource) error {
	scalableKinds := map[string]bool{"": true, "Deployment": true, "StatefulSet": true, "ReplicaSet": true}
	for _, hpa := range hpas {
		ref := hpa.ScaleTargetRef
		if ref.Name != depName {
			continue
		}
		if !scalableKinds[ref.Kind] {
			continue
		}
		return fmt.Errorf(
			"%s crds.%s workload %q autoscale: conflicts with sibling HPA %q — remove autoscale: or remove the HPA; both control spec.replicas",
			failureMark(), crdName, depName, hpa.Name,
		)
	}
	return nil
}

// validateWorkloadAutoscaleSpec validates the autoscale: block on a single workload declaration.
func validateWorkloadAutoscaleSpec(crdName, depName string, cfg *orktypes.WorkloadAutoscale) error {
	loc := fmt.Sprintf("crds.%s workload %q autoscale", crdName, depName)

	if cfg.Max == 0 {
		return fmt.Errorf("%s %s: max is required and must be > 0", failureMark(), loc)
	}

	if cfg.Min != nil && *cfg.Min >= cfg.Max {
		return fmt.Errorf("%s %s: min (%d) must be less than max (%d)", failureMark(), loc, *cfg.Min, cfg.Max)
	}

	if err := validateScaleDirection(loc+".scaleUp", cfg.ScaleUp, cfg.Max); err != nil {
		return err
	}
	if err := validateScaleDirection(loc+".scaleDown", cfg.ScaleDown, cfg.Max); err != nil {
		return err
	}

	return nil
}

// validateScaleDirection checks one scale direction (scaleUp or scaleDown).
// max is optional — pass it to also validate that target stays within bounds.
func validateScaleDirection(loc string, dir *orktypes.WorkloadScaleDirection, max ...int32) error {
	if dir == nil {
		return nil
	}
	hasTarget := dir.Target != nil
	hasStep := dir.Increment != nil || dir.Decrement != nil
	if hasTarget && hasStep {
		return fmt.Errorf("%s %s: target and increment/decrement are mutually exclusive", failureMark(), loc)
	}
	if hasTarget && len(max) > 0 {
		t := *dir.Target
		if t > max[0] {
			return fmt.Errorf("%s %s: target (%d) exceeds max (%d)", failureMark(), loc, t, max[0])
		}
		if t < 0 {
			return fmt.Errorf("%s %s: target (%d) must be >= 0", failureMark(), loc, t)
		}
	}
	return nil
}
