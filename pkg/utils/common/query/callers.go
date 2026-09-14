package query

import (
	"fmt"
	"net/url"

	"github.com/orkspace/orkestra/pkg/runtime/kordinator/vitals"
)

// IsUnique lists every existing instance of the CRD via the runtime's cache
// and reports whether none of them (other than the CR under evaluation
// itself) has field == value.
func (q *runtimeQuery) IsUnique(field, value, selfNamespace, selfName string) (bool, error) {
	reqURL := fmt.Sprintf("%s/katalog/%s/cr?field=%s", q.endpoint, q.crdName, url.QueryEscape(field))

	var list vitals.CRListResponse
	_, err := q.result(list, "uniqueness", reqURL)
	if err != nil {
		return false, err
	}

	for _, item := range list.Items {
		if item.Namespace == selfNamespace && item.Name == selfName {
			continue // never a duplicate of its own stored value
		}
		if item.Value == value {
			return false, nil
		}
	}
	return true, nil
}

// ForHealth returns the /health endpoint of the CRD via the runtime's /katalog endpoint
// The value is added to the resolver for validation and mutation rules to gate on
func (q *runtimeQuery) ForHealth() map[string]interface{} {
	reqURL := fmt.Sprintf("%s/katalog/%s/health", q.endpoint, q.crdName)

	var health map[string]interface{}
	_, err := q.result(health, "health", reqURL)
	if err != nil {
		return map[string]interface{}{}
	}
	return health
}

// ForMetrics returns the /crdName endpoint of the CRD via the runtime's /katalog endpoint
// The value is added to the resolver for validation and mutation rules to gate on
func (q *runtimeQuery) ForMetrics() map[string]interface{} {
	reqURL := fmt.Sprintf("%s/katalog/%s", q.endpoint, q.crdName)

	// Parse top-level "metrics" key from the /katalog/{crd} response.
	var m struct {
		Metrics map[string]interface{} `json:"metrics"`
	}

	_, err := q.result(m, "metrics", reqURL)
	if err != nil {
		return map[string]interface{}{}
	}

	return m.Metrics
}
