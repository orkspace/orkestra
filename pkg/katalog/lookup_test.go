package katalog

import (
	"strings"
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Helper to create a test CRD entry with all required fields
func testCRDEntry(kind, target string, serveEnabled bool) orktypes.CRDEntry {
	entry := orktypes.CRDEntry{
		APITypes: orktypes.APITypes{
			Group:   "platform.myorg.io",
			Version: "v1",
			Kind:    kind,
			Plural:  kind + "s",
		},
	}
	if serveEnabled || target != "" {
		tv := orktypes.ServeTargetValue{}
		if target != "" {
			tv.Entries = map[string]*orktypes.ServeTargetConfig{
				target: {Primary: true},
			}
		}
		entry.Serve = &orktypes.ServeConfig{
			Enabled: true,
			Target:  tv,
		}
	}
	return entry
}

func TestBuildIndexes(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app":      testCRDEntry("App", "smartapp", true),
			"database": testCRDEntry("Database", "db", true),
		},
	}

	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("setGroupVersionKind: %v", err)
	}

	// Test kind index — stored lowercase for case-insensitive lookups.
	if name, ok := k.kindIndex["app"]; !ok || name != "app" {
		t.Errorf("kindIndex: expected 'app', got '%s'", name)
	}
	if name, ok := k.kindIndex["database"]; !ok || name != "database" {
		t.Errorf("kindIndex: expected 'database', got '%s'", name)
	}

	// Test target index
	if name, ok := k.targetIndex["smartapp"]; !ok || name != "app" {
		t.Errorf("targetIndex: expected 'app', got '%s'", name)
	}
	if name, ok := k.targetIndex["db"]; !ok || name != "database" {
		t.Errorf("targetIndex: expected 'database', got '%s'", name)
	}

	// Test name index — must handle camelCase keys (the "platRsc" bug)
	if name, ok := k.nameIndex["app"]; !ok || name != "app" {
		t.Errorf("nameIndex: expected 'app', got '%s'", name)
	}
	if name, ok := k.nameIndex["database"]; !ok || name != "database" {
		t.Errorf("nameIndex: expected 'database', got '%s'", name)
	}

	// Test plural index
	if name, ok := k.pluralIndex["apps"]; !ok || name != "app" {
		t.Errorf("pluralIndex: expected 'app' for 'apps', got '%s'", name)
	}
	if name, ok := k.pluralIndex["databases"]; !ok || name != "database" {
		t.Errorf("pluralIndex: expected 'database' for 'databases', got '%s'", name)
	}

	// Test GVK index
	appGVK := schema.GroupVersionKind{
		Group:   "platform.myorg.io",
		Version: "v1",
		Kind:    "App",
	}
	if name, ok := k.gvkIndex[strings.ToLower(appGVK.String())]; !ok || name != "app" {
		t.Errorf("gvkIndex: expected 'app', got '%s'", name)
	}

	// Test GVR index
	appGVR := schema.GroupVersionResource{
		Group:    "platform.myorg.io",
		Version:  "v1",
		Resource: "apps",
	}
	if name, ok := k.gvrIndex[strings.ToLower(appGVR.String())]; !ok || name != "app" {
		t.Errorf("gvrIndex: expected 'app', got '%s'", name)
	}
}

func TestBuildAllIDEnabledCRDs(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app":      testCRDEntry("App", "smartapp", true),
			"database": testCRDEntry("Database", "", false),
			"cache": {
				APITypes: orktypes.APITypes{Kind: "Cache"},
				Serve:    nil,
			},
		},
	}

	serveEnabledCrds := k.BuildServeEnabledCRDs()

	if len(serveEnabledCrds) != 1 {
		t.Fatalf("expected 1 serve-enabled CRD, got %d", len(serveEnabledCrds))
	}

	if serveEnabledCrds[0].APITypes.Kind != "App" {
		t.Errorf("expected App, got %s", serveEnabledCrds[0].APITypes.Kind)
	}
	if serveEnabledCrds[0].ServeTarget() != "smartapp" {
		t.Errorf("expected target smartapp, got %s", serveEnabledCrds[0].ServeTarget())
	}
}

func TestLookupByKind(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app":      testCRDEntry("App", "", false),
			"database": testCRDEntry("Database", "", false),
		},
	}
	k.SetGroupVersionKind()

	tests := []struct {
		name     string
		kind     string
		expected string
	}{
		{"found", "App", "app"},
		{"found", "Database", "database"},
		{"not found", "Unknown", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := k.LookupByKind(tt.kind).Entry()
			if tt.expected == "" {
				if crd != nil {
					t.Errorf("expected nil, got %+v", crd)
				}
				return
			}
			if crd == nil {
				t.Fatalf("expected CRD, got nil")
			}
			if crd.APITypes.Kind != tt.kind {
				t.Errorf("expected kind %s, got %s", tt.kind, crd.APITypes.Kind)
			}
		})
	}
}

func TestLookupByTarget(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app":      testCRDEntry("App", "smartapp", true),
			"database": testCRDEntry("Database", "db", true),
		},
	}
	k.SetGroupVersionKind()

	tests := []struct {
		name     string
		target   string
		expected string
	}{
		{"found", "smartapp", "App"},
		{"found", "db", "Database"},
		{"not found", "unknown", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := k.LookupByTarget(tt.target).Entry()
			if tt.expected == "" {
				if crd != nil {
					t.Errorf("expected nil, got %+v", crd)
				}
				return
			}
			if crd == nil {
				t.Fatalf("expected CRD, got nil")
			}
			if crd.APITypes.Kind != tt.expected {
				t.Errorf("expected kind %s, got %s", tt.expected, crd.APITypes.Kind)
			}
		})
	}
}

func TestLookupByName(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app":      testCRDEntry("App", "smartapp", true),
			"database": testCRDEntry("Database", "db", true),
			// camelCase key — this was the production bug: LookupByName lowercased
			// the input but looked up directly in enabledCRDs (also camelCase keyed),
			// so "platRsc" → lowercase "platrsc" → miss. nameIndex fixes it.
			"platRsc": testCRDEntry("PlatformResource", "apifixture", true),
		},
	}
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("setGroupVersionKind: %v", err)
	}

	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{"exact lowercase match", "app", "App"},
		{"case-insensitive", "Database", "Database"},
		{"whitespace trimmed", "  app  ", "App"},
		{"camelCase key exact", "platRsc", "PlatformResource"},
		{"camelCase key lowercased", "platrsc", "PlatformResource"},
		{"camelCase key uppercased", "PLATRSC", "PlatformResource"},
		{"not found", "unknown", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := k.LookupByName(tt.query).Entry()
			if tt.expected == "" {
				if crd != nil {
					t.Errorf("expected nil, got %+v", crd)
				}
				return
			}
			if crd == nil {
				t.Fatalf("expected CRD, got nil")
			}
			if crd.APITypes.Kind != tt.expected {
				t.Errorf("expected kind %s, got %s", tt.expected, crd.APITypes.Kind)
			}
		})
	}
}

func TestLookupByPlural(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": testCRDEntry("App", "smartapp", true),
			// testCRDEntry sets Plural = Kind + "s", so "Apps"
			"platRsc": {
				APITypes: orktypes.APITypes{
					Group:   "gateway.fixture.orkestra.io",
					Version: "v1alpha1",
					Kind:    "PlatformResource",
					Plural:  "platformresources",
				},
				Serve: &orktypes.ServeConfig{
					Enabled: true,
					Target: orktypes.ServeTargetValue{
						Entries: map[string]*orktypes.ServeTargetConfig{
							"apifixture": {Primary: true},
						},
					},
				},
			},
		},
	}
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("setGroupVersionKind: %v", err)
	}

	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{"exact lowercase", "platformresources", "PlatformResource"},
		{"case-insensitive", "PlatformResources", "PlatformResource"},
		{"testCRDEntry plural (Kind+s)", "Apps", "App"},
		{"testCRDEntry plural lowercase", "apps", "App"},
		{"whitespace trimmed", "  platformresources  ", "PlatformResource"},
		{"not found", "widgets", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := k.LookupByPlural(tt.query).Entry()
			if tt.expected == "" {
				if crd != nil {
					t.Errorf("expected nil, got %+v", crd)
				}
				return
			}
			if crd == nil {
				t.Fatalf("LookupByPlural(%q): expected CRD, got nil", tt.query)
			}
			if crd.APITypes.Kind != tt.expected {
				t.Errorf("expected kind %s, got %s", tt.expected, crd.APITypes.Kind)
			}
		})
	}
}

func TestLookupByTargetOrKind(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app":      testCRDEntry("App", "smartapp", true),
			"database": testCRDEntry("Database", "db", true),
			"cache":    testCRDEntry("Cache", "", false),
		},
	}
	k.SetGroupVersionKind()

	tests := []struct {
		name       string
		identifier string
		expected   string
	}{
		{"target match", "smartapp", "App"},
		{"target match", "db", "Database"},
		{"kind match", "Cache", "Cache"},
		{"kind match", "App", "App"},
		{"not found", "unknown", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := k.LookupByTargetOrKind(tt.identifier).Entry()
			if tt.expected == "" {
				if crd != nil {
					t.Errorf("expected nil, got %+v", crd)
				}
				return
			}
			if crd == nil {
				t.Fatalf("expected CRD, got nil")
			}
			if crd.APITypes.Kind != tt.expected {
				t.Errorf("expected kind %s, got %s", tt.expected, crd.APITypes.Kind)
			}
		})
	}
}

func TestLookupByGVKString(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v1",
					Kind:    "App",
					Plural:  "apps",
				},
			},
		},
	}
	// set defaults
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("failed to set defaults: %v", err)
	}
	k.SetGroupVersionKind()

	// Use the exact format that GVKString() returns
	gvk := schema.GroupVersionKind{
		Group:   "platform.myorg.io",
		Version: "v1",
		Kind:    "App",
	}

	// Debug: print what the index keys are
	t.Logf("Index keys: %v", k.gvkIndex)
	t.Logf("GVK.String(): %q", gvk.String())

	crd := k.LookupByGVKString(gvk.String()).Entry()
	if crd == nil {
		t.Fatal("expected CRD, got nil")
	}
	if crd.APITypes.Kind != "App" {
		t.Errorf("expected App, got %s", crd.APITypes.Kind)
	}

	// Test not found
	unknownGVK := schema.GroupVersionKind{
		Group:   "unknown.io",
		Version: "v1",
		Kind:    "Unknown",
	}
	crd = k.LookupByGVKString(unknownGVK.String()).Entry()
	if crd != nil {
		t.Errorf("expected nil, got %+v", crd)
	}
}

func TestLookupByGVRString(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v1",
					Kind:    "App",
					Plural:  "apps",
				},
			},
		},
	}
	// set defaults
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("failed to set defaults: %v", err)
	}
	k.SetGroupVersionKind()

	gvr := schema.GroupVersionResource{
		Group:    "platform.myorg.io",
		Version:  "v1",
		Resource: "apps",
	}

	// Debug: print what the index keys are
	t.Logf("Index keys: %v", k.gvrIndex)
	t.Logf("GVR.String(): %q", gvr.String())

	crd := k.LookupByGVRString(gvr.String()).Entry()
	if crd == nil {
		t.Fatal("expected CRD, got nil")
	}
	if crd.APITypes.Kind != "App" {
		t.Errorf("expected App, got %s", crd.APITypes.Kind)
	}

	// Test not found
	unknownGVR := schema.GroupVersionResource{
		Group:    "unknown.io",
		Version:  "v1",
		Resource: "unknowns",
	}
	crd = k.LookupByGVRString(unknownGVR.String()).Entry()
	if crd != nil {
		t.Errorf("expected nil, got %+v", crd)
	}
}

func TestMustLookupByTarget(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": testCRDEntry("App", "smartapp", true),
		},
	}
	k.SetGroupVersionKind()

	// Should not panic
	crd := k.MustLookupByTarget("smartapp").Entry()
	if crd == nil {
		t.Fatal("expected CRD, got nil")
	}
	if crd.APITypes.Kind != "App" {
		t.Errorf("expected App, got %s", crd.APITypes.Kind)
	}

	// Should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown target")
		}
	}()
	k.MustLookupByTarget("unknown")
}

func TestMustLookupByKind(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v1",
					Kind:    "App",
					Plural:  "apps",
				},
			},
		},
	}
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("setGroupVersionKind: %v", err)
	}

	// Should not panic
	crd := k.MustLookupByKind("App").Entry()
	if crd == nil {
		t.Fatal("expected CRD, got nil")
	}
	if crd.APITypes.Kind != "App" {
		t.Errorf("expected App, got %s", crd.APITypes.Kind)
	}

	// Should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown kind")
		}
	}()
	k.MustLookupByKind("Unknown")
}

func TestIsKindRegistered(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": testCRDEntry("App", "", false),
		},
	}
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("setGroupVersionKind: %v", err)
	}

	if !k.IsKindRegistered("App") {
		t.Error("expected App to be registered")
	}
	if k.IsKindRegistered("Unknown") {
		t.Error("expected Unknown to not be registered")
	}
}

func TestIsTargetRegistered(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": testCRDEntry("App", "smartapp", true),
		},
	}
	k.SetGroupVersionKind()

	if !k.IsTargetRegistered("smartapp") {
		t.Error("expected smartapp to be registered")
	}
	if k.IsTargetRegistered("unknown") {
		t.Error("expected unknown to not be registered")
	}
}

func TestLookupWebhookSource(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": testCRDEntry("App", "smartapp", true),
		},
		Gateway: &orktypes.GatewayConfig{
			Webhooks: &orktypes.GatewayWebhookConfig{
				GitHub:  []orktypes.GitWebhookConfig{{Name: "payments-repo"}},
				GitLab:  []orktypes.GitWebhookConfig{{Name: "payments-repo-gitlab"}},
				Slack:   []orktypes.SlackWebhookConfig{{Name: "platform-workspace"}},
				Generic: []orktypes.GenericWebhookConfig{{Name: "pagerduty"}},
			},
		},
	}
	k.SetGroupVersionKind()

	tests := []struct {
		name   string
		lookup string
		want   string
		found  bool
	}{
		{"github", "payments-repo", "github", true},
		{"gitlab", "payments-repo-gitlab", "gitlab", true},
		{"slack", "platform-workspace", "slack", true},
		{"generic", "pagerduty", "generic", true},
		{"case-insensitive", "PAGERDUTY", "generic", true},
		{"unknown", "does-not-exist", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := k.LookupWebhookSource(tt.lookup)
			if ok != tt.found || got != tt.want {
				t.Errorf("LookupWebhookSource(%q) = (%q, %v), want (%q, %v)", tt.lookup, got, ok, tt.want, tt.found)
			}
		})
	}
}

func TestLookupWebhookSource_NoWebhooksConfigured(t *testing.T) {
	k := &Katalog{enabledCRDs: map[string]orktypes.CRDEntry{"app": testCRDEntry("App", "smartapp", true)}}
	k.SetGroupVersionKind()

	if _, ok := k.LookupWebhookSource("anything"); ok {
		t.Error("expected no match when no webhooks are configured")
	}
}

func TestListTargets(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app":      testCRDEntry("App", "smartapp", true),
			"database": testCRDEntry("Database", "db", true),
		},
	}
	k.SetGroupVersionKind()

	targets := k.ListTargets()
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	found := make(map[string]bool)
	for _, t := range targets {
		found[t] = true
	}
	if !found["smartapp"] {
		t.Error("expected smartapp in targets")
	}
	if !found["db"] {
		t.Error("expected db in targets")
	}
}

func TestListKinds(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app":      testCRDEntry("App", "", false),
			"database": testCRDEntry("Database", "", false),
		},
	}
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("setGroupVersionKind: %v", err)
	}

	kinds := k.ListKinds()
	if len(kinds) != 2 {
		t.Fatalf("expected 2 kinds, got %d", len(kinds))
	}

	found := make(map[string]bool)
	for _, k := range kinds {
		found[k] = true
	}
	// ListKinds reads kindIndex, which is stored lowercase.
	if !found["app"] {
		t.Error("expected app in kinds")
	}
	if !found["database"] {
		t.Error("expected database in kinds")
	}
}

func TestLookup_EmptyKatalog(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{},
	}
	k.SetGroupVersionKind()

	if crd := k.LookupByKind("App").Entry(); crd != nil {
		t.Errorf("expected nil, got %+v", crd)
	}
	if crd := k.LookupByTarget("smartapp").Entry(); crd != nil {
		t.Errorf("expected nil, got %+v", crd)
	}
	if crd := k.LookupByTargetOrKind("App").Entry(); crd != nil {
		t.Errorf("expected nil, got %+v", crd)
	}
}

func TestLookup_CRDWithoutServe(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"cache": testCRDEntry("Cache", "", false),
		},
	}
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("setGroupVersionKind: %v", err)
	}

	// Should still find by kind
	if crd := k.LookupByKind("Cache").Entry(); crd == nil {
		t.Error("expected to find Cache by kind")
	}

	// Should not find by target (no serve target)
	if crd := k.LookupByTarget("cache").Entry(); crd != nil {
		t.Errorf("expected nil for target, got %+v", crd)
	}
}

func TestLookupByKind_CaseInsensitive(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": testCRDEntry("App", "", false),
		},
	}
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("setGroupVersionKind: %v", err)
	}

	// Exact match should work
	if crd := k.LookupByKind("App").Entry(); crd == nil {
		t.Error("expected to find App by exact match")
	}

	// Case-insensitive match should work
	if crd := k.LookupByKind("app").Entry(); crd == nil {
		t.Error("expected to find App by case-insensitive match")
	}
	if crd := k.LookupByKind("APP").Entry(); crd == nil {
		t.Error("expected to find App by case-insensitive match")
	}
}

func TestLookupByAPIVersionAndKind(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v1",
					Kind:    "App",
					Plural:  "apps",
				},
			},
			"database": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v1beta1",
					Kind:    "Database",
					Plural:  "databases",
				},
			},
			"config": {
				APITypes: orktypes.APITypes{
					Group:   "config.myorg.io",
					Version: "v1",
					Kind:    "Config",
					Plural:  "configs",
				},
			},
		},
	}
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("setGroupVersionKind: %v", err)
	}

	tests := []struct {
		name       string
		apiVersion string
		kind       string
		wantKind   string
		wantNil    bool
	}{
		{
			name:       "exact match - core group",
			apiVersion: "platform.myorg.io/v1",
			kind:       "App",
			wantKind:   "App",
			wantNil:    false,
		},
		{
			name:       "exact match - different version",
			apiVersion: "platform.myorg.io/v1beta1",
			kind:       "Database",
			wantKind:   "Database",
			wantNil:    false,
		},
		{
			name:       "exact match - different group",
			apiVersion: "config.myorg.io/v1",
			kind:       "Config",
			wantKind:   "Config",
			wantNil:    false,
		},
		{
			name:       "case insensitive - apiVersion",
			apiVersion: "Platform.myorg.io/V1",
			kind:       "App",
			wantKind:   "App",
			wantNil:    false,
		},
		{
			name:       "case insensitive - kind",
			apiVersion: "platform.myorg.io/v1",
			kind:       "app",
			wantKind:   "App",
			wantNil:    false,
		},
		{
			name:       "case insensitive - both",
			apiVersion: "Platform.myorg.io/V1",
			kind:       "app",
			wantKind:   "App",
			wantNil:    false,
		},
		{
			name:       "with spaces - apiVersion",
			apiVersion: "  platform.myorg.io/v1  ",
			kind:       "App",
			wantKind:   "App",
			wantNil:    false,
		},
		{
			name:       "with spaces - kind",
			apiVersion: "platform.myorg.io/v1",
			kind:       "  App  ",
			wantKind:   "App",
			wantNil:    false,
		},
		{
			name:       "not found - wrong group",
			apiVersion: "unknown.io/v1",
			kind:       "App",
			wantNil:    true,
		},
		{
			name:       "not found - wrong version",
			apiVersion: "platform.myorg.io/v2",
			kind:       "App",
			wantNil:    true,
		},
		{
			name:       "not found - wrong kind",
			apiVersion: "platform.myorg.io/v1",
			kind:       "Unknown",
			wantNil:    true,
		},
		{
			name:       "not found - wrong group and kind",
			apiVersion: "unknown.io/v1",
			kind:       "Unknown",
			wantNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := k.LookupByAPIVersionAndKind(tt.apiVersion, tt.kind).Entry()
			if tt.wantNil {
				if crd != nil {
					t.Errorf("expected nil, got %+v", crd)
				}
				return
			}
			if crd == nil {
				t.Fatal("expected CRD, got nil")
			}
			if crd.APITypes.Kind != tt.wantKind {
				t.Errorf("expected %q, got %q", tt.wantKind, crd.APITypes.Kind)
			}
			if wantGroup := strings.Split(tt.apiVersion, "/")[0]; !strings.EqualFold(crd.APITypes.Group, strings.TrimSpace(wantGroup)) {
				t.Errorf("expected group %q, got %q", wantGroup, crd.APITypes.Group)
			}
		})
	}
}

func TestLookupByAPIVersionAndKind_EmptyInput(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v1",
					Kind:    "App",
					Plural:  "apps",
				},
			},
		},
	}
	k.SetGroupVersionKind()

	tests := []struct {
		name       string
		apiVersion string
		kind       string
	}{
		{"empty apiVersion", "", "App"},
		{"empty kind", "platform.myorg.io/v1", ""},
		{"both empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := k.LookupByAPIVersionAndKind(tt.apiVersion, tt.kind).Entry()
			if crd != nil {
				t.Errorf("expected nil for empty input, got %+v", crd)
			}
		})
	}
}

func TestLookupByAPIVersionAndKind_IndexExists(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v1",
					Kind:    "App",
					Plural:  "apps",
				},
			},
		},
	}
	// set defaults
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("failed to set defaults: %v", err)
	}
	k.SetGroupVersionKind()

	// Check that the index was built correctly
	expectedKey := strings.ToLower("platform.myorg.io/v1" + "App")
	if _, ok := k.apiVersionIndex[expectedKey]; !ok {
		t.Errorf("index key %q not found in apiVersionIndex", expectedKey)
	}

	// Verify the lookup works with the exact key
	crd := k.LookupByAPIVersionAndKind("platform.myorg.io/v1", "App").Entry()
	if crd == nil {
		t.Fatal("expected CRD, got nil")
	}
	if crd.APITypes.Kind != "App" {
		t.Errorf("expected App, got %s", crd.APITypes.Kind)
	}
}

func TestLookupByAPIVersionAndKind_MultipleEntries(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v1",
					Kind:    "App",
					Plural:  "apps",
				},
			},
			"appv2": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v2",
					Kind:    "App",
					Plural:  "apps",
				},
			},
			"database": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v1",
					Kind:    "Database",
					Plural:  "databases",
				},
			},
		},
	}
	// set defaults
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("failed to set defaults: %v", err)
	}
	k.SetGroupVersionKind()

	// Should find the v1 App
	crd := k.LookupByAPIVersionAndKind("platform.myorg.io/v1", "App").Entry()
	if crd == nil {
		t.Fatal("expected App v1, got nil")
	}
	if crd.APITypes.Version != "v1" {
		t.Errorf("expected version v1, got %s", crd.APITypes.Version)
	}

	// Should find the v2 App
	crd = k.LookupByAPIVersionAndKind("platform.myorg.io/v2", "App").Entry()
	if crd == nil {
		t.Fatal("expected App v2, got nil")
	}
	if crd.APITypes.Version != "v2" {
		t.Errorf("expected version v2, got %s", crd.APITypes.Version)
	}

	// Should find the Database
	crd = k.LookupByAPIVersionAndKind("platform.myorg.io/v1", "Database").Entry()
	if crd == nil {
		t.Fatal("expected Database, got nil")
	}
	if crd.APITypes.Kind != "Database" {
		t.Errorf("expected Database, got %s", crd.APITypes.Kind)
	}
}

func TestServeEnabledCRDsForCluster(t *testing.T) {
	// mirrors the multi-cluster walkthrough scenario:
	//   website  → staging + prod (static)
	//   function → prod only (static)
	//   config   → template (conservative: all clusters)
	//   draft    → no clusters (local only → never appears on a remote cluster)
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"website": {
				APITypes: orktypes.APITypes{Kind: "Website", Plural: "websites"},
				Serve: &orktypes.ServeConfig{
					Enabled:  true,
					Clusters: []string{"staging", "prod"},
				},
			},
			"function": {
				APITypes: orktypes.APITypes{Kind: "Function", Plural: "functions"},
				Serve: &orktypes.ServeConfig{
					Enabled:  true,
					Clusters: []string{"prod"},
				},
			},
			"config": {
				APITypes: orktypes.APITypes{Kind: "Config", Plural: "configs"},
				Serve: &orktypes.ServeConfig{
					Enabled:  true,
					Clusters: []string{`{{ if eq .request.env "prod" }}prod{{ else }}staging{{ end }}`},
				},
			},
			"draft": {
				APITypes: orktypes.APITypes{Kind: "Draft", Plural: "drafts"},
				Serve: &orktypes.ServeConfig{
					Enabled:  true,
					Clusters: nil, // local only
				},
			},
		},
	}

	kindsFor := func(crds []*orktypes.CRDEntry) map[string]bool {
		out := make(map[string]bool, len(crds))
		for _, c := range crds {
			out[c.APITypes.Kind] = true
		}
		return out
	}

	t.Run("staging", func(t *testing.T) {
		got := kindsFor(k.ServeEnabledCRDsForCluster("staging"))
		if !got["Website"] {
			t.Error("expected Website on staging")
		}
		if got["Function"] {
			t.Error("Function should not appear on staging (prod only)")
		}
		if !got["Config"] {
			t.Error("expected Config on staging (template — conservative)")
		}
		if got["Draft"] {
			t.Error("Draft should not appear on staging (local only)")
		}
	})

	t.Run("prod", func(t *testing.T) {
		got := kindsFor(k.ServeEnabledCRDsForCluster("prod"))
		if !got["Website"] {
			t.Error("expected Website on prod")
		}
		if !got["Function"] {
			t.Error("expected Function on prod")
		}
		if !got["Config"] {
			t.Error("expected Config on prod (template — conservative)")
		}
		if got["Draft"] {
			t.Error("Draft should not appear on prod (local only)")
		}
	})

	t.Run("unknown cluster", func(t *testing.T) {
		got := k.ServeEnabledCRDsForCluster("eu-west")
		// only template-routed CRDs (Config) appear for unknown clusters
		kinds := kindsFor(got)
		if !kinds["Config"] {
			t.Error("expected Config on eu-west (template — conservative)")
		}
		if kinds["Website"] || kinds["Function"] || kinds["Draft"] {
			t.Errorf("only template CRDs should appear for unknown cluster, got %v", kinds)
		}
	})
}

func TestServeEnabledCRDsForCluster_TargetOverride(t *testing.T) {
	// serve.clusters: [staging, prod]; target.prod-only.clusters: [prod]
	// Both staging and prod appear in serve.clusters, so both clusters see the CRD.
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"website": {
				APITypes: orktypes.APITypes{Kind: "Website", Plural: "websites"},
				Serve: &orktypes.ServeConfig{
					Enabled:  true,
					Clusters: []string{"staging", "prod"},
					Target: orktypes.ServeTargetValue{
						Entries: map[string]*orktypes.ServeTargetConfig{
							"prod-only": {Clusters: []string{"prod"}},
						},
					},
				},
			},
		},
	}

	for _, cluster := range []string{"staging", "prod"} {
		crds := k.ServeEnabledCRDsForCluster(cluster)
		if len(crds) != 1 || crds[0].APITypes.Kind != "Website" {
			t.Errorf("expected Website on %s, got %v", cluster, crds)
		}
	}
}

func TestServeEnabledCRDsForCluster_TargetTemplateRouted(t *testing.T) {
	// serve.clusters empty; target has a template cluster → CRD appears on all.
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				APITypes: orktypes.APITypes{Kind: "App", Plural: "apps"},
				Serve: &orktypes.ServeConfig{
					Enabled: true,
					Target: orktypes.ServeTargetValue{
						Entries: map[string]*orktypes.ServeTargetConfig{
							"primary": {Clusters: []string{`{{ .request.env }}`}},
						},
					},
				},
			},
		},
	}

	for _, cluster := range []string{"prod", "staging", "eu-west"} {
		crds := k.ServeEnabledCRDsForCluster(cluster)
		if len(crds) != 1 {
			t.Errorf("expected App on %s (template target), got %d CRDs", cluster, len(crds))
		}
	}
}

func TestServeEnabledCRDsForCluster_NilKatalog(t *testing.T) {
	var k *Katalog
	if crds := k.ServeEnabledCRDsForCluster("prod"); crds != nil {
		t.Errorf("expected nil from nil Katalog, got %v", crds)
	}
}

func TestLookupByAPIVersionAndKind_InvalidAPIVersion(t *testing.T) {
	k := &Katalog{
		enabledCRDs: map[string]orktypes.CRDEntry{
			"app": {
				APITypes: orktypes.APITypes{
					Group:   "platform.myorg.io",
					Version: "v1",
					Kind:    "App",
					Plural:  "apps",
				},
			},
		},
	}
	// set defaults
	if err := k.SetGroupVersionKind(); err != nil {
		t.Fatalf("failed to set defaults: %v", err)
	}
	k.SetGroupVersionKind()

	// Test with malformed apiVersion
	tests := []struct {
		name       string
		apiVersion string
		kind       string
	}{
		{"no version", "platform.myorg.io", "App"},
		{"multiple slashes", "platform/myorg/io/v1", "App"},
		{"empty group", "/v1", "App"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := k.LookupByAPIVersionAndKind(tt.apiVersion, tt.kind).Entry()
			if crd != nil {
				t.Errorf("expected nil for malformed apiVersion %q, got %+v", tt.apiVersion, crd)
			}
		})
	}
}
