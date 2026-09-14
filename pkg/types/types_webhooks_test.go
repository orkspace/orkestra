package types

import "testing"

func TestGatewayWebhookConfig_IsEmpty(t *testing.T) {
	cases := []struct {
		name string
		cfg  *GatewayWebhookConfig
		want bool
	}{
		{"nil", nil, true},
		{"zero value", &GatewayWebhookConfig{}, true},
		{"only include set, no entries yet", &GatewayWebhookConfig{Include: "./shared/webhooks.yaml"}, true},
		{"one github entry", &GatewayWebhookConfig{GitHub: []GitWebhookConfig{{Name: "payments-repo"}}}, false},
		{"one gitlab entry", &GatewayWebhookConfig{GitLab: []GitWebhookConfig{{Name: "orders-repo"}}}, false},
		{"one slack entry", &GatewayWebhookConfig{Slack: []SlackWebhookConfig{{Name: "platform-workspace"}}}, false},
		{"one generic entry", &GatewayWebhookConfig{Generic: []GenericWebhookConfig{{Name: "pagerduty"}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Empty(); got != tc.want {
				t.Errorf("Empty) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGitWebhookConfig_HasSecretRef(t *testing.T) {
	var nilEntry *GitWebhookConfig
	if nilEntry.HasSecretRef() {
		t.Error("nil receiver: HasSecretRef() = true, want false")
	}
	if (&GitWebhookConfig{}).HasSecretRef() {
		t.Error("no SecretRef: HasSecretRef() = true, want false")
	}
	if !(&GitWebhookConfig{SecretRef: &APISecretRef{Name: "x", Key: "y"}}).HasSecretRef() {
		t.Error("SecretRef set: HasSecretRef() = false, want true")
	}
}

func TestGitWebhookConfig_HasContentTokenRef(t *testing.T) {
	var nilEntry *GitWebhookConfig
	if nilEntry.HasContentTokenRef() {
		t.Error("nil receiver: HasContentTokenRef() = true, want false")
	}
	if (&GitWebhookConfig{}).HasContentTokenRef() {
		t.Error("no ContentTokenRef: HasContentTokenRef() = true, want false")
	}
	if !(&GitWebhookConfig{ContentTokenRef: &APISecretRef{Name: "x", Key: "y"}}).HasContentTokenRef() {
		t.Error("ContentTokenRef set: HasContentTokenRef() = false, want true")
	}
}

func TestSlackWebhookConfig_HasSigningSecretRef(t *testing.T) {
	var nilEntry *SlackWebhookConfig
	if nilEntry.HasSigningSecretRef() {
		t.Error("nil receiver: HasSigningSecretRef() = true, want false")
	}
	if (&SlackWebhookConfig{}).HasSigningSecretRef() {
		t.Error("no SigningSecretRef: HasSigningSecretRef() = true, want false")
	}
	if !(&SlackWebhookConfig{SigningSecretRef: &APISecretRef{Name: "x", Key: "y"}}).HasSigningSecretRef() {
		t.Error("SigningSecretRef set: HasSigningSecretRef() = false, want true")
	}
}

func TestGenericWebhookConfig_HasSecretRef(t *testing.T) {
	var nilEntry *GenericWebhookConfig
	if nilEntry.HasSecretRef() {
		t.Error("nil receiver: HasSecretRef() = true, want false")
	}
	if (&GenericWebhookConfig{}).HasSecretRef() {
		t.Error("no SecretRef: HasSecretRef() = true, want false")
	}
	if !(&GenericWebhookConfig{SecretRef: &APISecretRef{Name: "x", Key: "y"}}).HasSecretRef() {
		t.Error("SecretRef set: HasSecretRef() = false, want true")
	}
}

func TestGatewayConfig_HasWebhooks(t *testing.T) {
	var nilGw *GatewayConfig
	if nilGw.HasWebhooks() {
		t.Error("nil receiver: HasWebhooks() = true, want false")
	}
	if (&GatewayConfig{}).HasWebhooks() {
		t.Error("no Webhooks: HasWebhooks() = true, want false")
	}
	if (&GatewayConfig{Webhooks: &GatewayWebhookConfig{}}).HasWebhooks() {
		t.Error("empty Webhooks: HasWebhooks() = true, want false")
	}
	gw := &GatewayConfig{Webhooks: &GatewayWebhookConfig{
		GitHub: []GitWebhookConfig{{Name: "payments-repo"}},
	}}
	if !gw.HasWebhooks() {
		t.Error("one entry: HasWebhooks() = false, want true")
	}
}
