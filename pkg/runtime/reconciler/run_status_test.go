// pkg/reconciler/run_status_test.go
package reconciler

import (
	"errors"
	"strings"
	"testing"
	"time"

	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ── buildReadyCondition ───────────────────────────────────────────────────

func TestBuildReadyCondition_Success(t *testing.T) {
	cond := buildReadyCondition(nil, 3, false)

	if cond["type"] != "Ready" {
		t.Errorf("type: expected Ready, got %v", cond["type"])
	}
	if cond["status"] != "True" {
		t.Errorf("status: expected True, got %v", cond["status"])
	}
	if cond["reason"] != "ReconcileSucceeded" {
		t.Errorf("reason: expected ReconcileSucceeded, got %v", cond["reason"])
	}
	if cond["message"] != "" {
		t.Errorf("message: expected empty, got %v", cond["message"])
	}
	if cond["observedGeneration"] != int64(3) {
		t.Errorf("observedGeneration: expected 3, got %v", cond["observedGeneration"])
	}
	if _, ok := cond["lastTransitionTime"]; !ok {
		t.Error("lastTransitionTime: expected to be set")
	}
}

func TestBuildReadyCondition_Failure(t *testing.T) {
	err := errors.New("deployment: image pull failed")
	cond := buildReadyCondition(err, 5, false)

	if cond["status"] != "False" {
		t.Errorf("status: expected False, got %v", cond["status"])
	}
	if cond["reason"] != "ReconcileError" {
		t.Errorf("reason: expected ReconcileError, got %v", cond["reason"])
	}
	if cond["message"] != "deployment: image pull failed" {
		t.Errorf("message: expected error text, got %v", cond["message"])
	}
}

func TestBuildReadyCondition_LongErrorTruncated(t *testing.T) {
	longErr := errors.New(string(make([]byte, 300)))
	cond := buildReadyCondition(longErr, 1, false)

	msg, ok := cond["message"].(string)
	if !ok {
		t.Fatal("message is not a string")
	}
	if len(msg) > 256 {
		t.Errorf("message should be truncated to ≤256 chars, got %d", len(msg))
	}
	if !strings.HasSuffix(msg, "...") {
		t.Errorf("truncated message should end with ..., got %q", msg)
	}
}

func TestBuildReadyCondition_HasValidTimestamp(t *testing.T) {
	cond := buildReadyCondition(nil, 1, false)

	ts, ok := cond["lastTransitionTime"].(string)
	if !ok {
		t.Fatal("lastTransitionTime is not a string")
	}

	_, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Errorf("lastTransitionTime %q is not valid RFC3339: %v", ts, err)
	}
}

// ── ResolveStatusFields ───────────────────────────────────────────────────

func TestResolveStatusFields_StaticValues(t *testing.T) {
	resolver := orktmpl.NewResolverFromMap(map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "my-website",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"replicas": float64(3),
		},
	})

	fields := []orktypes.StatusFieldSpec{
		{Path: "phase", Value: "Running"},
		{Path: "ready", Value: "true"},
	}

	result, err := resolver.ResolveStatusFields(fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["phase"] != "Running" {
		t.Errorf("phase: expected Running, got %v", result["phase"])
	}
	if result["ready"] != "true" {
		t.Errorf("ready: expected true, got %v", result["ready"])
	}
}

func TestResolveStatusFields_TemplateExpressions(t *testing.T) {
	resolver := orktmpl.NewResolverFromMap(map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "my-website",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"replicas": float64(3),
			"version":  "1.25",
		},
	})

	fields := []orktypes.StatusFieldSpec{
		{Path: "endpoint", Value: "{{ .metadata.name }}.{{ .metadata.namespace }}.svc.cluster.local"},
		{Path: "observedReplicas", Value: "{{ .spec.replicas }}"},
		{Path: "version", Value: "{{ .spec.version }}"},
	}

	result, err := resolver.ResolveStatusFields(fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["endpoint"] != "my-website.default.svc.cluster.local" {
		t.Errorf("endpoint: got %v", result["endpoint"])
	}
	if result["observedReplicas"] != "3" {
		t.Errorf("observedReplicas: got %v", result["observedReplicas"])
	}
	if result["version"] != "1.25" {
		t.Errorf("version: got %v", result["version"])
	}
}

func TestResolveStatusFields_NestedPath(t *testing.T) {
	resolver := orktmpl.NewResolverFromMap(map[string]interface{}{
		"spec": map[string]interface{}{
			"host": "db.platform.svc",
			"port": "5432",
		},
	})

	fields := []orktypes.StatusFieldSpec{
		{Path: "database.host", Value: "{{ .spec.host }}"},
		{Path: "database.port", Value: "{{ .spec.port }}"},
	}

	result, err := resolver.ResolveStatusFields(fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	db, ok := result["database"].(map[string]interface{})
	if !ok {
		t.Fatalf("database: expected map, got %T", result["database"])
	}
	if db["host"] != "db.platform.svc" {
		t.Errorf("database.host: got %v", db["host"])
	}
	if db["port"] != "5432" {
		t.Errorf("database.port: got %v", db["port"])
	}
}

func TestResolveStatusFields_DeepNestedPath(t *testing.T) {
	resolver := orktmpl.NewResolverFromMap(map[string]interface{}{
		"spec": map[string]interface{}{
			"region": "us-east-1",
		},
	})

	fields := []orktypes.StatusFieldSpec{
		{Path: "cloud.provider.region", Value: "{{ .spec.region }}"},
	}

	result, err := resolver.ResolveStatusFields(fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cloud, ok := result["cloud"].(map[string]interface{})
	if !ok {
		t.Fatalf("cloud: expected map, got %T", result["cloud"])
	}
	provider, ok := cloud["provider"].(map[string]interface{})
	if !ok {
		t.Fatalf("cloud.provider: expected map, got %T", cloud["provider"])
	}
	if provider["region"] != "us-east-1" {
		t.Errorf("cloud.provider.region: got %v", provider["region"])
	}
}

func TestResolveStatusFields_EmptyFields(t *testing.T) {
	resolver := orktmpl.NewResolverFromMap(map[string]interface{}{})
	result, err := resolver.ResolveStatusFields(nil)
	if err != nil {
		t.Errorf("nil fields: expected no error, got %v", err)
	}
	if result != nil {
		t.Errorf("nil fields: expected nil result, got %v", result)
	}
}

func TestResolveStatusFields_MissingTemplateField_ResolvesToEmpty(t *testing.T) {
	// Missing CR fields resolve to "" — missingkey=zero
	// The field is still written — an empty string is a valid status value
	resolver := orktmpl.NewResolverFromMap(map[string]interface{}{
		"spec": map[string]interface{}{},
	})

	fields := []orktypes.StatusFieldSpec{
		{Path: "version", Value: "{{ .spec.version }}"},
	}

	result, err := resolver.ResolveStatusFields(fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["version"] != "" {
		t.Errorf("missing field should resolve to empty string, got %v", result["version"])
	}
}

// ── StatusConfig ──────────────────────────────────────────────────────────

func TestStatusConfig_ConditionsEnabled_DefaultTrue(t *testing.T) {
	var cfg *orktypes.StatusConfig
	if !cfg.ConditionsEnabled() {
		t.Error("nil StatusConfig: ConditionsEnabled should default to true")
	}
}

func TestStatusConfig_ConditionsEnabled_ExplicitFalse(t *testing.T) {
	f := false
	cfg := &orktypes.StatusConfig{Conditions: &f}
	if cfg.ConditionsEnabled() {
		t.Error("conditions: false should disable automatic conditions")
	}
}

func TestStatusConfig_HasFields_EmptyFalse(t *testing.T) {
	cfg := &orktypes.StatusConfig{}
	if cfg.HasFields() {
		t.Error("empty fields: HasFields should be false")
	}
}

func TestStatusConfig_HasFields_WithFields(t *testing.T) {
	cfg := &orktypes.StatusConfig{
		Fields: []orktypes.StatusFieldSpec{
			{Path: "phase", Value: "Running"},
		},
	}
	if !cfg.HasFields() {
		t.Error("non-empty fields: HasFields should be true")
	}
}
