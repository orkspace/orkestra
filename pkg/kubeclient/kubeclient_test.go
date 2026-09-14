package kubeclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithForceConflict(t *testing.T) {
	// Case 1: Info is nil
	k := &Kubeclient{
		Info: nil,
	}
	result := k.WithForceConflict(&[]bool{true}[0])
	assert.NotNil(t, result.(*Kubeclient).Info)
	assert.True(t, *result.(*Kubeclient).Info.ForceConflict)

	// Case 2: Info exists
	k2 := &Kubeclient{
		Info: &CRDInfo{
			ForceConflict: &[]bool{false}[0],
		},
	}
	result2 := k2.WithForceConflict(&[]bool{true}[0])
	assert.NotNil(t, result2.(*Kubeclient).Info)
	assert.True(t, *result2.(*Kubeclient).Info.ForceConflict)
	// Original should be unchanged
	assert.False(t, *k2.Info.ForceConflict)
}
