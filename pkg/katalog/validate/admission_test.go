package validate

import (
	"strings"
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func katalogWithValidationRule(crdName string, rules ...orktypes.ValidationRule) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {
			Name:       crdName,
			Validation: &orktypes.ValidationConfig{Rules: rules},
		},
	})
}

func katalogWithMutationRule(crdName string, rules ...orktypes.MutationRule) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {
			Name:     crdName,
			Mutation: &orktypes.MutationConfig{Rules: rules},
		},
	})
}

func TestValidateAdmissionOperators_NoCRDs(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{})
	assert.NoError(t, k.validateAdmissionOperators())
}

func TestValidateAdmissionOperators_NoOperatorSet(t *testing.T) {
	k := katalogWithValidationRule("app", orktypes.ValidationRule{
		Field: "spec.image", Prefix: "myorg/", Message: "must be from myorg",
	})
	assert.NoError(t, k.validateAdmissionOperators())
}

func TestValidateAdmissionOperators_KnownOperator(t *testing.T) {
	k := katalogWithValidationRule("app", orktypes.ValidationRule{
		Field: "spec.replicas", Operator: orktypes.ConditionLte, Value: "10", Message: "too many replicas",
	})
	assert.NoError(t, k.validateAdmissionOperators())
}

func TestValidateAdmissionOperators_UnknownOperator(t *testing.T) {
	k := katalogWithValidationRule("app", orktypes.ValidationRule{
		Field: "spec.replicas", Operator: "lte2", Value: "10", Message: "too many replicas",
	})
	err := k.validateAdmissionOperators()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lte2")
	assert.Contains(t, err.Error(), "app")
	assert.Contains(t, err.Error(), "spec.replicas")
}

func TestValidateAdmissionOperators_UnknownOperatorInWhen(t *testing.T) {
	k := katalogWithValidationRule("app", orktypes.ValidationRule{
		Field: "spec.replicas", Prefix: "x", Message: "msg",
		When: []orktypes.Condition{{Field: "spec.tier", Operator: "greaterOrEqual"}},
	})
	err := k.validateAdmissionOperators()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greaterOrEqual")
}

func TestValidateAdmissionOperators_UnknownOperatorInOr(t *testing.T) {
	k := katalogWithValidationRule("app", orktypes.ValidationRule{
		Field: "spec.replicas", Prefix: "x", Message: "msg",
		Or: []orktypes.Condition{{Field: "spec.tier", Operator: "notARealOp"}},
	})
	err := k.validateAdmissionOperators()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notARealOp")
}

func TestValidateAdmissionOperators_MutationRuleUnknownOperatorInWhen(t *testing.T) {
	k := katalogWithMutationRule("app", orktypes.MutationRule{
		Field:   "spec.tier",
		Default: "standard",
		When:    []orktypes.Condition{{Field: "spec.env", Operator: "isEqualTo"}},
	})
	err := k.validateAdmissionOperators()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "isEqualTo")
}

func TestValidateAdmissionOperators_MutationRuleKnownOperator(t *testing.T) {
	k := katalogWithMutationRule("app", orktypes.MutationRule{
		Field:   "spec.tier",
		Default: "standard",
		When:    []orktypes.Condition{{Field: "spec.env", Operator: orktypes.ConditionGte, Value: "1"}},
	})
	assert.NoError(t, k.validateAdmissionOperators())
}

func katalogWithServeValidationRule(crdName string, srv *orktypes.ServeConfig, rules ...orktypes.ValidationRule) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		crdName: {
			Name:       crdName,
			Serve:      srv,
			Validation: &orktypes.ValidationConfig{Rules: rules},
		},
	})
}

func TestValidateValidationRuleLinks_NoLink(t *testing.T) {
	k := katalogWithValidationRule("app", orktypes.ValidationRule{
		Field: "spec.image", Prefix: "myorg/", Message: "must be from myorg",
	})
	assert.NoError(t, k.validateValidationRuleLinks())
}

func TestValidateValidationRuleLinks_MatchesAdditionalLabelField(t *testing.T) {
	serve := &orktypes.ServeConfig{
		Enabled: true,
		Labels:  map[string]orktypes.ServeFieldConfig{"team": {Label: "Team"}},
	}
	k := katalogWithServeValidationRule("app", serve, orktypes.ValidationRule{
		Field: `{{ isDNS1123Subdomain team }}`, Link: "team", Equals: "true", Message: "must be a valid subdomain",
	})
	assert.NoError(t, k.validateValidationRuleLinks())
}

func TestValidateValidationRuleLinks_MatchesAdditionalAnnotationField(t *testing.T) {
	serve := &orktypes.ServeConfig{
		Enabled:     true,
		Annotations: map[string]orktypes.ServeFieldConfig{"platform.myorg.io/jira-ticket": {Label: "Jira Ticket"}},
	}
	k := katalogWithServeValidationRule("app", serve, orktypes.ValidationRule{
		Field: `{{ getAnnotation . "platform.myorg.io/jira-ticket" }}`, Link: "platform.myorg.io/jira-ticket", Message: "must be set",
	})
	assert.NoError(t, k.validateValidationRuleLinks())
}

func TestValidateValidationRuleLinks_SpecFieldWithWrappingExpression(t *testing.T) {
	// link: pointing at a spec field is valid when Field wraps it in
	// something other than the plain "spec.<name>" path — e.g. a format
	// check built on a notes: function.
	serve := &orktypes.ServeConfig{
		Enabled: true, Fields: map[string]orktypes.ServeFieldConfig{
			"repoURL": {Label: "Repository URL"},
		}}
	k := katalogWithServeValidationRule("app", serve, orktypes.ValidationRule{
		Field: `{{ isValidGitRepository .spec.repoURL }}`, Link: "repoURL", Equals: "true", Message: "must be a valid git repository",
	})
	assert.NoError(t, k.validateValidationRuleLinks())
}

func TestValidateValidationRuleLinks_RedundantSpecFieldLink(t *testing.T) {
	serve := &orktypes.ServeConfig{
		Enabled: true,
		Fields: map[string]orktypes.ServeFieldConfig{
			"team": {Label: "Team"},
		}}
	k := katalogWithServeValidationRule("app", serve, orktypes.ValidationRule{
		Field: "spec.team", Link: "team", Operator: orktypes.ConditionExists, Message: "team is required",
	})
	err := k.validateValidationRuleLinks()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redundant")
	assert.Contains(t, err.Error(), "team")
}

func TestValidateValidationRuleLinks_UnknownLink(t *testing.T) {
	serve := &orktypes.ServeConfig{
		Enabled: true,
		Labels:  map[string]orktypes.ServeFieldConfig{"team": {Label: "Team"}},
	}
	k := katalogWithServeValidationRule("app", serve, orktypes.ValidationRule{
		Field: `{{ isDNS1123Subdomain typo }}`, Link: "typo", Equals: "true", Message: "must be valid",
	})
	err := k.validateValidationRuleLinks()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "typo")
	assert.Contains(t, err.Error(), "does not match any serve field")
}

func TestValidateValidationRuleLinks_NoServeConfig(t *testing.T) {
	k := katalogWithServeValidationRule("app", nil, orktypes.ValidationRule{
		Field: `{{ isDNS1123Subdomain team }}`, Link: "team", Equals: "true", Message: "must be valid",
	})
	err := k.validateValidationRuleLinks()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "team")
}

// Mutation Rules
func TestValidateMutationRules_FieldDefault(t *testing.T) {
	k := katalogWithMutationRule("app", orktypes.MutationRule{
		Field:   "metadata.name",
		Default: "my-app",
	})
	err := k.validateMutationRules()
	assert.NoError(t, err)
}

func TestValidateMutationRules_FieldOverride(t *testing.T) {
	k := katalogWithMutationRule("app", orktypes.MutationRule{
		Field:    "metadata.name",
		Override: "my-app",
	})
	err := k.validateMutationRules()
	assert.NoError(t, err)
}

func TestValidateMutationRules_FieldEmpty(t *testing.T) {
	k := katalogWithMutationRule("app", orktypes.MutationRule{
		Default:   3,
		ValueType: "int",
	})
	err := k.validateMutationRules()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field is required")
}

func TestValidateMutationRules_DefaultAndOverride(t *testing.T) {
	k := katalogWithMutationRule("app", orktypes.MutationRule{
		Field:     "spec.deploy.replicas",
		Default:   3,
		Override:  6,
		ValueType: "int",
	})

	err := k.validateMutationRules()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	crd := k.k.EnabledCRDs()["app"]
	if !crd.Warnings.HasWarnings() {
		t.Fatal("expected warning for default and override definition")
	}

	assert.NoError(t, err)

	warn := "CRD: app - mutation.rules[0] has both default and override defined. default will be ignored."
	containsWarn := crd.Warnings.Contains(warn)
	if !containsWarn {
		t.Fatalf("expected true: got %v - %q", containsWarn, strings.Join(crd.Warnings, ", "))
	}
}

// With Serve Synthesis
func TestValidateMutationRules_ServeFieldEmpty(t *testing.T) {
	k := katalogWithMutationRule("app", orktypes.MutationRule{
		Default:   3,
		ValueType: "int",
	})
	err := k.validateMutationRules()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field is required")
}

func TestValidateMutationRules_ServeFieldOverride(t *testing.T) {
	k := katalogWithServe(&orktypes.ServeConfig{
		Enabled: true,
		Fields: map[string]orktypes.ServeFieldConfig{
			"image": {
				Label:    "Image",
				Override: "my-app/v1",
			},
		},
	})
	err := k.validateMutationRules()
	assert.NoError(t, err)
}

func TestValidateMutationRules_ServeFieldDefault(t *testing.T) {
	k := katalogWithServe(&orktypes.ServeConfig{
		Enabled: true,
		Fields: map[string]orktypes.ServeFieldConfig{
			"image": {
				Label:   "Image",
				Default: "my-app/v1",
			},
		},
	})

	err := k.validateMutationRules()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'default' is not allowed for serve.fields.image")
}

func TestValidateMutationRules_ServeLabelDefaultAndOverride(t *testing.T) {
	k := katalogWithServe(&orktypes.ServeConfig{
		Enabled: true,
		Labels: map[string]orktypes.ServeFieldConfig{
			"image": {
				Label:    "Image",
				Default:  "my-app/v1",
				Override: "my-app/v2",
			},
		},
	})

	err := k.validateMutationRules()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	crd := k.k.EnabledCRDs()["myresource"]
	if !crd.Warnings.HasWarnings() {
		t.Fatal("expected warning for default and override definition")
	}

	assert.NoError(t, err)

	warn := "CRD: myresource - serve.labels.image has both default and override defined. default will be ignored."
	containsWarn := crd.Warnings.Contains(warn)
	if !containsWarn {
		t.Fatalf("expected true: got %v - %q", containsWarn, strings.Join(crd.Warnings, ", "))
	}
}

func TestValidateMutationRules_ServeAnnotationDefaultAndOverride(t *testing.T) {
	k := katalogWithServe(&orktypes.ServeConfig{
		Enabled: true,
		Annotations: map[string]orktypes.ServeFieldConfig{
			"image": {
				Label:    "Image",
				Default:  "my-app/v1",
				Override: "my-app/v2",
			},
		},
	})

	err := k.validateMutationRules()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	crd := k.k.EnabledCRDs()["myresource"]
	if !crd.Warnings.HasWarnings() {
		t.Fatal("expected warning for default and override definition")
	}

	assert.NoError(t, err)

	warn := "CRD: myresource - serve.annotations.image has both default and override defined. default will be ignored."
	containsWarn := crd.Warnings.Contains(warn)
	if !containsWarn {
		t.Fatalf("expected true: got %v - %q", containsWarn, strings.Join(crd.Warnings, ", "))
	}
}
