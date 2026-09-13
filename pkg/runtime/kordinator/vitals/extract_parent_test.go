// pkg/runtime/kordinator/vitals/extract_parent_test.go
package vitals

import (
	"testing"
)

// buildObj builds a minimal objMap for testing.
func buildObj(status map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"status": status}
}

func withCondition(typ, statusStr, reason, message string) map[string]interface{} {
	return map[string]interface{}{
		"type":    typ,
		"status":  statusStr,
		"reason":  reason,
		"message": message,
	}
}

// ── statusless types (ConfigMap, Secret) ──────────────────────────────────────

func TestExtractParentReady_ConfigMap_NoAnnotation_ReturnsReady(t *testing.T) {
	obj := map[string]interface{}{"metadata": map[string]interface{}{}}
	ready, reason, _ := extractParentReady(obj, "ConfigMap")
	if !ready || reason != "Exists" {
		t.Errorf("ConfigMap without annotation must be ready/Exists, got ready=%v reason=%q", ready, reason)
	}
}

func TestExtractParentReady_ConfigMap_PhaseReadyAnnotation(t *testing.T) {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]interface{}{
				"orkestra.io/phase": "Ready",
			},
		},
	}
	ready, reason, _ := extractParentReady(obj, "ConfigMap")
	if !ready || reason != "Ready" {
		t.Errorf("ConfigMap with phase=Ready must be ready, got ready=%v reason=%q", ready, reason)
	}
}

func TestExtractParentReady_ConfigMap_PhaseFailedAnnotation(t *testing.T) {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]interface{}{
				"orkestra.io/phase": "Failed",
			},
		},
	}
	ready, reason, _ := extractParentReady(obj, "ConfigMap")
	if ready || reason != "Failed" {
		t.Errorf("ConfigMap with phase=Failed must not be ready, got ready=%v reason=%q", ready, reason)
	}
}

// ── Ready condition ───────────────────────────────────────────────────────────

func TestExtractParentReady_ReadyConditionTrue(t *testing.T) {
	obj := buildObj(map[string]interface{}{
		"conditions": []interface{}{
			withCondition("Ready", "True", "AllGood", "all systems go"),
		},
	})
	ready, reason, _ := extractParentReady(obj, "Deployment")
	if !ready || reason != "AllGood" {
		t.Errorf("Ready=True must return ready, got ready=%v reason=%q", ready, reason)
	}
}

func TestExtractParentReady_ReadyConditionFalse(t *testing.T) {
	obj := buildObj(map[string]interface{}{
		"conditions": []interface{}{
			withCondition("Ready", "False", "Unavailable", "not ready yet"),
		},
	})
	ready, _, _ := extractParentReady(obj, "Deployment")
	if ready {
		t.Error("Ready=False must return not ready")
	}
}

// ── Success condition types ───────────────────────────────────────────────────

func TestExtractParentReady_CompletedConditionTrue(t *testing.T) {
	// Use a non-builtin kind so we go through the conditions path (not statusless)
	obj := buildObj(map[string]interface{}{
		"conditions": []interface{}{
			withCondition("Complete", "True", "Completed", "job done"),
		},
	})
	ready, reason, _ := extractParentReady(obj, "CustomResource")
	if !ready || reason != "Complete" {
		t.Errorf("Complete=True must return ready, got ready=%v reason=%q", ready, reason)
	}
}

func TestExtractParentReady_FailedConditionTrue(t *testing.T) {
	obj := buildObj(map[string]interface{}{
		"conditions": []interface{}{
			withCondition("Failed", "True", "BackoffLimitExceeded", "too many retries"),
		},
	})
	ready, reason, _ := extractParentReady(obj, "CustomResource")
	if ready || reason != "Failed" {
		t.Errorf("Failed=True must return not ready, got ready=%v reason=%q", ready, reason)
	}
}

// ── Numeric/replica heuristics ────────────────────────────────────────────────

func TestExtractParentReady_SucceededField(t *testing.T) {
	// Use a non-builtin kind to exercise numeric heuristics
	obj := buildObj(map[string]interface{}{
		"succeeded": int64(1),
	})
	ready, reason, _ := extractParentReady(obj, "CustomResource")
	if !ready || reason != "Succeeded" {
		t.Errorf("succeeded>0 must return ready, got ready=%v reason=%q", ready, reason)
	}
}

func TestExtractParentReady_DeploymentAvailableReplicas(t *testing.T) {
	obj := buildObj(map[string]interface{}{
		"replicas":          int64(3),
		"availableReplicas": int64(3),
	})
	ready, reason, _ := extractParentReady(obj, "Deployment")
	if !ready || reason != "AvailableReplicas" {
		t.Errorf("availableReplicas==replicas must return ready, got ready=%v reason=%q", ready, reason)
	}
}

func TestExtractParentReady_DeploymentNotYetAvailable(t *testing.T) {
	obj := buildObj(map[string]interface{}{
		"replicas":          int64(3),
		"availableReplicas": int64(1),
	})
	ready, _, _ := extractParentReady(obj, "Deployment")
	if ready {
		t.Error("partial availableReplicas must not be ready")
	}
}

// ── Phase-based heuristics ────────────────────────────────────────────────────

func TestExtractParentReady_PodPhaseRunning(t *testing.T) {
	obj := buildObj(map[string]interface{}{
		"phase": "Running",
	})
	ready, reason, _ := extractParentReady(obj, "Pod")
	if !ready || reason != "Running" {
		t.Errorf("phase=Running must return ready, got ready=%v reason=%q", ready, reason)
	}
}

func TestExtractParentReady_PodPhaseFailed(t *testing.T) {
	obj := buildObj(map[string]interface{}{
		"phase": "Failed",
	})
	ready, reason, _ := extractParentReady(obj, "Pod")
	if ready || reason != "Failed" {
		t.Errorf("phase=Failed must not be ready, got ready=%v reason=%q", ready, reason)
	}
}

func TestExtractParentReady_PodPhaseSucceeded(t *testing.T) {
	obj := buildObj(map[string]interface{}{
		"phase": "Succeeded",
	})
	ready, reason, _ := extractParentReady(obj, "Pod")
	if !ready || reason != "Succeeded" {
		t.Errorf("phase=Succeeded must be ready, got ready=%v reason=%q", ready, reason)
	}
}

// ── No status — fallback ──────────────────────────────────────────────────────

func TestExtractParentReady_NoStatus_ReturnsPending(t *testing.T) {
	obj := map[string]interface{}{}
	ready, reason, _ := extractParentReady(obj, "CustomResource")
	if ready || reason != "Pending" {
		t.Errorf("no status must return not-ready/Pending, got ready=%v reason=%q", ready, reason)
	}
}

func TestExtractParentReady_EmptyStatus_ReturnsPending(t *testing.T) {
	obj := buildObj(map[string]interface{}{})
	ready, reason, _ := extractParentReady(obj, "CustomResource")
	if ready || reason != "Pending" {
		t.Errorf("empty status must return not-ready/Pending, got ready=%v reason=%q", ready, reason)
	}
}

// ── Top-level phase ───────────────────────────────────────────────────────────

func TestExtractParentReady_TopLevelPhaseReady(t *testing.T) {
	obj := map[string]interface{}{"phase": "Ready"}
	ready, reason, _ := extractParentReady(obj, "CustomResource")
	if !ready || reason != "Ready" {
		t.Errorf("top-level phase=Ready must be ready, got ready=%v reason=%q", ready, reason)
	}
}

func TestExtractParentReady_TopLevelPhaseFailed(t *testing.T) {
	obj := map[string]interface{}{"phase": "Failed"}
	ready, reason, _ := extractParentReady(obj, "CustomResource")
	if ready || reason != "Failed" {
		t.Errorf("top-level phase=Failed must not be ready, got ready=%v reason=%q", ready, reason)
	}
}
