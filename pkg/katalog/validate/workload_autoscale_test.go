package validate

import (
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"testing"
)

func ptr32(i int32) *int32 { return &i }

func TestValidateWorkloadAutoscaleSpec_MaxRequired(t *testing.T) {
	err := validateWorkloadAutoscaleSpec("mydb", "app", &orktypes.WorkloadAutoscale{Max: 0})
	if err == nil {
		t.Fatal("expected error when max is 0")
	}
}

func TestValidateWorkloadAutoscaleSpec_MinLtMax(t *testing.T) {
	err := validateWorkloadAutoscaleSpec("mydb", "app", &orktypes.WorkloadAutoscale{
		Min: ptr32(10),
		Max: 5,
	})
	if err == nil {
		t.Fatal("expected error when min >= max")
	}
}

func TestValidateWorkloadAutoscaleSpec_MinEqualsMax(t *testing.T) {
	err := validateWorkloadAutoscaleSpec("mydb", "app", &orktypes.WorkloadAutoscale{
		Min: ptr32(5),
		Max: 5,
	})
	if err == nil {
		t.Fatal("expected error when min == max")
	}
}

func TestValidateWorkloadAutoscaleSpec_Valid(t *testing.T) {
	err := validateWorkloadAutoscaleSpec("mydb", "app", &orktypes.WorkloadAutoscale{
		Min: ptr32(2),
		Max: 10,
		ScaleUp: &orktypes.WorkloadScaleDirection{
			Target: ptr32(8),
		},
		ScaleDown: &orktypes.WorkloadScaleDirection{
			Target: ptr32(2),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateScaleDirection_TargetAndIncrementMutuallyExclusive(t *testing.T) {
	err := validateScaleDirection("scaleUp", &orktypes.WorkloadScaleDirection{
		Target:    ptr32(8),
		Increment: ptr32(2),
	})
	if err == nil {
		t.Fatal("expected error when target and increment both set")
	}
}

func TestValidateScaleDirection_TargetAndDecrementMutuallyExclusive(t *testing.T) {
	err := validateScaleDirection("scaleDown", &orktypes.WorkloadScaleDirection{
		Target:    ptr32(2),
		Decrement: ptr32(1),
	})
	if err == nil {
		t.Fatal("expected error when target and decrement both set")
	}
}

func TestValidateScaleDirection_NilIsValid(t *testing.T) {
	if err := validateScaleDirection("scaleUp", nil); err != nil {
		t.Fatalf("nil direction should be valid: %v", err)
	}
}

func TestValidateScaleDirection_ScaleUpTargetExceedsMax(t *testing.T) {
	err := validateScaleDirection("scaleUp", &orktypes.WorkloadScaleDirection{
		Target: ptr32(80),
	}, 8)
	if err == nil {
		t.Fatal("expected error when scaleUp target exceeds max")
	}
}

func TestValidateScaleDirection_ScaleDownTargetExceedsMax(t *testing.T) {
	err := validateScaleDirection("scaleDown", &orktypes.WorkloadScaleDirection{
		Target: ptr32(20),
	}, 8)
	if err == nil {
		t.Fatal("expected error when scaleDown target exceeds max")
	}
}

func TestValidateScaleDirection_NegativeTarget(t *testing.T) {
	err := validateScaleDirection("scaleDown", &orktypes.WorkloadScaleDirection{
		Target: ptr32(-1),
	}, 8)
	if err == nil {
		t.Fatal("expected error when target is negative")
	}
}

func TestValidateWorkloadAutoscaleSpec_ScaleUpTargetExceedsMax(t *testing.T) {
	err := validateWorkloadAutoscaleSpec("mydb", "app", &orktypes.WorkloadAutoscale{
		Max: 8,
		ScaleUp: &orktypes.WorkloadScaleDirection{
			Target: ptr32(80),
		},
	})
	if err == nil {
		t.Fatal("expected error when scaleUp target exceeds max")
	}
}

func TestValidateWorkloadAutoscaleSpec_ScaleDownTargetExceedsMax(t *testing.T) {
	err := validateWorkloadAutoscaleSpec("mydb", "app", &orktypes.WorkloadAutoscale{
		Max: 8,
		ScaleDown: &orktypes.WorkloadScaleDirection{
			Target: ptr32(20),
		},
	})
	if err == nil {
		t.Fatal("expected error when scaleDown target exceeds max")
	}
}

func TestValidateAutoscaleHPAConflict_Detected(t *testing.T) {
	hpas := []orktypes.HPATemplateSource{
		{
			Name:           "{{ .metadata.name }}-hpa",
			ScaleTargetRef: orktypes.ScaleTargetRef{Kind: "Deployment", Name: "{{ .metadata.name }}"},
		},
	}
	err := validateAutoscaleHPAConflict("mydb", "{{ .metadata.name }}", hpas)
	if err == nil {
		t.Fatal("expected error when autoscale conflicts with sibling HPA")
	}
}

func TestValidateAutoscaleHPAConflict_DifferentTarget(t *testing.T) {
	hpas := []orktypes.HPATemplateSource{
		{
			Name:           "other-hpa",
			ScaleTargetRef: orktypes.ScaleTargetRef{Kind: "Deployment", Name: "other-deployment"},
		},
	}
	err := validateAutoscaleHPAConflict("mydb", "{{ .metadata.name }}", hpas)
	if err != nil {
		t.Fatalf("unexpected error for non-conflicting HPA: %v", err)
	}
}

func TestValidateAutoscaleHPAConflict_StatefulSetKindDetected(t *testing.T) {
	hpas := []orktypes.HPATemplateSource{
		{
			Name:           "sts-hpa",
			ScaleTargetRef: orktypes.ScaleTargetRef{Kind: "StatefulSet", Name: "{{ .metadata.name }}"},
		},
	}
	err := validateAutoscaleHPAConflict("mydb", "{{ .metadata.name }}", hpas)
	if err == nil {
		t.Fatal("expected error: StatefulSet HPA conflicts with autoscale: on the same workload name")
	}
}

func TestValidateAutoscaleHPAConflict_UnrelatedKindIgnored(t *testing.T) {
	hpas := []orktypes.HPATemplateSource{
		{
			Name:           "custom-hpa",
			ScaleTargetRef: orktypes.ScaleTargetRef{Kind: "CustomWorkload", Name: "{{ .metadata.name }}"},
		},
	}
	err := validateAutoscaleHPAConflict("mydb", "{{ .metadata.name }}", hpas)
	if err != nil {
		t.Fatalf("unexpected error: unknown kind should not be flagged: %v", err)
	}
}

// TestValidateWorkloadAutoscale_OnReconcileOnly ensures the method does not
// panic when a CRD has onReconcile but no onCreate block (nil OnCreate).
func TestValidateWorkloadAutoscale_OnReconcileOnly(t *testing.T) {
	autoscale := &orktypes.WorkloadAutoscale{
		Max: 10,
		ScaleUp: &orktypes.WorkloadScaleDirection{
			Increment: ptr32(2),
		},
	}

	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"workerpool": {
			OperatorBox: orktypes.OperatorBoxConfig{
				OnReconcile: &orktypes.HookTemplates{
					Deployments: []orktypes.DeploymentTemplateSource{
						{Name: "my-pool", Autoscale: autoscale},
					},
				},
			},
		},
	})

	if err := k.validateWorkloadAutoscale(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
