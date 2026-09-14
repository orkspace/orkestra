package validate

import (
	"reflect"
	"strings"
	"testing"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/konfig"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var kfg = konfig.NewDefaultKonfig()

func katalogWithMetadata(m orktypes.KatalogMeta) *executor {
	return newExec(katalog.NewKatalogWithMetadataForTest(m, map[string]orktypes.CRDEntry{
		"app": {IsStatusless: true},
	}))
}

func katalogWithAPITypes(a orktypes.APITypes) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {APITypes: a},
	})

}

func katalogWithOperatorBox(box orktypes.OperatorBoxConfig) *executor {
	return newKatalogExec(map[string]orktypes.CRDEntry{
		"app": {OperatorBox: box},
	})

}

// setDefaults

func TestSetDefaults_MetadataNoData(t *testing.T) {
	k := katalogWithMetadata(orktypes.KatalogMeta{})
	err := k.k.SetDefaults(kfg)
	assert.NoError(t, err)

	entry := k.k.EnabledCRDs()["app"]
	assert.Equal(t, entry.KatalogName, "orkestra-runtime-app")
	assert.Equal(t, entry.KatalogNamespace, "default")
}

func TestSetDefaults_MetadataInvalidName(t *testing.T) {
	k := katalogWithMetadata(orktypes.KatalogMeta{
		Name:        "test:m,",
		Namespace:   "test-tea",
		ClusterName: "test-cluster",
	})
	err := k.k.SetDefaults(kfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid metadata.Name")
}

func TestSetDefaults_MetadataInvalidNameSpace(t *testing.T) {
	k := katalogWithMetadata(orktypes.KatalogMeta{
		Name:        "test",
		Namespace:   "test-tea/m",
		ClusterName: "test-cluster",
	})
	err := k.k.SetDefaults(kfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid metadata.Namespace")
}

func TestSetDefaults_MetadataWithData(t *testing.T) {
	k := katalogWithMetadata(orktypes.KatalogMeta{
		Namespace:   "test-team2",
		ClusterName: "test-cluster",
	})
	err := k.k.SetDefaults(kfg)
	assert.NoError(t, err)

	entry := k.k.EnabledCRDs()["app"]
	assert.Equal(t, entry.KatalogName, "test-cluster-app")
	assert.Equal(t, entry.KatalogNamespace, "test-team2")
}

func TestSetDefaults_MetadataKatalogNameDefaultToClusterName(t *testing.T) {
	k := katalogWithMetadata(orktypes.KatalogMeta{
		Name:        "test",
		Namespace:   "test-team",
		ClusterName: "test-cluster",
	})
	err := k.k.SetDefaults(kfg)
	assert.NoError(t, err)

	entry := k.k.EnabledCRDs()["app"]
	assert.Equal(t, entry.KatalogName, "test")
	assert.Equal(t, entry.KatalogNamespace, "test-team")
}

func TestSetDefaults_APITypes(t *testing.T) {
	k := katalogWithAPITypes(orktypes.APITypes{
		Kind:    "Website",
		Group:   "test.orkestra.katalog",
		Version: "v1",
	})

	err := k.k.SetDefaults(kfg)
	assert.NoError(t, err)

	// Assert APITypes default
	entry := k.k.EnabledCRDs()["app"]
	apiPath := entry.APITypes.APIPath
	plural := entry.APITypes.Plural

	assert.Contains(t, apiPath, "/apis")
	assert.Contains(t, plural, "websites")

}

func TestSetDefaults_OperatorBoxFinalizersDefault(t *testing.T) {
	k := katalogWithOperatorBox(orktypes.OperatorBoxConfig{})
	k.k.Spec.Finalizers = []string{"spec-finalizer"}

	err := k.k.SetDefaults(kfg)
	assert.NoError(t, err)

	boxFinalizers := k.k.EnabledCRDs()["app"].OperatorBox.Finalizers
	if !reflect.DeepEqual(boxFinalizers, k.k.Spec.Finalizers) {
		t.Fatalf("unexpected error. want true, got false")
	}
}

func TestSetDefaults_SpecFinalizerAddToOperatorBoxFinalizer(t *testing.T) {
	k := katalogWithOperatorBox(orktypes.OperatorBoxConfig{})
	k.k.Spec.Finalizers = []string{"spec-finalizer"}

	err := k.k.SetDefaults(kfg)
	assert.NoError(t, err)

	boxFinalizers := k.k.EnabledCRDs()["app"].OperatorBox.Finalizers
	boxFinalizers = append(boxFinalizers, "box-finalizer")

	if reflect.DeepEqual(boxFinalizers, k.k.Spec.Finalizers) {
		t.Fatalf("unexpected error. want false, got true")
	}

	if len(boxFinalizers) != 2 {
		t.Fatalf("unexpected error. want true, got false: %d", len(boxFinalizers))
	}
}

func TestSetDefaults_TargetOperatorBoxFinalizersDefault(t *testing.T) {
	serve := &orktypes.ServeConfig{
		Enabled: true,
		Target: orktypes.ServeTargetValue{
			Entries: map[string]*orktypes.ServeTargetConfig{
				"testfixture": {
					OperatorBox: &orktypes.OperatorBoxConfig{
						Finalizers: []string{"target-finalizer"},
					},
				},
			},
		},
	}

	k := katalogWithServe(serve)
	k.k.Spec.Finalizers = []string{"spec-finalizer"}
	entry := k.k.EnabledCRDs()["myresource"]

	err := k.k.SetDefaults(kfg)
	assert.NoError(t, err)

	boxFinalizers := entry.OperatorBox.Finalizers
	if reflect.DeepEqual(boxFinalizers, k.k.Spec.Finalizers) {
		t.Fatalf("unexpected error. want true, got false")
	}

	targetFinalizers := entry.Serve.Target.Entries["testfixture"].OperatorBox.Finalizers
	if len(targetFinalizers) != 1 {
		t.Fatalf("unexpected error. want true, got false")
	}
	// if len(boxFinalizers) != 2 {
	// 	t.Fatalf("unexpected error. want true, got %d", len(boxFinalizers))
	// }
}

func TestSetDefaults_ReconcilerDefaultsQueueWarning(t *testing.T) {
	k := katalogWithOperatorBox(orktypes.OperatorBoxConfig{})
	err := k.k.SetDefaults(kfg)
	assert.NoError(t, err)

	entry := k.k.EnabledCRDs()["app"]
	rec := entry.ReconcilerConfig()
	assert.Equal(t, rec.Workers, 3)
	assert.Equal(t, rec.Resync.String(), "15s")
	assert.Equal(t, rec.Queue.MaxDepth, 0) // left as-is
	assert.Equal(t, rec.Queue.FailureThreshold, 5)

	// Assert warning is added to crd for unlimited queue
	if !entry.Warnings.HasWarnings() {
		t.Fatal("expected crd to have warning")
	}

	warn := entry.Warnings.String()
	if !strings.Contains(warn, "has uses unlimited queue") {
		t.Fatalf("incorrect warning. got: %q", warn)
	}
}
