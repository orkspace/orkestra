package katalog

import (
	"fmt"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// CronConditionWarnings returns warnings for any cron: condition declared without duration:.
func (k *Katalog) CronConditionWarnings() []string {
	var warnings []string
	for crdName, crd := range k.EnabledCRDs() {
		box := crd.OperatorBox
		for _, ht := range []*orktypes.HookTemplates{box.OnCreate, box.OnReconcile, box.OnDelete} {
			if ht == nil {
				continue
			}
			warnings = append(warnings, cronNoDurationWarnings(crdName, collectHookConditions(ht))...)
		}
	}
	return warnings
}

func collectHookConditions(ht *orktypes.HookTemplates) []orktypes.Condition {
	var out []orktypes.Condition
	add := func(when, or []orktypes.Condition) {
		out = append(out, when...)
		out = append(out, or...)
	}
	for _, r := range ht.Deployments {
		add(r.Conditions, r.Or)
	}
	for _, r := range ht.StatefulSets {
		add(r.Conditions, r.Or)
	}
	for _, r := range ht.ReplicaSets {
		add(r.Conditions, r.Or)
	}
	for _, r := range ht.Services {
		add(r.Conditions, r.Or)
	}
	for _, r := range ht.Jobs {
		add(r.Conditions, r.Or)
	}
	for _, r := range ht.CronJobs {
		add(r.Conditions, r.Or)
	}
	for _, r := range ht.ConfigMaps {
		add(r.Conditions, r.Or)
	}
	for _, r := range ht.Secrets {
		add(r.Conditions, r.Or)
	}
	for _, r := range ht.HorizontalPodAutoscalers {
		add(r.Conditions, r.Or)
	}
	return out
}

func cronNoDurationWarnings(crdName string, conds []orktypes.Condition) []string {
	var out []string
	for _, c := range conds {
		if c.Cron != "" && c.Duration.Duration == 0 {
			out = append(out, fmt.Sprintf(
				"crds.%s: cron %q has no duration: — window stays open until the next fire. Add duration: to close it sooner.",
				crdName, c.Cron,
			))
		}
	}
	return out
}
