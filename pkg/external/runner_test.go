package external_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	orkexternal "github.com/orkspace/orkestra/pkg/external"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// stubTransport returns a fixed response for every request.
type stubTransport struct {
	status int
	body   string
	err    error
}

func (s stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

func withTransport(t *testing.T, transport http.RoundTripper) func() {
	t.Helper()
	prev := orkexternal.HTTPTransport
	orkexternal.HTTPTransport = transport
	return func() { orkexternal.HTTPTransport = prev }
}

func resolver(data map[string]interface{}) *orktmpl.Resolver {
	return orktmpl.NewResolverFromMap(data)
}

func TestRun_EmptyCalls(t *testing.T) {
	r := resolver(map[string]interface{}{})
	got, err := orkexternal.Run(context.Background(), "test", r, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != r {
		t.Fatal("expected same resolver when calls is nil")
	}
}

func TestRun_SuccessfulCall(t *testing.T) {
	defer withTransport(t, stubTransport{status: 200, body: `{"enabled":true}`})()

	r := resolver(map[string]interface{}{})
	calls := []orktypes.ExternalCallSpec{
		{Name: "flags", URL: "http://flags.internal/v1"},
	}

	got, err := orkexternal.Run(context.Background(), "test", r, calls, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := got.Data()
	ext, ok := data["external"].(map[string]interface{})
	if !ok {
		t.Fatal("expected .external in resolver data")
	}
	flags, ok := ext["flags"].(map[string]interface{})
	if !ok {
		t.Fatal("expected .external.flags")
	}
	if flags["status"] != "200" {
		t.Errorf("status: got %v, want 200", flags["status"])
	}
	if flags["called"] != "true" {
		t.Errorf("called: got %v, want true", flags["called"])
	}
}

func TestRun_JSONBodyAutoParsed(t *testing.T) {
	defer withTransport(t, stubTransport{status: 200, body: `{"enabled":true,"limit":5}`})()

	r := resolver(map[string]interface{}{})
	calls := []orktypes.ExternalCallSpec{
		{Name: "cfg", URL: "http://config.internal/v1"},
	}

	got, err := orkexternal.Run(context.Background(), "test", r, calls, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := got.Data()
	ext := data["external"].(map[string]interface{})
	cfg := ext["cfg"].(map[string]interface{})

	// top-level JSON keys auto-merged into entry
	if cfg["enabled"] != true {
		t.Errorf("enabled: got %v, want true", cfg["enabled"])
	}
	if fmt.Sprintf("%v", cfg["limit"]) != "5" {
		t.Errorf("limit: got %v, want 5", cfg["limit"])
	}
}

func TestRun_ContinueOnError_True(t *testing.T) {
	defer withTransport(t, stubTransport{status: 503, body: "unavailable"})()

	r := resolver(map[string]interface{}{})
	calls := []orktypes.ExternalCallSpec{
		{Name: "svc", URL: "http://svc.internal/health", ContinueOnError: true},
	}

	got, err := orkexternal.Run(context.Background(), "test", r, calls, nil)
	if err != nil {
		t.Fatalf("expected no error with continueOnError:true, got: %v", err)
	}

	ext := got.Data()["external"].(map[string]interface{})
	svc := ext["svc"].(map[string]interface{})
	if svc["error"] == "" {
		t.Error("expected error to be set on failed call")
	}
}

func TestRun_ContinueOnError_False(t *testing.T) {
	defer withTransport(t, stubTransport{status: 503, body: "unavailable"})()

	r := resolver(map[string]interface{}{})
	calls := []orktypes.ExternalCallSpec{
		{Name: "svc", URL: "http://svc.internal/health", ContinueOnError: false},
	}

	_, err := orkexternal.Run(context.Background(), "test", r, calls, nil)
	if err == nil {
		t.Fatal("expected error with continueOnError:false on a failed call")
	}
}

func TestRun_WhenConditionSkipsCall(t *testing.T) {
	called := false
	defer withTransport(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, fmt.Errorf("should not be called")
	}))()

	r := resolver(map[string]interface{}{
		"spec": map[string]interface{}{"env": "staging"},
	})
	calls := []orktypes.ExternalCallSpec{
		{
			Name: "prod-only",
			URL:  "http://prod.internal/v1",
			Conditions: []orktypes.Condition{
				{Field: "spec.env", Equals: "production"},
			},
		},
	}

	_, err := orkexternal.Run(context.Background(), "test", r, calls, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("HTTP call should have been skipped but transport was hit")
	}
}

func TestRun_LaterCallSeesEarlierResult(t *testing.T) {
	// First call returns a URL; second call uses it in its own URL template.
	callCount := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		callCount++
		var body string
		switch callCount {
		case 1:
			body = `{"endpoint":"http://actual.internal/data"}`
		case 2:
			// second call — just confirm it was made to the resolved URL
			body = `{"value":"ok"}`
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	defer withTransport(t, transport)()

	r := resolver(map[string]interface{}{})
	calls := []orktypes.ExternalCallSpec{
		{Name: "discovery", URL: "http://discovery.internal/v1"},
		{Name: "data", URL: `{{ index .external "discovery" "endpoint" }}`},
	}

	got, err := orkexternal.Run(context.Background(), "test", r, calls, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}

	ext := got.Data()["external"].(map[string]interface{})
	if ext["data"] == nil {
		t.Error("expected .external.data to be present")
	}
}

func TestRun_ExpectedStatusMismatch(t *testing.T) {
	defer withTransport(t, stubTransport{status: 201, body: ""})()

	r := resolver(map[string]interface{}{})
	calls := []orktypes.ExternalCallSpec{
		{Name: "api", URL: "http://api.internal/v1", ExpectedStatus: 200, ContinueOnError: false},
	}

	_, err := orkexternal.Run(context.Background(), "test", r, calls, nil)
	if err == nil {
		t.Fatal("expected error when response status doesn't match expectedStatus")
	}
}

func TestRun_JSONStatusKeyDoesNotOverwriteHTTPStatus(t *testing.T) {
	// Body {"status":"ok"} must not overwrite the HTTP status code "200".
	defer withTransport(t, stubTransport{status: 200, body: `{"status":"ok"}`})()

	r := resolver(map[string]interface{}{})
	calls := []orktypes.ExternalCallSpec{
		{Name: "health", URL: "http://svc.internal/health"},
	}

	got, err := orkexternal.Run(context.Background(), "test", r, calls, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ext := got.Data()["external"].(map[string]interface{})
	health := ext["health"].(map[string]interface{})
	if health["status"] != "200" {
		t.Errorf("status: got %v, want 200 — JSON body key must not overwrite HTTP status", health["status"])
	}
}

func TestRun_NonJSONBodyNotParsed(t *testing.T) {
	defer withTransport(t, stubTransport{status: 200, body: "plain text response"})()

	r := resolver(map[string]interface{}{})
	calls := []orktypes.ExternalCallSpec{
		{Name: "txt", URL: "http://txt.internal/v1"},
	}

	got, err := orkexternal.Run(context.Background(), "test", r, calls, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ext := got.Data()["external"].(map[string]interface{})
	entry := ext["txt"].(map[string]interface{})
	if entry["body"] != "plain text response" {
		t.Errorf("body: got %v, want 'plain text response'", entry["body"])
	}
	// no extra keys merged in
	if _, ok := entry["plain"]; ok {
		t.Error("unexpected key merged from non-JSON body")
	}
}

// roundTripFunc allows using a function as an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
