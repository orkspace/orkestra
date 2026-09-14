package common

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/secrets"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

const crossMetricHTTPTimeout = 5 * time.Second

// ResolveCrossToken resolves cross declaration token from either plan token or SecretRef.
// Used by autoscaler and reconciler packages currently
func ResolveCrossToken(ctx context.Context, cs kubernetes.Interface, s *orktypes.CrossSource) (string, error) {
	if !s.HasAuth() {
		return "", nil
	}
	if s.HasSecretRef() {
		ref := s.Auth.SecretRef
		return secrets.ReadSecretKey(ctx, cs, ref.SecretNamespace(), ref.SecretName(), ref.SecretKey())
	}
	return utils.ResolveEnvVar(s.Auth.Token)

}

// FetchCrossMetricHTTP calls the remote operator's /katalog/{crd} endpoint and
// extracts the named metric from the "metrics" key in the JSON response.
// This mirrors how readCross uses source.endpoint for CR observation.
// func FetchCrossMetricHTTP(ctx context.Context, cs kubernetes.Interface, source *orktypes.CrossSource, endpoint string) (map[string]interface{}, bool)
// FetchCrossMetricHTTP calls the remote operator's /katalog/{crd} endpoint and
// extracts the named metric from the "metrics" key in the JSON response.
// This mirrors how readCross uses source.endpoint for CR observation.

// fetchCrossViaHTTP fetches a CR's detail from an Orkestra CR endpoint.
// The endpoint should be the /katalog/{crd}/cr/{namespace}/{name} URL
// which is already built and running on every Orkestra instance.
func FetchCrossViaHTTP(ctx context.Context, cs kubernetes.Interface, source *orktypes.CrossSource) ([]byte, map[string]interface{}) {
	if ctx == context.TODO() {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, crossMetricHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, source.Endpoint, nil)
	if err != nil {
		return nil, nil
	}

	token, err := ResolveCrossToken(ctx, cs, source)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	code := resp.StatusCode
	if resp.StatusCode == http.StatusNotFound {
		return nil, map[string]interface{}{"found": "false"}
	}
	if code != http.StatusOK {
		return nil, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil
	}

	// Ensure "found" is set
	if _, ok := result["found"]; !ok {
		if result["name"] != nil {
			result["found"] = "true"
		} else {
			result["found"] = "false"
		}
	}

	return body, result
}

// ResolveResourceMetricFromObject extracts the raw metrics payload from the
// orkestra.orkspace.io/cross-metric annotation on a CR object map.
// This annotation is stamped by the runtime
// Returns nil when the annotation is absent or unparseable.
// Used by both the gateway to inject .metrics into
// the resolver so validation rules can reference metric-vocabulary fields.
func ResolveResourceMetricFromObject(obj map[string]interface{}) map[string]interface{} {
	return ResolveAnnotationFromObject(obj, labels.AnnotationCrossMetrics)
}

// ResolveResourceHealthFromObject extracts the raw health payload from the
// orkestra.orkspace.io/health annotation on a CR object map.
// This annotation is stamped by the runtime
// Returns nil when the annotation is absent or unparseable.
// Used by both the gateway to inject .health into
// the resolver so validation rules can reference health-vocabulary fields.
func ResolveResourceHealthFromObject(obj map[string]interface{}) map[string]interface{} {
	return ResolveAnnotationFromObject(obj, labels.AnnotationHealth)
}

// InjectMetricsAnnotation stores the raw metrics payload as a JSON-encoded
// annotation so the admission webhook can bind it as .metrics in validation
// rules, enabling metrics-level gates.
func InjectMetricsAnnotation(obj *unstructured.Unstructured, raw map[string]interface{}) {
	InjectAnnotationToObject(obj, raw, labels.AnnotationCrossMetrics)
}

// InjectHealthAnnotation stores the raw health payload as a JSON-encoded
// annotation so the admission webhook can bind it as .health in validation
// rules, enabling health-level gates.
func InjectHealthAnnotation(obj *unstructured.Unstructured, raw map[string]interface{}) {
	InjectAnnotationToObject(obj, raw, labels.AnnotationHealth)
}
