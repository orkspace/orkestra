package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"gopkg.in/yaml.v3"
)

func TestToGatewayClusterConfig_WithCA(t *testing.T) {
	r := &Result{
		Entry:           ClusterEntry{Name: "prod"},
		Endpoint:        "https://prod.internal:6443",
		SecretName:      "orkestra-prod",
		SecretNamespace: "default",
		HasCA:           true,
	}

	cfg := r.ToGatewayClusterConfig()

	if cfg.Endpoint != "https://prod.internal:6443" {
		t.Errorf("endpoint: got %q", cfg.Endpoint)
	}
	if cfg.TokenRef == nil {
		t.Fatal("expected TokenRef")
	}
	if cfg.TokenRef.Name != "orkestra-prod" || cfg.TokenRef.Namespace != "default" || cfg.TokenRef.Key != "token" {
		t.Errorf("unexpected TokenRef: %+v", cfg.TokenRef)
	}
	if cfg.CARef == nil {
		t.Fatal("expected CARef when HasCA=true")
	}
	if cfg.CARef.Name != "orkestra-prod" || cfg.CARef.Key != "ca.crt" {
		t.Errorf("unexpected CARef: %+v", cfg.CARef)
	}
	if cfg.SecretRef != nil {
		t.Error("SecretRef should be nil (token credential form)")
	}
}

func TestToGatewayClusterConfig_WithoutCA(t *testing.T) {
	r := &Result{
		Entry:           ClusterEntry{Name: "staging"},
		Endpoint:        "https://staging.internal:6443",
		SecretName:      "orkestra-staging",
		SecretNamespace: "orkestra",
		HasCA:           false,
	}

	cfg := r.ToGatewayClusterConfig()

	if cfg.TokenRef == nil {
		t.Fatal("expected TokenRef")
	}
	if cfg.CARef != nil {
		t.Error("CARef should be nil when HasCA=false")
	}
}

func TestWriteClusterCredentials_RoundTrip(t *testing.T) {
	results := []*Result{
		{
			Entry:           ClusterEntry{Name: "staging"},
			Endpoint:        "https://staging.internal:6443",
			SecretName:      "orkestra-staging",
			SecretNamespace: "default",
			HasCA:           true,
		},
		{
			Entry:           ClusterEntry{Name: "prod"},
			Endpoint:        "https://prod.internal:6443",
			SecretName:      "orkestra-prod",
			SecretNamespace: "default",
			HasCA:           false,
		},
	}

	path := filepath.Join(t.TempDir(), "clusters-creds.yaml")
	if err := WriteClusterCredentials(path, results); err != nil {
		t.Fatalf("WriteClusterCredentials: %v", err)
	}

	data, err := readLocal(path)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	var out struct {
		Clusters map[string]orktypes.GatewayClusterConfig `yaml:"clusters"`
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshalling output: %v", err)
	}

	if len(out.Clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(out.Clusters))
	}

	staging, ok := out.Clusters["staging"]
	if !ok {
		t.Fatal("staging missing from output")
	}
	if staging.Endpoint != "https://staging.internal:6443" {
		t.Errorf("staging endpoint: got %q", staging.Endpoint)
	}
	if staging.CARef == nil {
		t.Error("staging: expected CARef")
	}

	prod, ok := out.Clusters["prod"]
	if !ok {
		t.Fatal("prod missing from output")
	}
	if prod.CARef != nil {
		t.Error("prod: CARef should be absent (HasCA=false)")
	}
}

func TestWriteClusterCredentials_FilePermissions(t *testing.T) {
	r := &Result{
		Entry:           ClusterEntry{Name: "prod"},
		Endpoint:        "https://prod.internal:6443",
		SecretName:      "orkestra-prod",
		SecretNamespace: "default",
	}

	path := filepath.Join(t.TempDir(), "clusters-creds.yaml")
	if err := WriteClusterCredentials(path, []*Result{r}); err != nil {
		t.Fatalf("WriteClusterCredentials: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected 0600 permissions, got %04o", perm)
	}
}

func TestWriteClusterCredentials_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	path := filepath.Join(dir, "clusters-creds.yaml")

	r := &Result{
		Entry:      ClusterEntry{Name: "prod"},
		Endpoint:   "https://prod.internal:6443",
		SecretName: "orkestra-prod", SecretNamespace: "default",
	}
	if err := WriteClusterCredentials(path, []*Result{r}); err != nil {
		t.Fatalf("WriteClusterCredentials: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}
