package validate

import (
	"strings"
	"testing"

	"github.com/orkspace/orkestra/pkg/katalog"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// Helper to create a Katalog with gateway tokens
func katalogWithGatewayTokens(tokens ...string) *executor {
	applyTokens := make([]orktypes.APIToken, len(tokens))
	for i, name := range tokens {
		applyTokens[i] = orktypes.APIToken{Name: name}
	}

	return newExec(&katalog.Katalog{
		Gateway: &orktypes.GatewayConfig{
			Enabled: true,
			API: &orktypes.GatewayAPIConfig{
				Enabled: true,
				Auth: orktypes.APIAuth{
					Tokens: applyTokens,
				},
			},
		},
	})
}

// Helper to create token permissions with global list
func perms(ops ...string) orktypes.ServeTokenPermissions {
	return orktypes.ServeTokenPermissions{
		Permissions: orktypes.ServePermissionSet{
			Global: ops,
		},
	}
}

// Helper to create token permissions with schema and resources lists
func permsWithScopes(schemaOps, resourceOps []string) orktypes.ServeTokenPermissions {
	return orktypes.ServeTokenPermissions{
		Permissions: orktypes.ServePermissionSet{
			Schema:    schemaOps,
			Resources: resourceOps,
		},
	}
}

// Helper to create token permissions with all three lists
func permsWithAll(globalOps, schemaOps, resourceOps []string) orktypes.ServeTokenPermissions {
	return orktypes.ServeTokenPermissions{
		Permissions: orktypes.ServePermissionSet{
			Global:    globalOps,
			Schema:    schemaOps,
			Resources: resourceOps,
		},
	}
}

// Helper to create token permissions with namespaces
func permsWithNamespaces(ops []string, namespaces ...string) orktypes.ServeTokenPermissions {
	return orktypes.ServeTokenPermissions{
		Permissions: orktypes.ServePermissionSet{
			Global: ops,
		},
		Namespaces: namespaces,
	}
}

func TestValidateServeTokenRestrictions_NilServe(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"myresource": {Serve: nil},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTokenRestrictions_NoTokenRestrictions(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"myresource": {Serve: &orktypes.ServeConfig{Enabled: true}},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTokenRestrictions_TokenExists_Global(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline", "control-center")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": perms("get", "list"),
				},
			},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTokenRestrictions_TokenExists_Scoped(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline", "control-center")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithScopes(
						[]string{"get"},              // schema
						[]string{"create", "update"}, // resources
					),
				},
			},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTokenRestrictions_TokenExists_GlobalWithScopes(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline", "control-center")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithAll(
						[]string{"get", "list", "create", "update"}, // global
						[]string{"get"},              // schema (subset)
						[]string{"create", "update"}, // resources (subset)
					),
				},
			},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTokenRestrictions_TokenNotFound(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"unknown-token": perms("get"),
				},
			},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error for unknown token")
	}
	if !strings.Contains(err.Error(), "unknown-token") {
		t.Errorf("error should mention the unknown token, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ci-pipeline") {
		t.Errorf("error should list available tokens, got: %v", err)
	}
}

func TestValidateServeTokenRestrictions_InvalidOperation_Global(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": perms("get", "invalid-op"),
				},
			},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error for invalid operation")
	}
	if !strings.Contains(err.Error(), "invalid-op") {
		t.Errorf("error should mention the invalid operation, got: %v", err)
	}
}

func TestValidateServeTokenRestrictions_InvalidOperation_Schema(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithScopes(
						[]string{"invalid-op"}, // schema
						[]string{"create"},     // resources
					),
				},
			},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error for invalid schema operation")
	}
	if !strings.Contains(err.Error(), "invalid-op") {
		t.Errorf("error should mention the invalid operation, got: %v", err)
	}
}

func TestValidateServeTokenRestrictions_InvalidOperation_Resources(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithScopes(
						[]string{"get"},        // schema
						[]string{"invalid-op"}, // resources
					),
				},
			},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error for invalid resources operation")
	}
	if !strings.Contains(err.Error(), "invalid-op") {
		t.Errorf("error should mention the invalid operation, got: %v", err)
	}
}

func TestValidateServeTokenRestrictions_DuplicateOperations(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": perms("get", "list", "get"),
				},
			},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error for duplicate operations")
	}
	if !strings.Contains(err.Error(), "get") {
		t.Errorf("error should mention the duplicate operation, got: %v", err)
	}
}

func TestValidateServeTokenRestrictions_SchemaNotSubsetOfGlobal(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithAll(
						[]string{"get", "list"}, // global
						[]string{"delete"},      // schema (not in global)
						[]string{"get"},         // resources
					),
				},
			},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error when schema is not subset of global")
	}
	if !strings.Contains(err.Error(), "delete") {
		t.Errorf("error should mention the missing operation, got: %v", err)
	}
}

func TestValidateServeTokenRestrictions_ResourcesNotSubsetOfGlobal(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithAll(
						[]string{"get", "list"}, // global
						[]string{"get"},         // schema
						[]string{"delete"},      // resources (not in global)
					),
				},
			},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error when resources is not subset of global")
	}
	if !strings.Contains(err.Error(), "delete") {
		t.Errorf("error should mention the missing operation, got: %v", err)
	}
}

func TestValidateServeTokenRestrictions_ClassWildcardWithoutGlobalWildcard(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithAll(
						[]string{"get", "list"}, // global
						[]string{"*"},           // schema wildcard (broader than global)
						[]string{"get"},         // resources
					),
				},
			},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error when class wildcard exceeds global")
	}
	if !strings.Contains(err.Error(), "*") {
		t.Errorf("error should mention wildcard, got: %v", err)
	}
}

func TestValidateServeTokenRestrictions_GlobalWildcard(t *testing.T) {
	k := katalogWithGatewayTokens("admin")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"admin": perms("*"),
				},
			},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTokenRestrictions_GlobalWildcardWithScopes(t *testing.T) {
	// When global has "*", class-specific scopes should be ignored.
	// The validation should not error because global overrides everything.
	k := katalogWithGatewayTokens("admin")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"admin": permsWithAll(
						[]string{"*"},      // global wildcard
						[]string{"get"},    // schema (ignored)
						[]string{"create"}, // resources (ignored)
					),
				},
			},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTokenRestrictions_NamespaceAllowed(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithNamespaces(
						[]string{"get"},
						"staging",
					),
				},
			},
			AllowedNamespaces: []string{"staging", "production"},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTokenRestrictions_NamespaceNotAllowed(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithNamespaces(
						[]string{"get"},
						"unauthorized",
					),
				},
			},
			AllowedNamespaces: []string{"staging", "production"},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error for unauthorized namespace")
	}
	if !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("error should mention the unauthorized namespace, got: %v", err)
	}
	if !strings.Contains(err.Error(), "staging") {
		t.Errorf("error should list allowed namespaces, got: %v", err)
	}
}

func TestValidateServeTokenRestrictions_RestrictedNamespace(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithNamespaces(
						[]string{"get"},
						"restricted-ns",
					),
				},
			},
			RestrictedNamespaces: []string{"restricted-ns", "blocked"},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error for restricted namespace")
	}
	if !strings.Contains(err.Error(), "restricted-ns") {
		t.Errorf("error should mention the restricted namespace, got: %v", err)
	}
}

func TestValidateServeTokenRestrictions_MultipleTokens(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline", "control-center")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline":    perms("get", "list"),
					"control-center": perms("*"),
				},
			},
			AllowedNamespaces: []string{"staging", "production"},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTokenRestrictions_WildcardOperation(t *testing.T) {
	k := katalogWithGatewayTokens("admin")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"admin": perms("*"),
				},
			},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateServeTokenRestrictions_GatewayNotConfigured(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": perms("get"),
				},
			},
			Warnings: orktypes.Warnings{},
		},
	})
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error when gateway is not configured but serve tokens are defined")
	}
}

func TestValidateServeTokenRestrictions_GatewayAuthEmpty(t *testing.T) {
	k := newKatalogExec(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": perms("get"),
				},
			},
			Warnings: orktypes.Warnings{},
		},
	})
	k.k.Gateway = &orktypes.GatewayConfig{
		API: &orktypes.GatewayAPIConfig{
			Auth: orktypes.APIAuth{
				Tokens: []orktypes.APIToken{},
			},
		},
	}
	err := k.validateServeTokenRestrictions()
	if err == nil {
		t.Fatal("expected error when gateway auth tokens are empty but serve tokens are defined")
	}
}

func TestGatewayTokenNames(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline", "control-center")
	names := k.k.GatewayTokenNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 tokens, got %d: %v", len(names), names)
	}
	if names[0] != "ci-pipeline" || names[1] != "control-center" {
		t.Errorf("unexpected token names: %v", names)
	}
}

func TestGatewayTokenNames_WithTokens(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline", "control-center")
	names := k.k.GatewayTokenNames()

	if len(names) != 2 {
		t.Fatalf("expected 2 tokens, got %d: %v", len(names), names)
	}
	expected := []string{"ci-pipeline", "control-center"}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("expected %q, got %q", name, names[i])
		}
	}
}

func TestGatewayTokenNames_NoTokens(t *testing.T) {
	k := katalogWithGatewayTokens() // no tokens
	names := k.k.GatewayTokenNames()
	if names != nil {
		t.Errorf("expected nil, got %v", names)
	}
}

func TestGatewayTokenNames_GatewayDisabled(t *testing.T) {
	// Gateway exists but API is disabled
	k := newExec(&katalog.Katalog{
		Gateway: &orktypes.GatewayConfig{
			API: &orktypes.GatewayAPIConfig{
				Enabled: false,
				Auth: orktypes.APIAuth{
					Tokens: []orktypes.APIToken{
						{Name: "ci-pipeline"},
					},
				},
			},
		},
	})
	names := k.k.GatewayTokenNames()
	if names != nil {
		t.Errorf("expected nil when API disabled, got %v", names)
	}
}

func TestValidateServeTokenRestrictions_EmptyPermissionsWarning(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": {
						Permissions: orktypes.ServePermissionSet{},
					},
				},
			},
			Warnings: orktypes.Warnings{},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	crd := k.k.EnabledCRDs()["myresource"]
	if !crd.Warnings.HasWarnings() {
		t.Fatal("expected warning for empty permissions")
	}
}

func TestValidateServeTokenRestrictions_ClusterScopedCRDWithNamespaces(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	falsePtr := false
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"clusterresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					"ci-pipeline": permsWithNamespaces(
						[]string{"get"},
						"staging",
					),
				},
			},
			Namespaced: &falsePtr,
			Warnings:   orktypes.Warnings{},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	crd := k.k.EnabledCRDs()["clusterresource"]
	if !crd.Warnings.HasWarnings() {
		t.Fatal("expected warning for namespace restrictions on cluster-scoped CRD")
	}
}

func TestValidateServeTokenRestrictions_SchemaInheritsInvalidGlobalOpWarning(t *testing.T) {
	k := katalogWithGatewayTokens("ci-pipeline")
	k.k.SetEnabledCRDsForTest(map[string]orktypes.CRDEntry{
		"myresource": {
			Serve: &orktypes.ServeConfig{
				Tokens: map[string]orktypes.ServeTokenPermissions{
					// No explicit schema list — schema inherits from global,
					// but "create" isn't valid for schema endpoints.
					"ci-pipeline": perms("get", "create"),
				},
			},
			Warnings: orktypes.Warnings{},
		},
	})
	if err := k.validateServeTokenRestrictions(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	crd := k.k.EnabledCRDs()["myresource"]
	if !crd.Warnings.HasWarnings() {
		t.Fatal("expected warning for global op not valid on schema endpoints")
	}
}
