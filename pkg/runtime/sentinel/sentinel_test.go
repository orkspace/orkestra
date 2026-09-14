package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type refAndManagedFields struct {
	name       string
	apiversion string
	kind       string
	controller *bool
	manager    string
	operation  string
	fieldType  string
}

func obj(gen int64, labels, annotations map[string]string) metav1.Object {
	o := &metav1.ObjectMeta{
		Generation:  gen,
		Labels:      labels,
		Annotations: annotations,
	}
	return o
}

func collectOwnerRefs(ownRefs []refAndManagedFields) (refs []metav1.OwnerReference) {
	for _, ref := range ownRefs {
		newRef := metav1.OwnerReference{
			Name:       ref.name,
			Kind:       ref.kind,
			APIVersion: ref.apiversion,
			Controller: boolPtr(*ref.controller),
		}
		refs = append(refs, newRef)
	}
	return refs
}

func collectManagedFields(mgdFields []refAndManagedFields) (fields []metav1.ManagedFieldsEntry) {
	for _, field := range mgdFields {
		newEntry := metav1.ManagedFieldsEntry{
			Manager:    field.manager,
			Operation:  metav1.ManagedFieldsOperationType(field.operation),
			APIVersion: field.apiversion,
			FieldsType: field.fieldType,
		}
		fields = append(fields, newEntry)
	}
	return fields
}

func TestCompute_Empty(t *testing.T) {
	result := Compute(nil, obj(1, nil, nil), obj(2, nil, nil))
	assert.Nil(t, result)
}

func TestCompute_GenerationChanged(t *testing.T) {
	result := Compute([]string{GenerationChanged.String()},
		obj(1, nil, nil), obj(2, nil, nil))
	assert.Equal(t, "true", result[GenerationChanged.String()])
}

func TestCompute_GenerationUnchanged(t *testing.T) {
	result := Compute([]string{GenerationChanged.String()},
		obj(5, nil, nil), obj(5, nil, nil))
	assert.Equal(t, "false", result[GenerationChanged.String()])
}

func TestCompute_LabelsChanged(t *testing.T) {
	result := Compute([]string{LabelsChanged.String()},
		obj(1, map[string]string{"a": "1"}, nil),
		obj(1, map[string]string{"a": "2"}, nil))
	assert.Equal(t, "true", result[LabelsChanged.String()])
}

func TestCompute_LabelsUnchanged(t *testing.T) {
	result := Compute([]string{LabelsChanged.String()},
		obj(1, map[string]string{"a": "1"}, nil),
		obj(1, map[string]string{"a": "1"}, nil))
	assert.Equal(t, "false", result[LabelsChanged.String()])
}

func TestCompute_AnnotationsChanged(t *testing.T) {
	result := Compute([]string{AnnotationsChanged.String()},
		obj(1, nil, map[string]string{"x": "old"}),
		obj(1, nil, map[string]string{"x": "new"}))
	assert.Equal(t, "true", result[AnnotationsChanged.String()])
}

func TestCompute_OnlyDeclaredAreComputed(t *testing.T) {
	result := Compute([]string{GenerationChanged.String()},
		obj(1, map[string]string{"a": "1"}, nil),
		obj(2, map[string]string{"b": "2"}, nil))
	assert.Equal(t, "true", result[GenerationChanged.String()])
	_, hasLabels := result[LabelsChanged.String()]
	assert.False(t, hasLabels)
}

func TestCompute_DeletionStarted(t *testing.T) {
	now := metav1.Now()
	old := &metav1.ObjectMeta{}
	new := &metav1.ObjectMeta{DeletionTimestamp: &now}
	result := Compute([]string{DeletionStarted.String()}, old, new)
	assert.Equal(t, "true", result[DeletionStarted.String()])
}

func TestCompute_DeletionStarted_AlreadyDeleting(t *testing.T) {
	now := metav1.Now()
	old := &metav1.ObjectMeta{DeletionTimestamp: &now}
	new := &metav1.ObjectMeta{DeletionTimestamp: &now}
	result := Compute([]string{DeletionStarted.String()}, old, new)
	assert.Equal(t, "false", result[DeletionStarted.String()])
}

func TestCompute_FinalizersChanged(t *testing.T) {
	old := &metav1.ObjectMeta{Finalizers: []string{"orkestra.io/protect"}}
	new := &metav1.ObjectMeta{}
	result := Compute([]string{FinalizersChanged.String()}, old, new)
	assert.Equal(t, "true", result[FinalizersChanged.String()])
}

func TestCompute_FinalizersUnchanged(t *testing.T) {
	old := &metav1.ObjectMeta{Finalizers: []string{"orkestra.io/protect"}}
	new := &metav1.ObjectMeta{Finalizers: []string{"orkestra.io/protect"}}
	result := Compute([]string{FinalizersChanged.String()}, old, new)
	assert.Equal(t, "false", result[FinalizersChanged.String()])
}

func TestCompute_NameChanged(t *testing.T) {
	old := &metav1.ObjectMeta{Name: "ork"}
	new := &metav1.ObjectMeta{Name: "orkestra"}
	result := Compute([]string{NameChanged.String()}, old, new)
	assert.Equal(t, "true", result[NameChanged.String()])
}

func TestCompute_NameUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{Name: "ork"}
	new := &metav1.ObjectMeta{Name: "ork"}
	result := Compute([]string{NameChanged.String()}, old, new)
	assert.Equal(t, "false", result[NameChanged.String()])
}

func TestCompute_NamespaceChanged(t *testing.T) {
	old := &metav1.ObjectMeta{Namespace: "ork-system"}
	new := &metav1.ObjectMeta{Namespace: "orkestra-system"}
	result := Compute([]string{NamespaceChanged.String()}, old, new)
	assert.Equal(t, "true", result[NamespaceChanged.String()])
}

func TestCompute_NamespaceUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{Namespace: "ork-system"}
	new := &metav1.ObjectMeta{Namespace: "ork-system"}
	result := Compute([]string{NamespaceChanged.String()}, old, new)
	assert.Equal(t, "false", result[NamespaceChanged.String()])
}

func TestCompute_GenerateNameChanged(t *testing.T) {
	old := &metav1.ObjectMeta{GenerateName: "ork-generate"}
	new := &metav1.ObjectMeta{GenerateName: "orkestra-generate"}
	result := Compute([]string{GenerateNameChanged.String()}, old, new)
	assert.Equal(t, "true", result[GenerateNameChanged.String()])
}

func TestCompute_GenerateNameUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{GenerateName: "ork-generate"}
	new := &metav1.ObjectMeta{GenerateName: "ork-generate"}
	result := Compute([]string{GenerateNameChanged.String()}, old, new)
	assert.Equal(t, "false", result[GenerateNameChanged.String()])
}

func TestCompute_UIDChanged(t *testing.T) {
	old := &metav1.ObjectMeta{UID: "ORK-XX-12-Y-TX"}
	new := &metav1.ObjectMeta{UID: "ORK-XX-12-Y-TY"}
	result := Compute([]string{UIDChanged.String()}, old, new)
	assert.Equal(t, "true", result[UIDChanged.String()])
}

func TestCompute_UIDUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{UID: "ORK-XX-12-Y-TX"}
	new := &metav1.ObjectMeta{UID: "ORK-XX-12-Y-TX"}
	result := Compute([]string{UIDChanged.String()}, old, new)
	assert.Equal(t, "false", result[UIDChanged.String()])
}

func TestCompute_ResourceVersionChanged(t *testing.T) {
	old := &metav1.ObjectMeta{ResourceVersion: "ork-v1.0"}
	new := &metav1.ObjectMeta{ResourceVersion: "orkestra-v1.1"}
	result := Compute([]string{ResourceVersionChanged.String()}, old, new)
	assert.Equal(t, "true", result[ResourceVersionChanged.String()])
}

func TestCompute_ResourceVersionUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{ResourceVersion: "ork-v1.0"}
	new := &metav1.ObjectMeta{ResourceVersion: "ork-v1.0"}
	result := Compute([]string{ResourceVersionChanged.String()}, old, new)
	assert.Equal(t, "false", result[ResourceVersionChanged.String()])
}

func TestCompute_CreationTimestampChanged(t *testing.T) {
	now := metav1.Now()
	newTime := metav1.NewTime(now.Add(5))

	old := &metav1.ObjectMeta{CreationTimestamp: now}
	new := &metav1.ObjectMeta{CreationTimestamp: newTime}
	result := Compute([]string{CreationTimestampChanged.String()}, old, new)
	assert.Equal(t, "true", result[CreationTimestampChanged.String()])
}

func TestCompute_CreationTimestamUnpChanged(t *testing.T) {
	now := metav1.Now()

	old := &metav1.ObjectMeta{CreationTimestamp: now}
	new := &metav1.ObjectMeta{CreationTimestamp: now}
	result := Compute([]string{CreationTimestampChanged.String()}, old, new)
	assert.Equal(t, "false", result[CreationTimestampChanged.String()])
}

func TestCompute_DeletionGracePeriodSecondsChanged(t *testing.T) {
	old := &metav1.ObjectMeta{DeletionGracePeriodSeconds: int64Ptr(5)}
	new := &metav1.ObjectMeta{DeletionGracePeriodSeconds: int64Ptr(6)}
	result := Compute([]string{DeletionGracePeriodSecondsChanged.String()}, old, new)
	assert.Equal(t, "true", result[DeletionGracePeriodSecondsChanged.String()])
}

func TestCompute_DeletionGracePeriodSecondsUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{DeletionGracePeriodSeconds: int64Ptr(5)}
	new := &metav1.ObjectMeta{DeletionGracePeriodSeconds: int64Ptr(5)}
	result := Compute([]string{DeletionGracePeriodSecondsChanged.String()}, old, new)
	assert.Equal(t, "false", result[DeletionGracePeriodSecondsChanged.String()])
}

func TestCompute_OwnerReferenceChangedNewControllerPtr(t *testing.T) {
	ref := func(ctrl bool) refAndManagedFields {
		return refAndManagedFields{
			name:       "orkestra",
			kind:       "Katalog",
			apiversion: "test.orkestra.io",
			controller: boolPtr(ctrl),
		}
	}

	oldRef := []refAndManagedFields{ref(false)}
	newRef := []refAndManagedFields{ref(true)}

	old := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(oldRef)}
	new := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(newRef)}
	result := Compute([]string{OwnerReferenceChanged.String()}, old, new)
	assert.Equal(t, "true", result[OwnerReferenceChanged.String()])
}

func TestCompute_OwnerReferenceChangedAddNewRef(t *testing.T) {
	oldRef := []refAndManagedFields{
		{
			name:       "orkestra",
			kind:       "Katalog",
			apiversion: "test.orkestra.io",
			controller: boolPtr(false),
		},
	}
	newRef := append(oldRef, refAndManagedFields{
		name:       "orkestra-gateway",
		kind:       "Komposer",
		apiversion: "test.orkestra.io",
		controller: boolPtr(true),
	})

	old := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(oldRef)}
	new := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(newRef)}
	result := Compute([]string{OwnerReferenceChanged.String()}, old, new)
	assert.Equal(t, "true", result[OwnerReferenceChanged.String()])
}

func TestCompute_OwnerReferenceUnChanged(t *testing.T) {
	ref := func(ctrl bool) refAndManagedFields {
		return refAndManagedFields{
			name:       "orkestra",
			kind:       "Katalog",
			apiversion: "test.orkestra.io",
			controller: boolPtr(ctrl),
		}
	}

	oldRef := []refAndManagedFields{ref(false)}

	old := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(oldRef)}
	new := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(oldRef)}
	result := Compute([]string{OwnerReferenceChanged.String()}, old, new)
	assert.Equal(t, "false", result[OwnerReferenceChanged.String()])
}

func TestCompute_ManagedFieldsChanged(t *testing.T) {
	entry := func(manager, operation string) refAndManagedFields {
		return refAndManagedFields{
			apiversion: "test.orkestra.io",
			fieldType:  "test",
			manager:    manager,
			operation:  operation,
		}
	}

	oldEntry := []refAndManagedFields{entry("ork", "apply")}
	newEntry := []refAndManagedFields{entry("orkestra", "apply")}

	old := &metav1.ObjectMeta{ManagedFields: collectManagedFields(oldEntry)}
	new := &metav1.ObjectMeta{ManagedFields: collectManagedFields(newEntry)}
	result := Compute([]string{ManagedFieldsChanged.String()}, old, new)
	assert.Equal(t, "true", result[ManagedFieldsChanged.String()])
}

func TestCompute_OwnerReferenceChangedAddNewField(t *testing.T) {
	oldEntry := []refAndManagedFields{
		{
			apiversion: "test.orkestra.io",
			fieldType:  "test",
			manager:    "orkestra-manager",
			operation:  "apply",
		},
	}
	newEntry := append(oldEntry, refAndManagedFields{
		apiversion: "test.orkestra.io",
		fieldType:  "test",
		manager:    "orkestra-manager",
		operation:  "update",
	})

	old := &metav1.ObjectMeta{ManagedFields: collectManagedFields(oldEntry)}
	new := &metav1.ObjectMeta{ManagedFields: collectManagedFields(newEntry)}
	result := Compute([]string{ManagedFieldsChanged.String()}, old, new)
	assert.Equal(t, "true", result[ManagedFieldsChanged.String()])
}

func TestCompute_ManagedFieldsUnChanged(t *testing.T) {
	entry := func(manager, operation string) refAndManagedFields {
		return refAndManagedFields{
			apiversion: "test.orkestra.io",
			fieldType:  "test",
			manager:    manager,
			operation:  operation,
		}
	}

	oldEntry := []refAndManagedFields{entry("ork", "exists")}

	old := &metav1.ObjectMeta{ManagedFields: collectManagedFields(oldEntry)}
	new := &metav1.ObjectMeta{ManagedFields: collectManagedFields(oldEntry)}
	result := Compute([]string{OwnerReferenceChanged.String()}, old, new)
	assert.Equal(t, "false", result[OwnerReferenceChanged.String()])
}

func TestCompute_UnknownSentinelReturnsEmpty(t *testing.T) {
	result := Compute([]string{"specChanged"},
		obj(1, nil, nil), obj(2, nil, nil))
	assert.Equal(t, "", result["specChanged"])
}

func TestIsValid_ValidSentinel(t *testing.T) {
	known := ManagedFieldsChanged.String()
	assert.Equal(t, true, IsValid(known))
}

func TestIsValid_InalidSentinel(t *testing.T) {
	assert.Equal(t, false, IsValid("unknown"))
}

func TestIsAllValid_MultipleValidSentinels(t *testing.T) {
	known := []string{ManagedFieldsChanged.String(), NameChanged.String(), NamespaceChanged.String()}
	allValid, unknown := IsAllValid(known)
	assert.Equal(t, []string([]string(nil)), unknown)
	assert.Equal(t, true, allValid)
}

func TestIsAllValid_MultipleInvalidSentinels(t *testing.T) {
	known := []string{
		ManagedFieldsChanged.String(), "invalid-1",
		NameChanged.String(), "invalid-2",
		NamespaceChanged.String(), "invalid-3",
	}
	allValid, unknown := IsAllValid(known)

	assert.Equal(t, []string{"invalid-1", "invalid-2", "invalid-3"}, unknown)
	assert.Equal(t, 3, len(unknown))
	assert.Equal(t, false, allValid)
}
