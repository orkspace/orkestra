package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileRegistry_IsEmpty(t *testing.T) {
	assert.True(t, ProfileRegistry{}.Empty())
	assert.False(t, ProfileRegistry{
		NetworkPolicies: []NetworkPolicyProfileDef{{Name: "x"}},
	}.Empty())
}

func TestProfileRegistry_LookupNetworkPolicy(t *testing.T) {
	reg := ProfileRegistry{
		NetworkPolicies: []NetworkPolicyProfileDef{
			{Name: "allow-monitoring", PolicyTypes: []string{"Ingress"}},
		},
	}
	got, found := reg.LookupNetworkPolicy("allow-monitoring")
	require.True(t, found)
	assert.Equal(t, "allow-monitoring", got.Name)

	_, found = reg.LookupNetworkPolicy("missing")
	assert.False(t, found)
}

func TestProfileRegistry_LookupResourceQuota(t *testing.T) {
	reg := ProfileRegistry{
		ResourceQuotas: []ResourceQuotaProfileDef{
			{Name: "team-medium", Hard: map[string]string{"pods": "30"}},
		},
	}
	got, found := reg.LookupResourceQuota("team-medium")
	require.True(t, found)
	assert.Equal(t, "30", got.Hard["pods"])

	_, found = reg.LookupResourceQuota("missing")
	assert.False(t, found)
}

func TestProfileRegistry_LookupHPA(t *testing.T) {
	reg := ProfileRegistry{
		HPA: []HPAProfileDef{
			{Name: "aggressive-scale", MaxReplicas: "20"},
		},
	}
	got, found := reg.LookupHPA("aggressive-scale")
	require.True(t, found)
	assert.Equal(t, "20", got.MaxReplicas)

	_, found = reg.LookupHPA("missing")
	assert.False(t, found)
}

func TestProfileRegistry_LookupPDB(t *testing.T) {
	reg := ProfileRegistry{
		PDB: []PDBProfileDef{
			{Name: "strict", MinAvailable: "2"},
		},
	}
	got, found := reg.LookupPDB("strict")
	require.True(t, found)
	assert.Equal(t, "2", got.MinAvailable)
}

func TestProfileRegistry_LookupRollingUpdate(t *testing.T) {
	reg := ProfileRegistry{
		RollingUpdate: []RollingUpdateProfileDef{
			{Name: "canary", MaxSurge: "1", MaxUnavailable: "0"},
		},
	}
	got, found := reg.LookupRollingUpdate("canary")
	require.True(t, found)
	assert.Equal(t, "1", got.MaxSurge)
}

func TestProfileRegistry_Merge_NoConflict(t *testing.T) {
	base := ProfileRegistry{
		NetworkPolicies: []NetworkPolicyProfileDef{{Name: "base-policy"}},
	}
	other := ProfileRegistry{
		NetworkPolicies: []NetworkPolicyProfileDef{{Name: "motif-policy"}},
		ResourceQuotas:  []ResourceQuotaProfileDef{{Name: "motif-quota", Hard: map[string]string{"pods": "10"}}},
	}
	merged, err := base.Merge(other, "motif \"tenant-isolation\"")
	require.NoError(t, err)
	assert.Len(t, merged.NetworkPolicies, 2)
	assert.Len(t, merged.ResourceQuotas, 1)
}

func TestProfileRegistry_Merge_ConflictNetworkPolicy(t *testing.T) {
	base := ProfileRegistry{
		NetworkPolicies: []NetworkPolicyProfileDef{{Name: "shared-policy"}},
	}
	other := ProfileRegistry{
		NetworkPolicies: []NetworkPolicyProfileDef{{Name: "shared-policy"}},
	}
	_, err := base.Merge(other, "motif \"tenant-isolation\"")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "profile conflict")
	assert.Contains(t, err.Error(), "shared-policy")
}

func TestProfileRegistry_Merge_ConflictResourceQuota(t *testing.T) {
	base := ProfileRegistry{
		ResourceQuotas: []ResourceQuotaProfileDef{{Name: "shared", Hard: map[string]string{}}},
	}
	other := ProfileRegistry{
		ResourceQuotas: []ResourceQuotaProfileDef{{Name: "shared", Hard: map[string]string{}}},
	}
	_, err := base.Merge(other, "motif \"quotas\"")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resourceQuotas")
}

func TestProfileRegistry_Merge_SameNameDifferentClass_Allowed(t *testing.T) {
	// "medium" in resourceQuotas and "medium" in hpa are independent — no conflict.
	base := ProfileRegistry{
		ResourceQuotas: []ResourceQuotaProfileDef{{Name: "medium", Hard: map[string]string{}}},
	}
	other := ProfileRegistry{
		HPA: []HPAProfileDef{{Name: "medium", MaxReplicas: "10"}},
	}
	merged, err := base.Merge(other, "motif \"sizing\"")
	require.NoError(t, err)
	assert.Len(t, merged.ResourceQuotas, 1)
	assert.Len(t, merged.HPA, 1)
}

// ── Empty: each class ───────────────────────────────────────────────────────

func TestProfileRegistry_IsEmpty_EachClass(t *testing.T) {
	cases := []struct {
		name string
		reg  ProfileRegistry
	}{
		{"ResourceQuotas", ProfileRegistry{ResourceQuotas: []ResourceQuotaProfileDef{{Name: "x"}}}},
		{"LimitRanges", ProfileRegistry{LimitRanges: []LimitRangeProfileDef{{Name: "x"}}}},
		{"HPA", ProfileRegistry{HPA: []HPAProfileDef{{Name: "x"}}}},
		{"PDB", ProfileRegistry{PDB: []PDBProfileDef{{Name: "x"}}}},
		{"RollingUpdate", ProfileRegistry{RollingUpdate: []RollingUpdateProfileDef{{Name: "x"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.False(t, tc.reg.Empty())
		})
	}
}

// ── Lookup: multiple entries, returns correct one ─────────────────────────────

func TestProfileRegistry_LookupNetworkPolicy_MultipleEntries(t *testing.T) {
	reg := ProfileRegistry{
		NetworkPolicies: []NetworkPolicyProfileDef{
			{Name: "first", PolicyTypes: []string{"Ingress"}},
			{Name: "second", PolicyTypes: []string{"Egress"}},
			{Name: "third", PolicyTypes: []string{"Ingress", "Egress"}},
		},
	}
	got, found := reg.LookupNetworkPolicy("second")
	require.True(t, found)
	assert.Equal(t, []string{"Egress"}, got.PolicyTypes)
}

func TestProfileRegistry_LookupLimitRange(t *testing.T) {
	reg := ProfileRegistry{
		LimitRanges: []LimitRangeProfileDef{
			{
				Name: "default-limits",
				Limits: []LimitRangeItem{
					{Type: "Container", Default: map[string]string{"cpu": "500m", "memory": "256Mi"}},
				},
			},
		},
	}
	got, found := reg.LookupLimitRange("default-limits")
	require.True(t, found)
	assert.Len(t, got.Limits, 1)
	assert.Equal(t, "500m", got.Limits[0].Default["cpu"])

	_, found = reg.LookupLimitRange("missing")
	assert.False(t, found)
}

func TestProfileRegistry_LookupHPA_WithMinReplicas(t *testing.T) {
	reg := ProfileRegistry{
		HPA: []HPAProfileDef{
			{Name: "scaled", MinReplicas: "2", MaxReplicas: "{{ .spec.maxReplicas }}", TargetCPUUtilizationPercentage: "70"},
		},
	}
	got, found := reg.LookupHPA("scaled")
	require.True(t, found)
	assert.Equal(t, "2", got.MinReplicas)
	assert.Equal(t, "{{ .spec.maxReplicas }}", got.MaxReplicas)
	assert.Equal(t, "70", got.TargetCPUUtilizationPercentage)
}

func TestProfileRegistry_LookupPDB_MaxUnavailable(t *testing.T) {
	reg := ProfileRegistry{
		PDB: []PDBProfileDef{
			{Name: "relaxed", MaxUnavailable: "{{ .spec.maxUnavailable }}"},
		},
	}
	got, found := reg.LookupPDB("relaxed")
	require.True(t, found)
	assert.Equal(t, "{{ .spec.maxUnavailable }}", got.MaxUnavailable)
}

func TestProfileRegistry_LookupRollingUpdate_BothFields(t *testing.T) {
	reg := ProfileRegistry{
		RollingUpdate: []RollingUpdateProfileDef{
			{Name: "gradual", MaxSurge: "{{ .spec.surge }}", MaxUnavailable: "0"},
		},
	}
	got, found := reg.LookupRollingUpdate("gradual")
	require.True(t, found)
	assert.Equal(t, "{{ .spec.surge }}", got.MaxSurge)
	assert.Equal(t, "0", got.MaxUnavailable)
}

// ── Merge: conflict for every class ──────────────────────────────────────────

func TestProfileRegistry_Merge_ConflictHPA(t *testing.T) {
	base := ProfileRegistry{HPA: []HPAProfileDef{{Name: "clash"}}}
	other := ProfileRegistry{HPA: []HPAProfileDef{{Name: "clash"}}}
	_, err := base.Merge(other, "motif \"scaling\"")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hpa")
	assert.Contains(t, err.Error(), "clash")
}

func TestProfileRegistry_Merge_ConflictPDB(t *testing.T) {
	base := ProfileRegistry{PDB: []PDBProfileDef{{Name: "clash"}}}
	other := ProfileRegistry{PDB: []PDBProfileDef{{Name: "clash"}}}
	_, err := base.Merge(other, "motif \"disruption\"")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pdb")
}

func TestProfileRegistry_Merge_ConflictRollingUpdate(t *testing.T) {
	base := ProfileRegistry{RollingUpdate: []RollingUpdateProfileDef{{Name: "clash"}}}
	other := ProfileRegistry{RollingUpdate: []RollingUpdateProfileDef{{Name: "clash"}}}
	_, err := base.Merge(other, "motif \"rollout\"")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rollingUpdate")
}

func TestProfileRegistry_Merge_ConflictLimitRange(t *testing.T) {
	base := ProfileRegistry{LimitRanges: []LimitRangeProfileDef{{Name: "clash"}}}
	other := ProfileRegistry{LimitRanges: []LimitRangeProfileDef{{Name: "clash"}}}
	_, err := base.Merge(other, "motif \"limits\"")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limitRanges")
}

func TestProfileRegistry_Merge_EmptyBase(t *testing.T) {
	other := ProfileRegistry{
		NetworkPolicies: []NetworkPolicyProfileDef{{Name: "np"}},
		HPA:             []HPAProfileDef{{Name: "hpa", MaxReplicas: "5"}},
	}
	merged, err := ProfileRegistry{}.Merge(other, "motif \"full\"")
	require.NoError(t, err)
	assert.Len(t, merged.NetworkPolicies, 1)
	assert.Len(t, merged.HPA, 1)
}

func TestProfileRegistry_Merge_EmptyOther(t *testing.T) {
	base := ProfileRegistry{
		PDB: []PDBProfileDef{{Name: "strict", MinAvailable: "2"}},
	}
	merged, err := base.Merge(ProfileRegistry{}, "motif \"empty\"")
	require.NoError(t, err)
	assert.Len(t, merged.PDB, 1)
}

// ── NetworkPolicyProfileDef fields ───────────────────────────────────────────

func TestNetworkPolicyProfileDef_FullFields(t *testing.T) {
	def := NetworkPolicyProfileDef{
		Name:        "allow-monitoring",
		Description: "Allows ingress from the platform monitoring namespace",
		PodSelector: map[string]interface{}{"app": "my-app"},
		Ingress: []NetworkPolicyIngressRule{
			{From: []NetworkPolicyPeer{{NamespaceSelector: map[string]string{"team": "platform"}}}},
		},
		PolicyTypes: []string{"Ingress"},
	}
	assert.Equal(t, "allow-monitoring", def.Name)
	assert.Len(t, def.Ingress, 1)
	assert.Equal(t, []string{"Ingress"}, def.PolicyTypes)
}

// ── ResourceQuotaProfileDef: template expressions ────────────────────────────

func TestResourceQuotaProfileDef_TemplateExpressions(t *testing.T) {
	def := ResourceQuotaProfileDef{
		Name: "dynamic",
		Hard: map[string]string{
			"pods":   "{{ .spec.maxPods }}",
			"cpu":    "{{ .spec.cpuLimit }}",
			"memory": "{{ .spec.memLimit }}",
		},
	}
	reg := ProfileRegistry{ResourceQuotas: []ResourceQuotaProfileDef{def}}
	got, found := reg.LookupResourceQuota("dynamic")
	require.True(t, found)
	assert.Equal(t, "{{ .spec.maxPods }}", got.Hard["pods"])
}
