package shared

import (
	"testing"

	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

type fakeKubeclient struct {
	kubeclient.Interface
	forceConflict *bool
}

func (f *fakeKubeclient) ForceConflict() *bool {
	return f.forceConflict
}
func TestResolveOwnerReferences(t *testing.T) {
	owner := &unstructured.Unstructured{}
	owner.SetAPIVersion("demo.orkestra.io/v1alpha1")
	owner.SetKind("Application")
	owner.SetName("my-app")
	owner.SetUID("test-uid")

	refs := ResolveOwnerReferences(owner)

	require.Len(t, refs, 1)
	assert.Equal(t, "demo.orkestra.io/v1alpha1", refs[0].APIVersion)
	assert.Equal(t, "Application", refs[0].Kind)
	assert.Equal(t, "my-app", refs[0].Name)
	assert.Equal(t, types.UID("test-uid"), refs[0].UID)
	assert.True(t, *refs[0].Controller)
	assert.True(t, *refs[0].BlockOwnerDeletion)
}

func TestResolveForceConflict(t *testing.T) {
	trueValue := true
	falseValue := false

	tests := []struct {
		name     string
		kube     kubeclient.Interface
		resource *bool
		want     bool
	}{
		{
			name:     "resource overrides crd",
			kube:     &fakeKubeclient{forceConflict: &trueValue},
			resource: &trueValue,
			want:     true,
		},
		{
			name: "crd used when resource unset",
			kube: &fakeKubeclient{forceConflict: &falseValue},
			want: false,
		},
		{
			name: "defaults to true",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveForceConflict(tt.kube, tt.resource)
			require.NotNil(t, got)
			assert.Equal(t, tt.want, *got)
		})
	}
}
