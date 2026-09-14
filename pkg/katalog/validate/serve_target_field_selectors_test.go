package validate

import (
	"strings"
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

func katalogWithServeAndFieldSelector(target string, selector map[string]string) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Enabled: true,
				Target: orktypes.ServeTargetValue{
					Entries: map[string]*orktypes.ServeTargetConfig{
						target: {
							FieldSelector: selector,
						},
					},
				},
			},
		},
	})
}

func katalogWithServeTargetsAndFieldSelectors(targets map[string]map[string]string) *executor {
	entries := make(map[string]*orktypes.ServeTargetConfig)
	for name, selector := range targets {
		entries[name] = &orktypes.ServeTargetConfig{
			FieldSelector: selector,
		}
	}
	return newKatalogExec(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Enabled: true,
				Target: orktypes.ServeTargetValue{
					Entries: entries,
				},
			},
		},
	})
}

// ─── Tests ───────────────────────────────────────────────────────────────────

func TestValidateServeFieldSelector_NoFieldSelector(t *testing.T) {
	k := katalogWithServeAndTarget("myapp")
	if err := k.validateServeFieldSelector(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeFieldSelector_SingleTargetValid(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.mealPlan": "dinner",
	})
	if err := k.validateServeFieldSelector(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeFieldSelector_MultipleFieldsValid(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.mealPlan":      "dinner",
		"spec.kitchenConfig": "standard",
	})
	if err := k.validateServeFieldSelector(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeFieldSelector_MaxThreeFields(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.field1": "value1",
		"spec.field2": "value2",
		"spec.field3": "value3",
	})
	if err := k.validateServeFieldSelector(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeFieldSelector_MoreThanThreeFields(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.field1": "value1",
		"spec.field2": "value2",
		"spec.field3": "value3",
		"spec.field4": "value4",
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error when more than 3 field selectors")
	}
	if !strings.Contains(err.Error(), "maximum is 3") {
		t.Errorf("error should mention max 3 fields, got: %v", err)
	}
}

func TestValidateServeFieldSelector_DuplicateSelectorsAcrossTargets(t *testing.T) {
	k := katalogWithServeTargetsAndFieldSelectors(map[string]map[string]string{
		"kitchen":  {"spec.mealPlan": "dinner"},
		"payments": {"spec.mealPlan": "dinner"}, // duplicate
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error when field selectors are duplicated across targets")
	}
	if !strings.Contains(err.Error(), "field selector") || !strings.Contains(err.Error(), "used by both targets") {
		t.Errorf("error should mention duplicate field selector, got: %v", err)
	}
}

func TestValidateServeFieldSelector_UniqueSelectors(t *testing.T) {
	k := katalogWithServeTargetsAndFieldSelectors(map[string]map[string]string{
		"kitchen":  {"spec.mealPlan": "dinner"},
		"payments": {"spec.paymentMethod": "card"},
	})
	if err := k.validateServeFieldSelector(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeFieldSelector_InvalidPathFormat(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		".invalid": "value",
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error for invalid field selector path format")
	}
	if !strings.Contains(err.Error(), "cannot start or end with a dot") {
		t.Errorf("error should mention invalid format, got: %v", err)
	}
}

func TestValidateServeFieldSelector_EmptyPath(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"": "value",
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error for empty field selector path")
	}
	if !strings.Contains(err.Error(), "field selector path cannot be empty") {
		t.Errorf("error should mention empty path, got: %v", err)
	}
}

func TestValidateServeFieldSelector_EmptyValue(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.mealPlan": "",
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error for empty field selector value")
	}
	if !strings.Contains(err.Error(), "empty field selector value") {
		t.Errorf("error should mention empty value, got: %v", err)
	}
}

func TestValidateServeFieldSelector_CRModeDisabledNoSelector(t *testing.T) {
	entries := map[string]*orktypes.ServeTargetConfig{
		"kitchen": {
			Modes: &orktypes.ServeModes{
				CR: boolPtr(false),
			},
			FieldSelector: nil,
		},
	}
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Enabled: true,
				Target: orktypes.ServeTargetValue{
					Entries: entries,
				},
			},
		},
	})
	if err := k.validateServeFieldSelector(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeFieldSelector_CRModeDisabledWithSelector(t *testing.T) {
	entries := map[string]*orktypes.ServeTargetConfig{
		"kitchen": {
			Modes: &orktypes.ServeModes{
				CR: boolPtr(false),
			},
			FieldSelector: map[string]string{
				"spec.mealPlan": "dinner",
			},
		},
	}
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Enabled: true,
				Target: orktypes.ServeTargetValue{
					Entries: entries,
				},
			},
		},
	})
	if err := k.validateServeFieldSelector(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Check that a warning was added
	entry := k.k.EnabledCRDs()["myresource"]
	if !entry.Warnings.HasWarnings() {
		t.Fatal("expected warning for CR mode disabled with no field selector")
	}
}

func TestValidateServeFieldSelector_TemplateInPath(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.{{.env}}.mealPlan": "dinner",
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error when field selector path contains template syntax")
	}
	if !strings.Contains(err.Error(), "template syntax in path") {
		t.Errorf("error should mention template syntax in path, got: %v", err)
	}
}

func TestValidateServeFieldSelector_TemplateInValue(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.mealPlan": "{{.mealType}}",
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error when field selector value contains template syntax")
	}
	if !strings.Contains(err.Error(), "template syntax in value") {
		t.Errorf("error should mention template syntax in value, got: %v", err)
	}
}

func TestValidateServeFieldSelector_TemplateInBothPathAndValue(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.{{.env}}.mealPlan": "{{.mealType}}",
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error when both path and value contain template syntax")
	}
	// Should mention both
	if !strings.Contains(err.Error(), "contains template syntax in path and value") {
		t.Errorf("error should mention contains template syntax in path and value, got: %v", err)
	}
}

func TestValidateServeFieldSelector_TemplateMultipleSelectors(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.mealPlan":      "dinner",
		"spec.{{.env}}.name": "production",
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error when one selector has template syntax")
	}
	if !strings.Contains(err.Error(), "template syntax in path") {
		t.Errorf("error should mention template syntax, got: %v", err)
	}
	if !strings.Contains(err.Error(), "spec.{{.env}}.name") {
		t.Errorf("error should mention the problematic path, got: %v", err)
	}
}

func TestValidateServeFieldSelector_TemplateInValueWithMultipleSelectors(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.mealPlan":      "{{.meal}}",
		"spec.kitchenConfig": "standard",
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error when one selector value has template syntax")
	}
	if !strings.Contains(err.Error(), "template syntax in value") {
		t.Errorf("error should mention template syntax in value, got: %v", err)
	}
	if !strings.Contains(err.Error(), "{{.meal}}") {
		t.Errorf("error should mention the problematic value, got: %v", err)
	}
}

func TestValidateServeFieldSelector_TemplateWithBraces(t *testing.T) {
	// Test different template syntax variations
	testCases := []struct {
		name  string
		path  string
		value string
	}{
		{
			name:  "double curly braces",
			path:  "spec.{{.env}}.mealPlan",
			value: "{{.mealType}}",
		},
		{
			name:  "triple curly braces",
			path:  "spec.{{{.env}}}.mealPlan",
			value: "{{{.mealType}}}",
		},
		{
			name:  "nested template",
			path:  "spec.{{.env.{{.region}}}}.mealPlan",
			value: "{{.mealType.{{.time}}}}",
		},
		{
			name:  "template with pipe",
			path:  "spec.{{.env | upper}}.mealPlan",
			value: "{{.mealType | lower}}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
				tc.path: tc.value,
			})
			err := k.validateServeFieldSelector()
			if err == nil {
				t.Fatal("expected error for template syntax")
			}
			if !strings.Contains(err.Error(), "template syntax") {
				t.Errorf("error should mention template syntax, got: %v", err)
			}
		})
	}
}

func TestValidateServeFieldSelector_NoTemplateInValidSelectors(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.mealPlan":      "dinner",
		"spec.kitchenConfig": "standard",
		"metadata.namespace": "default",
	})
	if err := k.validateServeFieldSelector(); err != nil {
		t.Fatalf("unexpected error for valid selectors: %v", err)
	}
}

func TestValidateServeFieldSelector_TemplateInPathMultipleBraces(t *testing.T) {
	k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
		"spec.{{.env}}.mealPlan.{{.time}}": "dinner",
	})
	err := k.validateServeFieldSelector()
	if err == nil {
		t.Fatal("expected error when path has multiple template placeholders")
	}
	if !strings.Contains(err.Error(), "template syntax in path") {
		t.Errorf("error should mention template syntax, got: %v", err)
	}
}

// Test template in path with different variable patterns
func TestValidateServeFieldSelector_TemplateVariablePatterns(t *testing.T) {
	testCases := []struct {
		name     string
		template string
	}{
		{"with dot", "{{.field}}"},
		{"without dot", "{{field}}"},
		{"with nested", "{{.field.subfield}}"},
		{"with index", "{{index .field 0}}"},
		{"with range", "{{range .items}}{{.}}{{end}}"},
		{"with if", "{{if .condition}}value{{end}}"},
		{"with and", "{{and .a .b}}"},
		{"with or", "{{or .a .b}}"},
		{"with not", "{{not .a}}"},
		{"with eq", "{{eq .a .b}}"},
		{"with ne", "{{ne .a .b}}"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			k := katalogWithServeAndFieldSelector("kitchen", map[string]string{
				"spec." + tc.template: "value",
			})
			err := k.validateServeFieldSelector()
			if err == nil {
				t.Errorf("expected error for template syntax: %s", tc.template)
			}
			if !strings.Contains(err.Error(), "template syntax") {
				t.Errorf("error should mention template syntax, got: %v", err)
			}
		})
	}
}
