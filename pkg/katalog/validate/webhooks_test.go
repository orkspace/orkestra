package validate

import (
	"testing"

	"github.com/orkspace/orkestra/pkg/katalog"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

func gatewayWithAPI() *orktypes.GatewayConfig {
	return &orktypes.GatewayConfig{
		API: &orktypes.GatewayAPIConfig{Enabled: true},
	}
}

func validGitHubEntry(name, path string) orktypes.GitWebhookConfig {
	return orktypes.GitWebhookConfig{
		Name:            name,
		Enabled:         true,
		Path:            path,
		Branch:          "main",
		SecretRef:       &orktypes.APISecretRef{Name: "s", Key: "secret"},
		ContentTokenRef: &orktypes.APISecretRef{Name: "t", Key: "token"},
	}
}

func TestValidateWebhooks_NoWebhooksDeclared(t *testing.T) {
	k := newExec(&katalog.Katalog{Gateway: gatewayWithAPI()})
	if err := k.validateGatewayWebhooks(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateWebhooks_GatewayNotEnabled(t *testing.T) {
	k := newExec(&katalog.Katalog{})
	if err := k.validateGatewayWebhooks(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateWebhooks_RequiresAPI(t *testing.T) {
	k := newExec(&katalog.Katalog{
		Gateway: &orktypes.GatewayConfig{
			Webhooks: &orktypes.GatewayWebhookConfig{
				GitHub: []orktypes.GitWebhookConfig{validGitHubEntry("payments-repo", "/webhooks/github/payments")},
			},
		},
	})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — webhooks declared without applyAPI.enabled")
	}
}

func TestValidateWebhooks_Valid(t *testing.T) {
	gw := gatewayWithAPI()
	gw.Webhooks = &orktypes.GatewayWebhookConfig{
		GitHub: []orktypes.GitWebhookConfig{validGitHubEntry("payments-repo", "/webhooks/github/payments")},
		GitLab: []orktypes.GitWebhookConfig{validGitHubEntry("orders-repo", "/webhooks/gitlab/orders")},
		Slack: []orktypes.SlackWebhookConfig{{
			Name:             "platform-workspace",
			Enabled:          true,
			Path:             "/webhooks/slack",
			SigningSecretRef: &orktypes.APISecretRef{Name: "s", Key: "secret"},
			Commands:         []string{"/deploy"},
		}},
		Generic: []orktypes.GenericWebhookConfig{{
			Name:      "pagerduty",
			Enabled:   true,
			Path:      "/webhooks/generic/pagerduty",
			SecretRef: &orktypes.APISecretRef{Name: "s", Key: "secret"},
		}},
	}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateWebhooks_DisabledEntrySkipsRequirements(t *testing.T) {
	// A disabled entry doesn't need path/secretRef/etc — only name.
	gw := gatewayWithAPI()
	gw.Webhooks = &orktypes.GatewayWebhookConfig{
		GitHub: []orktypes.GitWebhookConfig{{Name: "payments-repo", Enabled: false}},
	}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err != nil {
		t.Errorf("unexpected error for a disabled entry: %v", err)
	}
}

func TestValidateWebhooks_MissingName(t *testing.T) {
	gw := gatewayWithAPI()
	gw.Webhooks = &orktypes.GatewayWebhookConfig{
		GitHub: []orktypes.GitWebhookConfig{{Path: "/webhooks/github/payments"}},
	}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — entry has no name")
	}
}

func TestValidateWebhooks_DuplicateName(t *testing.T) {
	gw := gatewayWithAPI()
	gw.Webhooks = &orktypes.GatewayWebhookConfig{
		GitHub: []orktypes.GitWebhookConfig{
			{Name: "payments-repo", Enabled: false},
			{Name: "payments-repo", Enabled: false},
		},
	}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — duplicate entry name")
	}
}

func TestValidateWebhooks_DuplicateNameAcrossSources(t *testing.T) {
	gw := gatewayWithAPI()
	gw.Webhooks = &orktypes.GatewayWebhookConfig{
		GitHub: []orktypes.GitWebhookConfig{{Name: "payments-repo", Enabled: false}},
		Slack:  []orktypes.SlackWebhookConfig{{Name: "payments-repo", Enabled: false}},
	}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — same name reused across two different sources")
	}
}

func TestValidateWebhooks_MissingPath(t *testing.T) {
	gw := gatewayWithAPI()
	e := validGitHubEntry("payments-repo", "")
	gw.Webhooks = &orktypes.GatewayWebhookConfig{GitHub: []orktypes.GitWebhookConfig{e}}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — enabled entry has no path")
	}
}

func TestValidateWebhooks_DuplicatePathAcrossSources(t *testing.T) {
	gw := gatewayWithAPI()
	gw.Webhooks = &orktypes.GatewayWebhookConfig{
		GitHub: []orktypes.GitWebhookConfig{validGitHubEntry("payments-repo", "/webhooks/shared")},
		GitLab: []orktypes.GitWebhookConfig{validGitHubEntry("orders-repo", "/webhooks/shared")},
	}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — same path claimed by two entries across different sources")
	}
}

func TestValidateWebhooks_MissingSecretRef(t *testing.T) {
	gw := gatewayWithAPI()
	e := validGitHubEntry("payments-repo", "/webhooks/github/payments")
	e.SecretRef = nil
	gw.Webhooks = &orktypes.GatewayWebhookConfig{GitHub: []orktypes.GitWebhookConfig{e}}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — enabled entry has no secretRef")
	}
}

func TestValidateWebhooks_MissingContentTokenRef(t *testing.T) {
	gw := gatewayWithAPI()
	e := validGitHubEntry("payments-repo", "/webhooks/github/payments")
	e.ContentTokenRef = nil
	gw.Webhooks = &orktypes.GatewayWebhookConfig{GitHub: []orktypes.GitWebhookConfig{e}}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — git source entry has no contentTokenRef")
	}
}

func TestValidateWebhooks_MissingBranch(t *testing.T) {
	gw := gatewayWithAPI()
	e := validGitHubEntry("payments-repo", "/webhooks/github/payments")
	e.Branch = ""
	gw.Webhooks = &orktypes.GatewayWebhookConfig{GitHub: []orktypes.GitWebhookConfig{e}}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — git source entry has no branch")
	}
}

func TestValidateWebhooks_Slack_MissingSigningSecretRef(t *testing.T) {
	gw := gatewayWithAPI()
	gw.Webhooks = &orktypes.GatewayWebhookConfig{
		Slack: []orktypes.SlackWebhookConfig{{
			Name:     "platform-workspace",
			Enabled:  true,
			Path:     "/webhooks/slack",
			Commands: []string{"/deploy"},
		}},
	}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — slack entry has no signingSecretRef")
	}
}

func TestValidateWebhooks_Slack_MissingCommands(t *testing.T) {
	gw := gatewayWithAPI()
	gw.Webhooks = &orktypes.GatewayWebhookConfig{
		Slack: []orktypes.SlackWebhookConfig{{
			Name:             "platform-workspace",
			Enabled:          true,
			Path:             "/webhooks/slack",
			SigningSecretRef: &orktypes.APISecretRef{Name: "s", Key: "secret"},
		}},
	}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — slack entry has no commands")
	}
}

func TestValidateWebhooks_Generic_MissingSecretRef(t *testing.T) {
	gw := gatewayWithAPI()
	gw.Webhooks = &orktypes.GatewayWebhookConfig{
		Generic: []orktypes.GenericWebhookConfig{{
			Name:    "pagerduty",
			Enabled: true,
			Path:    "/webhooks/generic/pagerduty",
		}},
	}
	k := newExec(&katalog.Katalog{Gateway: gw})
	if err := k.validateGatewayWebhooks(); err == nil {
		t.Fatal("expected an error — generic entry has no secretRef")
	}
}
