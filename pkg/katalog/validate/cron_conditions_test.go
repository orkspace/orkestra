package validate

import (
	"strings"
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

func katalogWithCronCondition(cron string, duration orktypes.Duration) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		"myresource": {
			OperatorBox: orktypes.OperatorBoxConfig{
				OnReconcile: &orktypes.HookTemplates{
					Deployments: []orktypes.DeploymentTemplateSource{
						{
							Name: "my-app",
							Conditions: []orktypes.Condition{
								{Cron: cron, Duration: duration},
							},
						},
					},
				},
			},
		},
	})
}

func TestCronConditionWarnings_NoDuration(t *testing.T) {
	k := katalogWithCronCondition("0 9 * * 1", orktypes.Duration{})
	warnings := k.k.CronConditionWarnings()
	if len(warnings) == 0 {
		t.Fatal("expected warning for cron without duration")
	}
	if !strings.Contains(warnings[0], "0 9 * * 1") {
		t.Errorf("warning should contain the cron expression, got: %s", warnings[0])
	}
}

func TestCronConditionWarnings_WithDuration(t *testing.T) {
	k := katalogWithCronCondition("0 9 * * 1", orktypes.Duration{Duration: 4 * 3600 * 1000000000}) // 4h
	warnings := k.k.CronConditionWarnings()
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings when duration is set, got: %v", warnings)
	}
}

func TestCronConditionWarnings_NoCronCondition(t *testing.T) {
	k := katalogWithCronCondition("", orktypes.Duration{})
	warnings := k.k.CronConditionWarnings()
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings when no cron condition, got: %v", warnings)
	}
}
