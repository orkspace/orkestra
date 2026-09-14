package validate

import (
	"strings"
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

func aliasEntry(kind, target string, aliases map[string]*orktypes.ServeTargetConfig) orktypes.CRDEntry {
	e := serveEntry(kind, target)
	if len(aliases) > 0 {
		e = withAliases(e, aliases)
	}
	return e
}

func TestValidateServeAliases_ValidNames(t *testing.T) {
	k := katalogWithGatewayTokens("ci")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"app": aliasEntry("App", "app", map[string]*orktypes.ServeTargetConfig{
			"public":   {},
			"internal": {},
			"v2":       {},
		}),
	})
	if err := k.validateServeAliases(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateServeAliases_InvalidName(t *testing.T) {
	k := katalogWithGatewayTokens()
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"app": aliasEntry("App", "app", map[string]*orktypes.ServeTargetConfig{
			"Public_Alias": {}, // uppercase + underscore — invalid
		}),
	})
	err := k.validateServeAliases()
	if err == nil {
		t.Fatal("expected error for invalid alias name")
	}
	if !strings.Contains(err.Error(), "Public_Alias") {
		t.Errorf("error should mention the invalid name, got: %v", err)
	}
}

func TestValidateServeAliases_CollidesWithPrimaryTarget(t *testing.T) {
	k := katalogWithGatewayTokens()
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"app":      serveEntry("App", "app"),
		"smartapp": aliasEntry("SmartApp", "smartapp", map[string]*orktypes.ServeTargetConfig{"app": {}}),
	})
	err := k.validateServeAliases()
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), "app") {
		t.Errorf("error should mention colliding name: %v", err)
	}
}

func TestValidateServeAliases_CollidesWithOtherAlias(t *testing.T) {
	k := katalogWithGatewayTokens()
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"app":      aliasEntry("App", "app", map[string]*orktypes.ServeTargetConfig{"shared": {}}),
		"smartapp": aliasEntry("SmartApp", "smartapp", map[string]*orktypes.ServeTargetConfig{"shared": {}}),
	})
	err := k.validateServeAliases()
	if err == nil {
		t.Fatal("expected alias name collision error")
	}
}

func TestValidateServeAliases_AliasTokenMustExistInCRDTokens(t *testing.T) {
	// CRD has token restrictions — alias token not in CRD tokens.
	k := katalogWithGatewayTokens("ci", "control-center")
	crd := aliasEntry("App", "app", map[string]*orktypes.ServeTargetConfig{
		"public": {
			Tokens: map[string]orktypes.ServeTokenPermissions{
				"control-center": {}, // not in CRD's serve.tokens
			},
		},
	})
	crd.Serve.Tokens = map[string]orktypes.ServeTokenPermissions{
		"ci": {}, // only ci allowed at CRD level
	}
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{"app": crd})
	err := k.validateServeAliases()
	if err == nil {
		t.Fatal("expected error: alias token not in CRD tokens")
	}
	if !strings.Contains(err.Error(), "control-center") {
		t.Errorf("error should mention the unknown token, got: %v", err)
	}
}

func TestValidateServeAliases_AliasTokenSubsetOfCRDTokens_Valid(t *testing.T) {
	k := katalogWithGatewayTokens("ci", "control-center")
	crd := aliasEntry("App", "app", map[string]*orktypes.ServeTargetConfig{
		"public": {
			Tokens: map[string]orktypes.ServeTokenPermissions{
				"ci": {}, // ci is in CRD tokens — valid
			},
		},
	})
	crd.Serve.Tokens = map[string]orktypes.ServeTokenPermissions{
		"ci":             {},
		"control-center": {},
	}
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{"app": crd})
	if err := k.validateServeAliases(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateServeAliases_NoCRDTokenRestrictions_AliasTokenCheckedAgainstGateway(t *testing.T) {
	// CRD has no token restrictions — alias can restrict to any gateway token.
	k := katalogWithGatewayTokens("ci", "control-center")
	crd := aliasEntry("App", "app", map[string]*orktypes.ServeTargetConfig{
		"public": {
			Tokens: map[string]orktypes.ServeTokenPermissions{
				"ci": {},
			},
		},
	})
	// No CRD-level tokens (all gateway tokens implicitly allowed)
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{"app": crd})
	if err := k.validateServeAliases(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateServeAliases_NoCRDTokenRestrictions_UnknownGatewayToken(t *testing.T) {
	k := katalogWithGatewayTokens("ci")
	crd := aliasEntry("App", "app", map[string]*orktypes.ServeTargetConfig{
		"public": {
			Tokens: map[string]orktypes.ServeTokenPermissions{
				"ghost-token": {}, // not in gateway tokens
			},
		},
	})
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{"app": crd})
	err := k.validateServeAliases()
	if err == nil {
		t.Fatal("expected error: token not in gateway tokens")
	}
}

func TestValidateServeAliases_TargetDisabledNoAliases_Warns(t *testing.T) {
	k := katalogWithGatewayTokens()
	crd := serveEntry("App", "app")
	// Disable the primary surface.
	disabled := false
	crd.Serve.Target.Entries["app"].Enabled = &disabled
	// no aliases declared
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{"app": crd})
	if err := k.validateServeAliases(); err != nil {
		t.Errorf("should not error, only warn: %v", err)
	}
	// warning should have been recorded
	entry := k.k.EnabledCRDs()["app"]
	if !entry.Warnings.HasWarnings() {
		t.Error("expected a warning for unreachable CRD")
	}
}

func TestValidateServeAliases_NoAliases_NoError(t *testing.T) {
	k := katalogWithGatewayTokens()
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"app": serveEntry("App", "app"),
	})
	if err := k.validateServeAliases(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
