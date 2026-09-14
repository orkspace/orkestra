package vitals

import (
	"fmt"

	"github.com/orkspace/orkestra/pkg/children"
)

// extractParentReady determines whether a parent object should be considered "ready".
// It handles statusless types (using annotations) and a variety of common Kubernetes
// status shapes for typed resources (Jobs, Deployments, StatefulSets, Pods, etc).
//
// The function is defensive: it accepts a generic map[string]interface{} (as produced
// by unstructured.Unstructured.Object) and attempts several heuristics in order:
//  1. If the type is statusless, use the orkestra.io/phase annotation (optimistic).
//  2. Look for a Ready condition in status.conditions (preferred).
//  3. Look for other common "success" condition types (Complete, Succeeded, SuccessCriteriaMet).
//  4. Inspect well-known numeric/status fields (succeeded, ready, availableReplicas, completionTime).
//  5. Inspect a top-level "phase" field (e.g., Pod phase).
//
// If none of the heuristics match, the function returns (false, "Pending", "") to indicate
// the object is not yet considered ready.
func extractParentReady(objMap map[string]interface{}, parentKind string) (ready bool, reason, message string) {
	// Helper accessors
	getMap := func(m map[string]interface{}, key string) map[string]interface{} {
		if v, ok := m[key].(map[string]interface{}); ok {
			return v
		}
		return nil
	}
	getSlice := func(m map[string]interface{}, key string) []interface{} {
		if v, ok := m[key].([]interface{}); ok {
			return v
		}
		return nil
	}
	getString := func(m map[string]interface{}, key string) string {
		if v, ok := m[key].(string); ok {
			return v
		}
		return ""
	}
	getInt := func(m map[string]interface{}, key string) int64 {
		switch v := m[key].(type) {
		case int:
			return int64(v)
		case int32:
			return int64(v)
		case int64:
			return v
		case float32:
			return int64(v)
		case float64:
			return int64(v)
		}
		return 0
	}
	hasKey := func(m map[string]interface{}, key string) bool {
		_, ok := m[key]
		return ok
	}

	// 1) Statusless types: use annotation-based phase if present
	m := children.BuiltInMeta(parentKind)
	statusless := m.Statusless || m.SkipStatusSubresource

	if statusless {
		meta := getMap(objMap, "metadata")
		if meta != nil {
			annotations := getMap(meta, "annotations")
			if annotations != nil {
				if phase, ok := annotations["orkestra.io/phase"].(string); ok {
					switch phase {
					case "Ready", "Succeeded":
						return true, phase, ""
					case "Failed", "Degraded", "Error":
						return false, phase, ""
					default:
						// Running, Pending, unknown → optimistic
						return true, phase, ""
					}
				}
			}
		}
		// No phase annotation yet — treat existence as ready for statusless types
		return true, "Exists", ""
	}

	// 2) Standard path: look for Ready condition in status.conditions
	status := getMap(objMap, "status")
	if status != nil {
		conds := getSlice(status, "conditions")
		for _, c := range conds {
			cond, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			if t, _ := cond["type"].(string); t == "Ready" {
				ready = (fmt.Sprint(cond["status"]) == "True")
				reason = fmt.Sprint(cond["reason"])
				message = fmt.Sprint(cond["message"])
				if message == "<nil>" {
					message = ""
				}
				return ready, reason, message
			}
		}

		// 3) Look for other common success condition types
		successTypes := map[string]struct{}{
			"Complete":           {},
			"Completed":          {},
			"Succeeded":          {},
			"SuccessCriteriaMet": {},
			"Ready":              {}, // already checked
		}
		failureTypes := map[string]struct{}{
			"Failed":   {},
			"Degraded": {},
			"Error":    {},
		}
		for _, c := range conds {
			cond, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			t := fmt.Sprint(cond["type"])
			st := fmt.Sprint(cond["status"])
			if _, ok := successTypes[t]; ok && st == "True" {
				reason = fmt.Sprint(cond["reason"])
				message = fmt.Sprint(cond["message"])
				if message == "<nil>" {
					message = ""
				}
				return true, t, message
			}
			if _, ok := failureTypes[t]; ok && st == "True" {
				reason = fmt.Sprint(cond["reason"])
				message = fmt.Sprint(cond["message"])
				if message == "<nil>" {
					message = ""
				}
				return false, t, message
			}
		}

		// 4) Numeric/status fields heuristics (Jobs, Deployments, StatefulSets, Pods)
		// Job-like: succeeded > 0 or completionTime present
		if hasKey(status, "succeeded") {
			if getInt(status, "succeeded") > 0 {
				return true, "Succeeded", ""
			}
		}
		if hasKey(status, "completionTime") {
			// presence of completionTime implies finished (success or failure); prefer succeeded check above
			if getString(status, "completionTime") != "" {
				// If conditions exist, prefer them; otherwise optimistic success
				return true, "Completed", ""
			}
		}
		// Some Job variants expose 'ready' or 'ready' numeric field
		if hasKey(status, "ready") {
			if getInt(status, "ready") > 0 {
				return true, "ReadyCount", ""
			}
		}
		// Deployment/StatefulSet: availableReplicas == replicas (or readyReplicas == replicas)
		if hasKey(status, "availableReplicas") && hasKey(status, "replicas") {
			if getInt(status, "availableReplicas") >= getInt(status, "replicas") && getInt(status, "replicas") > 0 {
				return true, "AvailableReplicas", ""
			}
		}
		if hasKey(status, "readyReplicas") && hasKey(status, "replicas") {
			if getInt(status, "readyReplicas") >= getInt(status, "replicas") && getInt(status, "replicas") > 0 {
				return true, "ReadyReplicas", ""
			}
		}
		// Pod-like: phase == Succeeded or Running (Running is optimistic)
		if hasKey(status, "phase") {
			phase := getString(status, "phase")
			switch phase {
			case "Succeeded":
				return true, "Succeeded", ""
			case "Failed":
				return false, "Failed", ""
			case "Running":
				return true, "Running", ""
			}
		}
	}

	// 5) Top-level phase (some CRs put phase at top-level)
	if phase, ok := objMap["phase"].(string); ok {
		switch phase {
		case "Succeeded", "Ready":
			return true, phase, ""
		case "Failed", "Degraded", "Error":
			return false, phase, ""
		default:
			return false, phase, ""
		}
	}

	// No definitive signal — first reconcile not yet complete
	return false, "Pending", ""
}
