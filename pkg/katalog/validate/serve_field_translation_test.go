package validate

import (
	"strings"
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// katalogWithTranslation builds a minimal Katalog with one serve-enabled CRD
// whose fields map contains the given field configs.
func katalogWithTranslation(fields map[string]orktypes.ServeFieldConfig) *executor {
	return katalogWithServe(&orktypes.ServeConfig{
		Enabled: true,
		Fields:  fields,
	})
}

func TestValidateServeFieldTranslation_NoFields(t *testing.T) {
	k := katalogWithServe(&orktypes.ServeConfig{Enabled: true})
	if err := k.validateServeFieldTranslation(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeFieldTranslation_PlainField_Valid(t *testing.T) {
	k := katalogWithTranslation(map[string]orktypes.ServeFieldConfig{
		"image": {Label: "Image"},
	})
	if err := k.validateServeFieldTranslation(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeFieldTranslation_Value_Valid(t *testing.T) {
	k := katalogWithTranslation(map[string]orktypes.ServeFieldConfig{
		"timeout": {
			Path:  "timeoutSeconds",
			Value: `{{ .value }}`,
		},
	})
	if err := k.validateServeFieldTranslation(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeFieldTranslation_Values_Valid(t *testing.T) {
	k := katalogWithTranslation(map[string]orktypes.ServeFieldConfig{
		"image": {
			Values: map[string]string{
				"image.registry":   `{{ .value }}`,
				"image.repository": `{{ .request.image }}`,
				"image.tag":        `latest`,
			},
		},
	})
	if err := k.validateServeFieldTranslation(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeFieldTranslation_GateOnly_Valid(t *testing.T) {
	k := katalogWithTranslation(map[string]orktypes.ServeFieldConfig{
		"productionApproval": {Label: "Approval ticket"},
	})
	if err := k.validateServeFieldTranslation(); err != nil {
		t.Fatalf("gate-only field (no path/value/values) should be valid: %v", err)
	}
}

func TestValidateServeFieldTranslation_ValueAndValues_MutuallyExclusive(t *testing.T) {
	k := katalogWithTranslation(map[string]orktypes.ServeFieldConfig{
		"image": {
			Value: `{{ .value }}`,
			Values: map[string]string{
				"image.registry": `{{ .value }}`,
			},
		},
	})
	err := k.validateServeFieldTranslation()
	if err == nil {
		t.Fatal("expected error: value and values mutually exclusive")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention mutually exclusive, got: %v", err)
	}
}

func TestValidateServeFieldTranslation_ValuesKeyEmpty(t *testing.T) {
	k := katalogWithTranslation(map[string]orktypes.ServeFieldConfig{
		"image": {
			Values: map[string]string{
				"": `{{ .value }}`,
			},
		},
	})
	err := k.validateServeFieldTranslation()
	if err == nil {
		t.Fatal("expected error: empty values key")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("error should mention empty key, got: %v", err)
	}
}

func TestValidateServeFieldTranslation_ValuesExpressionEmpty(t *testing.T) {
	k := katalogWithTranslation(map[string]orktypes.ServeFieldConfig{
		"image": {
			Values: map[string]string{
				"image.registry": "",
			},
		},
	})
	err := k.validateServeFieldTranslation()
	if err == nil {
		t.Fatal("expected error: empty values expression")
	}
	if !strings.Contains(err.Error(), "expression must not be empty") {
		t.Errorf("error should mention empty expression, got: %v", err)
	}
}

func TestValidateServeFieldTranslation_ValueInvalidTemplate(t *testing.T) {
	k := katalogWithTranslation(map[string]orktypes.ServeFieldConfig{
		"timeout": {
			Path:  "timeoutSeconds",
			Value: `{{ .value`, // missing closing }}
		},
	})
	err := k.validateServeFieldTranslation()
	if err == nil {
		t.Fatal("expected error: invalid template in value")
	}
	if !strings.Contains(err.Error(), "invalid template") {
		t.Errorf("error should mention invalid template, got: %v", err)
	}
}

func TestValidateServeFieldTranslation_ValuesInvalidTemplate(t *testing.T) {
	k := katalogWithTranslation(map[string]orktypes.ServeFieldConfig{
		"image": {
			Values: map[string]string{
				"image.tag": `{{ .value | `, // malformed pipe
			},
		},
	})
	err := k.validateServeFieldTranslation()
	if err == nil {
		t.Fatal("expected error: invalid template in values expression")
	}
	if !strings.Contains(err.Error(), "invalid template") {
		t.Errorf("error should mention invalid template, got: %v", err)
	}
}

func TestValidateServeFieldTranslation_ServeDisabled_Skipped(t *testing.T) {
	k := katalogWithServe(&orktypes.ServeConfig{
		Enabled: false,
		Fields: map[string]orktypes.ServeFieldConfig{
			"image": {
				Value: `{{ .value`, // would fail if checked
			},
		},
	})
	if err := k.validateServeFieldTranslation(); err != nil {
		t.Fatalf("disabled serve should be skipped, got: %v", err)
	}
}
