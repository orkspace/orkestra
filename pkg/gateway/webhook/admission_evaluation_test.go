// Tests for the pure evaluation logic in admission_evaluation.go and
// the shared helper functions in admission.go.
package webhook

import (
	"context"
	"encoding/json"
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ── setFieldPath ──────────────────────────────────────────────────────────────

func TestSetFieldPath(t *testing.T) {
	t.Run("top-level field", func(t *testing.T) {
		obj := map[string]interface{}{}
		setFieldPath(obj, "name", "orkestra")
		assert.Equal(t, "orkestra", obj["name"])
	})

	t.Run("nested field creates intermediate map", func(t *testing.T) {
		obj := map[string]interface{}{}
		setFieldPath(obj, "spec.image", "myorg/app:latest")
		spec, ok := obj["spec"].(map[string]interface{})
		require.True(t, ok, "spec should be a map")
		assert.Equal(t, "myorg/app:latest", spec["image"])
	})

	t.Run("deeply nested creates all levels", func(t *testing.T) {
		obj := map[string]interface{}{}
		setFieldPath(obj, "spec.db.host", "localhost")
		spec := obj["spec"].(map[string]interface{})
		db := spec["db"].(map[string]interface{})
		assert.Equal(t, "localhost", db["host"])
	})

	t.Run("overwrites existing scalar", func(t *testing.T) {
		obj := map[string]interface{}{
			"spec": map[string]interface{}{"replicas": "1"},
		}
		setFieldPath(obj, "spec.replicas", "3")
		spec := obj["spec"].(map[string]interface{})
		assert.Equal(t, "3", spec["replicas"])
	})

	t.Run("sibling keys preserved", func(t *testing.T) {
		obj := map[string]interface{}{
			"spec": map[string]interface{}{"image": "nginx"},
		}
		setFieldPath(obj, "spec.replicas", "2")
		spec := obj["spec"].(map[string]interface{})
		assert.Equal(t, "nginx", spec["image"])
		assert.Equal(t, "2", spec["replicas"])
	})
}

func specObj(fields map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"spec": fields}
}

// ── evaluateValidationRules: multi-rule ───────────────────────────────────────

func TestEvaluateValidationRules_MixedDenyAndWarn(t *testing.T) {
	h := &WebhookServer{}

	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"image":    "docker.io/nginx:1.25", // violates deny rule
			"replicas": float64(15),            // violates deny rule
		},
		// metadata.labels.team absent → violates warn rule
	}
	cfg := &orktypes.ValidationConfig{
		Rules: []orktypes.ValidationRule{
			{Field: "spec.image", Prefix: "myorg/", Message: "wrong registry", Action: orktypes.ValidationActionDeny},
			{Field: "spec.replicas", Max: "10", Message: "too many replicas", Action: orktypes.ValidationActionDeny},
			{Field: "metadata.labels.team", Operator: orktypes.ConditionExists, Message: "team label required", Action: orktypes.ValidationActionWarn},
		},
	}

	denials, warnings := h.evaluateValidationRules(context.Background(), obj, cfg, "Website")

	assert.Len(t, denials, 2, "should collect all deny violations — not fail-fast")
	assert.Len(t, warnings, 1)
	assert.Equal(t, "spec.image", denials[0].Field)
	assert.Equal(t, "spec.replicas", denials[1].Field)
	assert.Equal(t, "metadata.labels.team", warnings[0].Field)
}

func TestEvaluateValidationRules_AllPass(t *testing.T) {
	h := &WebhookServer{}

	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"image":    "myorg/nginx:1.25",
			"replicas": float64(3),
		},
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{"team": "platform"},
		},
	}
	cfg := &orktypes.ValidationConfig{
		Rules: []orktypes.ValidationRule{
			{Field: "spec.image", Prefix: "myorg/", Message: "wrong registry", Action: orktypes.ValidationActionDeny},
			{Field: "spec.replicas", Max: "10", Message: "too many", Action: orktypes.ValidationActionDeny},
			{Field: "metadata.labels.team", Operator: orktypes.ConditionExists, Message: "team required", Action: orktypes.ValidationActionWarn},
		},
	}

	denials, warnings := h.evaluateValidationRules(context.Background(), obj, cfg, "Website")

	assert.Empty(t, denials)
	assert.Empty(t, warnings)
}

func TestEvaluateValidationRules_EmptyActionDefaultsToDeny(t *testing.T) {
	h := &WebhookServer{}

	obj := specObj(map[string]interface{}{"image": "bad-registry/nginx"})
	cfg := &orktypes.ValidationConfig{
		Rules: []orktypes.ValidationRule{
			// Action field not set — should default to deny
			{Field: "spec.image", Prefix: "myorg/", Message: "wrong registry"},
		},
	}

	denials, warnings := h.evaluateValidationRules(context.Background(), obj, cfg, "Website")

	assert.Len(t, denials, 1)
	assert.Empty(t, warnings)
}

// ── applyMutationRules ────────────────────────────────────────────────────────

func TestApplyMutationRules_NilConfig(t *testing.T) {
	h := &WebhookServer{}
	obj := specObj(map[string]interface{}{})

	changes, err := h.applyMutationRules(context.Background(), obj, nil, "Website")

	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestApplyMutationRules_EmptyRules(t *testing.T) {
	h := &WebhookServer{}
	obj := specObj(map[string]interface{}{})
	cfg := &orktypes.MutationConfig{Rules: []orktypes.MutationRule{}}

	changes, err := h.applyMutationRules(context.Background(), obj, cfg, "Website")

	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestApplyMutationRules_DefaultAppliedWhenAbsent(t *testing.T) {
	h := &WebhookServer{}
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{"name": "my-site"},
		"spec":     map[string]interface{}{},
	}
	cfg := &orktypes.MutationConfig{
		Rules: []orktypes.MutationRule{
			{Field: "spec.replicas", Default: "2"},
		},
	}

	changes, err := h.applyMutationRules(context.Background(), obj, cfg, "Website")

	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "spec.replicas", changes[0].Field)
	assert.Equal(t, "", changes[0].OldValue)
	assert.Equal(t, "2", changes[0].NewValue)
	assert.Equal(t, "default", changes[0].ChangeType)

	// Verify in-place mutation
	spec := obj["spec"].(map[string]interface{})
	assert.Equal(t, "2", spec["replicas"])
}

func TestApplyMutationRules_DefaultSkippedWhenPresent(t *testing.T) {
	h := &WebhookServer{}
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{"name": "my-site"},
		"spec":     map[string]interface{}{"replicas": "5"},
	}
	cfg := &orktypes.MutationConfig{
		Rules: []orktypes.MutationRule{
			{Field: "spec.replicas", Default: "2"},
		},
	}

	changes, err := h.applyMutationRules(context.Background(), obj, cfg, "Website")

	require.NoError(t, err)
	assert.Empty(t, changes, "default must not overwrite an existing value")

	spec := obj["spec"].(map[string]interface{})
	assert.Equal(t, "5", spec["replicas"], "original value must be preserved")
}

func TestApplyMutationRules_DefaultSkippedWhenValueAlreadyMatches(t *testing.T) {
	h := &WebhookServer{}
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{"name": "my-site"},
		"spec":     map[string]interface{}{"logLevel": "info"},
	}
	cfg := &orktypes.MutationConfig{
		Rules: []orktypes.MutationRule{
			{Field: "spec.logLevel", Default: "info"},
		},
	}

	changes, err := h.applyMutationRules(context.Background(), obj, cfg, "Website")

	require.NoError(t, err)
	assert.Empty(t, changes, "no change emitted when value already matches")
}

func TestApplyMutationRules_OverrideAlwaysApplies(t *testing.T) {
	h := &WebhookServer{}
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{"name": "my-site"},
		"spec":     map[string]interface{}{},
	}
	cfg := &orktypes.MutationConfig{
		Rules: []orktypes.MutationRule{
			{Field: "metadata.labels.managed-by", Override: "orkestra"},
		},
	}

	changes, err := h.applyMutationRules(context.Background(), obj, cfg, "Website")

	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "metadata.labels.managed-by", changes[0].Field)
	assert.Equal(t, "orkestra", changes[0].NewValue)
	assert.Equal(t, "override", changes[0].ChangeType)
}

func TestApplyMutationRules_OverrideReplacesExistingValue(t *testing.T) {
	h := &WebhookServer{}
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name": "my-site",
			"labels": map[string]interface{}{
				"managed-by": "helm",
			},
		},
		"spec": map[string]interface{}{},
	}
	cfg := &orktypes.MutationConfig{
		Rules: []orktypes.MutationRule{
			{Field: "metadata.labels.managed-by", Override: "orkestra"},
		},
	}

	changes, err := h.applyMutationRules(context.Background(), obj, cfg, "Website")

	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "helm", changes[0].OldValue)
	assert.Equal(t, "orkestra", changes[0].NewValue)
	assert.Equal(t, "override", changes[0].ChangeType)
}

func TestApplyMutationRules_MultipleRules(t *testing.T) {
	h := &WebhookServer{}
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{"name": "my-site"},
		"spec":     map[string]interface{}{},
	}
	cfg := &orktypes.MutationConfig{
		Rules: []orktypes.MutationRule{
			{Field: "spec.replicas", Default: "2"},
			{Field: "spec.logLevel", Default: "info"},
			{Field: "metadata.labels.managed-by", Override: "orkestra"},
		},
	}

	changes, err := h.applyMutationRules(context.Background(), obj, cfg, "Website")

	require.NoError(t, err)
	assert.Len(t, changes, 3)

	spec := obj["spec"].(map[string]interface{})
	assert.Equal(t, "2", spec["replicas"])
	assert.Equal(t, "info", spec["logLevel"])
}

func TestApplyMutationRules_NestedFieldCreated(t *testing.T) {
	h := &WebhookServer{}
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{"name": "my-site"},
		"spec":     map[string]interface{}{},
	}
	cfg := &orktypes.MutationConfig{
		Rules: []orktypes.MutationRule{
			{Field: "spec.db.engine", Default: "postgres"},
		},
	}

	changes, err := h.applyMutationRules(context.Background(), obj, cfg, "Website")

	require.NoError(t, err)
	require.Len(t, changes, 1)

	spec := obj["spec"].(map[string]interface{})
	db, ok := spec["db"].(map[string]interface{})
	require.True(t, ok, "intermediate map should have been created")
	assert.Equal(t, "postgres", db["engine"])
}

// ── deepCopyMap ───────────────────────────────────────────────────────────────

func TestDeepCopyMap_IndependentFromOriginal(t *testing.T) {
	original := map[string]interface{}{
		"spec": map[string]interface{}{
			"image": "myorg/nginx:1.25",
		},
	}

	cp := deepCopyMap(original)

	// Mutate the copy — original must not change
	cp["spec"].(map[string]interface{})["image"] = "mutated"

	assert.Equal(t, "myorg/nginx:1.25",
		original["spec"].(map[string]interface{})["image"],
		"original must not be affected by copy mutation")
}

func TestDeepCopyMap_NilReturnsNil(t *testing.T) {
	assert.Nil(t, deepCopyMap(nil))
}

func TestDeepCopyMap_PreservesAllFields(t *testing.T) {
	original := map[string]interface{}{
		"a": "string",
		"b": float64(42),
		"c": true,
		"d": map[string]interface{}{"nested": "value"},
	}

	cp := deepCopyMap(original)

	assert.Equal(t, "string", cp["a"])
	assert.Equal(t, float64(42), cp["b"])
	assert.Equal(t, true, cp["c"])
	assert.Equal(t, "value", cp["d"].(map[string]interface{})["nested"])
}

// ── buildJSONPatch ────────────────────────────────────────────────────────────

func TestBuildJSONPatch_AddForAbsentField(t *testing.T) {
	changes := []fieldChange{
		{Field: "spec.replicas", OldValue: "", NewValue: "2", TypedValue: "2", ChangeType: "default"},
	}

	raw, err := buildJSONPatch(changes)
	require.NoError(t, err)

	var ops []JSONPatchOp
	require.NoError(t, json.Unmarshal(raw, &ops))
	require.Len(t, ops, 1)
	assert.Equal(t, "add", ops[0].Op)
	assert.Equal(t, "/spec/replicas", ops[0].Path)
	assert.Equal(t, "2", ops[0].Value)
}

func TestBuildJSONPatch_ReplaceForExistingField(t *testing.T) {
	changes := []fieldChange{
		{Field: "metadata.labels.managed-by", OldValue: "helm", NewValue: "orkestra", ChangeType: "override"},
	}

	raw, err := buildJSONPatch(changes)
	require.NoError(t, err)

	var ops []JSONPatchOp
	require.NoError(t, json.Unmarshal(raw, &ops))
	require.Len(t, ops, 1)
	assert.Equal(t, "replace", ops[0].Op)
	assert.Equal(t, "/metadata/labels/managed-by", ops[0].Path)
}

func TestBuildJSONPatch_MultipleChanges(t *testing.T) {
	changes := []fieldChange{
		{Field: "spec.replicas", OldValue: "", NewValue: "2", ChangeType: "default"},
		{Field: "spec.logLevel", OldValue: "", NewValue: "info", ChangeType: "default"},
		{Field: "metadata.labels.managed-by", OldValue: "helm", NewValue: "orkestra", ChangeType: "override"},
	}

	raw, err := buildJSONPatch(changes)
	require.NoError(t, err)

	var ops []JSONPatchOp
	require.NoError(t, json.Unmarshal(raw, &ops))
	assert.Len(t, ops, 3)
}

func TestBuildJSONPatch_EmptyChanges(t *testing.T) {
	raw, err := buildJSONPatch(nil)
	require.NoError(t, err)

	var ops []JSONPatchOp
	require.NoError(t, json.Unmarshal(raw, &ops))
	assert.Empty(t, ops)
}

// ── gvrToKey ──────────────────────────────────────────────────────────────────

func TestGVRToKey(t *testing.T) {
	tests := []struct {
		name string
		gvr  metav1.GroupVersionResource
		want string
	}{
		{
			name: "group resource",
			gvr:  metav1.GroupVersionResource{Group: "demo.orkestra.io", Version: "v1alpha1", Resource: "websites"},
			want: "demo.orkestra.io/v1alpha1/websites",
		},
		{
			name: "core group (empty group)",
			gvr:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			want: "v1/pods",
		},
		{
			name: "apps group",
			gvr:  metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			want: "apps/v1/deployments",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, gvrToKey(tc.gvr))
		})
	}
}
