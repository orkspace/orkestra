package validate

import (
	"testing"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── ValidateCustomResource ────────────────────────────────────────────────────

func TestValidateCustomResource_Valid(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Application",
		Metadata: orktypes.CustomResourceMetadata{
			Name:      "{{ .metadata.name }}",
			Namespace: "argocd",
		},
	}
	assert.NoError(t, ValidateCustomResource(nil, cr, "test.onCreate.custom[0]"))
}

func TestValidateCustomResource_Nil(t *testing.T) {
	err := ValidateCustomResource(nil, nil, "test.onCreate.custom[0]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestValidateCustomResource_MissingAPIVersion(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		Kind: "Application",
		Metadata: orktypes.CustomResourceMetadata{
			Name:      "my-app",
			Namespace: "argocd",
		},
	}
	err := ValidateCustomResource(nil, cr, "test.onCreate.custom[0]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiVersion")
}

func TestValidateCustomResource_InvalidAPIVersionFormat(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "v1", // core group — no slash — rejected as invalid format for custom:
		Kind:       "ConfigMap",
		Metadata: orktypes.CustomResourceMetadata{
			Name:      "my-cm",
			Namespace: "default",
		},
	}
	err := ValidateCustomResource(nil, cr, "test.onCreate.custom[0]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group/version")
}

func TestValidateCustomResource_MissingKind(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "argoproj.io/v1alpha1",
		Metadata: orktypes.CustomResourceMetadata{
			Name:      "my-app",
			Namespace: "argocd",
		},
	}
	err := ValidateCustomResource(nil, cr, "test.onCreate.custom[0]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind")
}

func TestValidateCustomResource_MissingName(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Application",
		Metadata: orktypes.CustomResourceMetadata{
			Namespace: "argocd",
		},
	}
	err := ValidateCustomResource(nil, cr, "test.onCreate.custom[0]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metadata.name")
}

func TestValidateCustomResource_MissingNamespace(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Application",
		Metadata: orktypes.CustomResourceMetadata{
			Name: "my-app",
			// Namespace omitted — should fail for namespaced (default)
		},
	}
	err := ValidateCustomResource(nil, cr, "test.onCreate.custom[0]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metadata.namespace")
}

func TestValidateCustomResource_ClusterScopedNoNamespace(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "ClusterWorkflowTemplate",
		Metadata: orktypes.CustomResourceMetadata{
			Name:       "my-template",
			Namespaced: boolPtr(false),
		},
	}
	assert.NoError(t, ValidateCustomResource(nil, cr, "test.onCreate.custom[0]"))
}

func TestValidateCustomResource_ClusterScopedWithNamespace(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "ClusterWorkflowTemplate",
		Metadata: orktypes.CustomResourceMetadata{
			Name:       "my-template",
			Namespace:  "should-not-be-here",
			Namespaced: boolPtr(false),
		},
	}
	err := ValidateCustomResource(nil, cr, "test.onCreate.custom[0]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespaced=false")
}

// ── Native type guard ─────────────────────────────────────────────────────────
// These are the cases that triggered the scheme double-registration panic in
// simulate. Using custom: for a built-in Orkestra resource must be caught at
// validate time with a clear redirect to the correct HookTemplates key.

func TestValidateCustomResource_NativeType_NetworkPolicy(t *testing.T) {
	// This is exactly what the fixture motif used to declare before the fix.
	// networking.k8s.io/v1 NetworkPolicy is a native Orkestra resource.
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "networking.k8s.io/v1",
		Kind:       "NetworkPolicy",
		Metadata: orktypes.CustomResourceMetadata{
			Name:      "{{ .metadata.name }}",
			Namespace: "{{ .spec.targetNamespace }}",
		},
	}
	err := ValidateCustomResource(nil, cr, "clusterPolicy.onCreate.custom[0]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "networkPolicies")
	assert.Contains(t, err.Error(), "native Orkestra resource")
}

func TestValidateCustomResource_NativeType_Deployment(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Metadata: orktypes.CustomResourceMetadata{
			Name:      "my-deploy",
			Namespace: "default",
		},
	}
	err := ValidateCustomResource(nil, cr, "test.onCreate.custom[0]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deployments")
	assert.Contains(t, err.Error(), "native Orkestra resource")
}

func TestValidateCustomResource_NativeType_ConfigMap(t *testing.T) {
	// apps/v1 ConfigMap doesn't exist — but configmaps/v1 uses bare "v1" which
	// already fails the apiVersion format check (no slash). This test confirms
	// the format gate fires before the native guard.
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata: orktypes.CustomResourceMetadata{
			Name:      "my-cm",
			Namespace: "default",
		},
	}
	err := ValidateCustomResource(nil, cr, "test.onCreate.custom[0]")
	require.Error(t, err)
	// Must fail on format (no slash), not reach the native guard.
	assert.Contains(t, err.Error(), "group/version")
}

func TestValidateCustomResource_NativeType_HPA(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "autoscaling/v2",
		Kind:       "HorizontalPodAutoscaler",
		Metadata: orktypes.CustomResourceMetadata{
			Name:      "my-hpa",
			Namespace: "default",
		},
	}
	err := ValidateCustomResource(nil, cr, "test.onCreate.custom[0]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hpa")
	assert.Contains(t, err.Error(), "native Orkestra resource")
}

func TestValidateCustomResource_NonNative_Crossplane(t *testing.T) {
	// crossplane.io resources are not native — custom: is the correct path.
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "database.crossplane.io/v1alpha1",
		Kind:       "PostgreSQLInstance",
		Metadata: orktypes.CustomResourceMetadata{
			Name:      "my-db",
			Namespace: "default",
		},
	}
	assert.NoError(t, ValidateCustomResource(nil, cr, "test.onCreate.custom[0]"))
}

func TestValidateCustomResource_NonNative_ArgoCD(t *testing.T) {
	cr := &orktypes.CustomResourceTemplateSource{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Application",
		Metadata: orktypes.CustomResourceMetadata{
			Name:      "{{ .metadata.name }}",
			Namespace: "argocd",
		},
	}
	assert.NoError(t, ValidateCustomResource(nil, cr, "test.onCreate.custom[0]"))
}
