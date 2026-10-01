package intake

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/orkspace/orkestra/pkg/registry/simulate"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/runtime"
)

// fakeSlackClient records PostMessage calls and signals a channel so async
// tests can wait for the background apply to finish instead of sleeping.
type fakeSlackClient struct {
	calls chan string
}

func newFakeSlackClient() *fakeSlackClient {
	return &fakeSlackClient{calls: make(chan string, 1)}
}

func (f *fakeSlackClient) PostMessage(_ /* responseURL */, text string) error {
	f.calls <- text
	return nil
}

func testSlackSource(secret string, commands ...string) ResolvedSlackSource {
	return ResolvedSlackSource{
		Config: orktypes.SlackWebhookConfig{
			Name:     "platform-workspace",
			Enabled:  true,
			Path:     "/webhooks/slack",
			Commands: commands,
		},
		SigningSecret: secret,
	}
}

func slackRequest(t *testing.T, secret string, form url.Values) *http.Request {
	t.Helper()
	body := []byte(form.Encode())
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/slack", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", signSlack(secret, ts, body))
	return req
}

func TestSlackHandler_MethodNotAllowed(t *testing.T) {
	h := NewSlackHandler(testSlackSource("s3cr3t", "/deploy"), nil, nil, nil, orktypes.NoteRegistry{}, newFakeSlackClient())
	req := httptest.NewRequest(http.MethodGet, "/webhooks/slack", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestSlackHandler_InvalidSignature(t *testing.T) {
	h := NewSlackHandler(testSlackSource("s3cr3t", "/deploy"), nil, nil, nil, orktypes.NoteRegistry{}, newFakeSlackClient())
	form := url.Values{"command": {"/deploy"}, "text": {"app"}}
	req := slackRequest(t, "wrong-secret", form)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestSlackHandler_UnknownCommand(t *testing.T) {
	h := NewSlackHandler(testSlackSource("s3cr3t", "/deploy"), nil, nil, nil, orktypes.NoteRegistry{}, newFakeSlackClient())
	form := url.Values{"command": {"/unknown-cmd"}, "text": {"app"}}
	req := slackRequest(t, "s3cr3t", form)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (Slack always gets a 200 ack, even for a rejection)", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Unknown command") {
		t.Errorf("body = %q, want it to mention the unknown command", rr.Body.String())
	}
}

func TestSlackHandler_InvalidArgs(t *testing.T) {
	h := NewSlackHandler(testSlackSource("s3cr3t", "/deploy"), nil, nil, nil, orktypes.NoteRegistry{}, newFakeSlackClient())
	form := url.Values{"command": {"/deploy"}, "text": {""}}
	req := slackRequest(t, "s3cr3t", form)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Invalid arguments") {
		t.Errorf("body = %q, want it to mention invalid arguments", rr.Body.String())
	}
}

func TestSlackHandler_UnknownTarget(t *testing.T) {
	kube := simulate.NewFakeKubeclient(runtime.NewScheme())
	kat := intakeTestKatalog("")
	h := NewSlackHandler(testSlackSource("s3cr3t", "/deploy"), kube, nil, kat, orktypes.NoteRegistry{}, newFakeSlackClient())
	form := url.Values{"command": {"/deploy"}, "text": {"does-not-exist"}}
	req := slackRequest(t, "s3cr3t", form)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Unknown target") {
		t.Errorf("body = %q, want it to mention the unknown target", rr.Body.String())
	}
}

func TestSlackHandler_ValidCommand_AcksThenAppliesInBackground(t *testing.T) {
	kube := simulate.NewFakeKubeclient(runtime.NewScheme())
	kat := intakeTestKatalog("resolved-name") // serve.name declared — no raw name needed
	slack := newFakeSlackClient()
	h := NewSlackHandler(testSlackSource("s3cr3t", "/deploy"), kube, nil, kat, orktypes.NoteRegistry{}, slack)

	form := url.Values{
		"command":      {"/deploy"},
		"text":         {"apprequest"},
		"response_url": {"https://hooks.slack.test/response"},
	}
	req := slackRequest(t, "s3cr3t", form)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Deploying apprequest") {
		t.Errorf("ack body = %q, want it to mention deploying apprequest", rr.Body.String())
	}

	// The fake dynamic client's SSA patch can't create a not-yet-existing
	// object (a client-go fake-client limitation, not something under test
	// here), so this only asserts the background apply reached the SSA
	// layer — not that the patch itself succeeded against the fake client.
	select {
	case msg := <-slack.calls:
		for _, rejection := range []string{"unauthorized", "Unknown command", "Unknown target", "Invalid arguments", "name is required", "namespace is required"} {
			if strings.Contains(msg, rejection) {
				t.Errorf("background apply was rejected before reaching SSA: %q", msg)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the background apply to post back to slack")
	}
}
