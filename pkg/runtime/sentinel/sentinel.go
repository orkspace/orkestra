// Package sentinel computes event-time sentinel values for preReconcile gates.
//
// Sentinels are computed in the informer's UpdateFunc by comparing oldObj and
// newObj. The result is carried through QueueItem.SentinelMap so both
// enqueueGate and reconcileGate can share the same preReconcile resolver
// without oldObj being available at dequeue time.
//
// This package is the canonical home for sentinel names. pkg/types imports it
// for the typed Sentinel constants; pkg/runtime/informer imports it for Compute.
// The package itself imports only stdlib and k8s apimachinery so neither
// direction creates an import cycle.
package sentinel

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Sentinel is the string type for event-time sentinel names declared under
// preReconcile.sentinels and used in enqueueGate/reconcileGate templates.
type Sentinel string

const (
	GenerationChanged  Sentinel = "generationChanged"
	LabelsChanged      Sentinel = "labelsChanged"
	AnnotationsChanged Sentinel = "annotationsChanged"
	// DeletionStarted is true when the object's DeletionTimestamp transitions
	// from nil to non-nil — i.e. the moment a delete is issued. Only computable
	// at event time (old vs new); by reconcile time the timestamp is already set.
	DeletionStarted Sentinel = "deletionStarted"
	// FinalizersChanged is true when the finalizer list differs between old and new.
	FinalizersChanged Sentinel = "finalizersChanged"

	// New Sentinels attempts to make available every method in metav1.Object
	// May not all be useful today, but still available
	NameChanged                       Sentinel = "nameChanged"
	NamespaceChanged                  Sentinel = "namespaceChanged"
	GenerateNameChanged               Sentinel = "generateNameChanged"
	UIDChanged                        Sentinel = "uidChanged"
	ResourceVersionChanged            Sentinel = "resourceVersionChanged"
	CreationTimestampChanged          Sentinel = "creationTimestampChanged"
	DeletionGracePeriodSecondsChanged Sentinel = "deletionGracePeriodSecondsChanged"
	OwnerReferenceChanged             Sentinel = "ownerReferenceChanged"
	ManagedFieldsChanged              Sentinel = "managedFieldsChanged"
)

// String stringifies a sentinel
func (s Sentinel) String() string {
	return string(s)
}

// ValidSentinels returns all known sentinel names in declaration order.
func ValidSentinels() []string {
	return []string{
		string(GenerationChanged),
		string(LabelsChanged),
		string(AnnotationsChanged),
		string(DeletionStarted),
		string(FinalizersChanged),
		string(NameChanged),
		string(NamespaceChanged),
		string(GenerateNameChanged),
		string(UIDChanged),
		string(ResourceVersionChanged),
		string(CreationTimestampChanged),
		string(DeletionGracePeriodSecondsChanged),
		string(OwnerReferenceChanged),
		string(ManagedFieldsChanged),
	}
}

// IsValid reports whether s is a known sentinel name.
func IsValid(s string) bool {
	for _, sent := range ValidSentinels() {
		if sent == s {
			return true
		}
	}
	return false
}

// IsAllValid reports whether s are all known sentinel names.
// returns bool and a list of unknown sentinels
func IsAllValid(s []string) (bool, []string) {
	var unknown []string
	for _, sent := range s {
		if !IsValid(sent) {
			unknown = append(unknown, sent)
		}
	}
	if len(unknown) > 0 {
		return false, unknown
	}
	return true, nil
}

// Compute returns the sentinel values for the declared names by comparing
// oldObj and newObj at UpdateFunc time.
//
// Returns nil when declared is empty (common path — no allocation).
func Compute(declared []string, oldObj, newObj metav1.Object) map[string]string {
	if len(declared) == 0 {
		return nil
	}
	result := make(map[string]string, len(declared))
	for _, name := range declared {
		result[name] = computeOne(name, oldObj, newObj)
	}
	return result
}

func computeOne(name string, old, new metav1.Object) string {
	switch Sentinel(name) {
	case GenerationChanged:
		return boolStr(old.GetGeneration() != new.GetGeneration())
	case LabelsChanged:
		return boolStr(!reflect.DeepEqual(old.GetLabels(), new.GetLabels()))
	case AnnotationsChanged:
		return boolStr(!reflect.DeepEqual(old.GetAnnotations(), new.GetAnnotations()))
	case DeletionStarted:
		return boolStr(old.GetDeletionTimestamp() == nil && new.GetDeletionTimestamp() != nil)
	case FinalizersChanged:
		return boolStr(!reflect.DeepEqual(old.GetFinalizers(), new.GetFinalizers()))
	case NameChanged:
		return boolStr(old.GetName() != new.GetName())
	case NamespaceChanged:
		return boolStr(old.GetNamespace() != new.GetNamespace())
	case GenerateNameChanged:
		return boolStr(old.GetGenerateName() != new.GetGenerateName())
	case UIDChanged:
		return boolStr(old.GetUID() != new.GetUID())
	case ResourceVersionChanged:
		return boolStr(old.GetResourceVersion() != new.GetResourceVersion())
	case CreationTimestampChanged:
		return boolStr(old.GetCreationTimestamp() != new.GetCreationTimestamp())
	case DeletionGracePeriodSecondsChanged:
		return boolStr(!reflect.DeepEqual(old.GetDeletionGracePeriodSeconds(), new.GetDeletionGracePeriodSeconds()))
	case OwnerReferenceChanged:
		return boolStr(!reflect.DeepEqual(old.GetOwnerReferences(), new.GetOwnerReferences()))
	case ManagedFieldsChanged:
		return boolStr(!reflect.DeepEqual(old.GetManagedFields(), new.GetManagedFields()))
	default:
		return ""
	}
}

// Helpers
func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func int64Ptr(v int64) *int64 { return &v }
func boolPtr(v bool) *bool    { return &v }
