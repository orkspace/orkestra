//go:build integration

package kubeclient_test

import (
	"context"
	"testing"

	kubeclient "github.com/orkspace/orkestra/pkg/kubeclient"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
)

var probeGVR = schema.GroupVersionResource{
	Group:    "integration.orkestra.io",
	Version:  "v1",
	Resource: "probes",
}

// newKube builds a Kubeclient wired to the envtest REST config.
func newKube(t *testing.T) *kubeclient.Kubeclient {
	t.Helper()
	s := scheme.Scheme
	_ = serializer.NewCodecFactory(s) // ensure scheme is populated
	return kubeclient.NewForTesting(testCfg, testEnv.Dynamic, s)
}

// createProbe creates a Probe CR in the given namespace and returns it.
func createProbe(t *testing.T, ctx context.Context, name, namespace string) *unstructured.Unstructured {
	t.Helper()
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "integration.orkestra.io/v1",
			"kind":       "Probe",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"image":    "nginx:1.25",
				"replicas": int64(1),
			},
		},
	}
	created, err := testEnv.Dynamic.Resource(probeGVR).Namespace(namespace).
		Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create probe %s/%s: %v", namespace, name, err)
	}
	return created
}

var noOpPatchOpts = metav1.PatchOptions{}

// ── PatchFinalizers ───────────────────────────────────────────────────────────

func TestPatchFinalizers_AddFinalizer(t *testing.T) {
	ctx := context.Background()
	kube := newKube(t)
	obj := createProbe(t, ctx, "fin-add", "default")

	finalizers := []string{"orkestra.io/cleanup"}
	if err := kube.PatchFinalizers(ctx, obj, finalizers, noOpPatchOpts); err != nil {
		t.Fatalf("PatchFinalizers: %v", err)
	}

	got, err := testEnv.Dynamic.Resource(probeGVR).Namespace("default").
		Get(ctx, "fin-add", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get after patch: %v", err)
	}
	actual := got.GetFinalizers()
	if len(actual) != 1 || actual[0] != "orkestra.io/cleanup" {
		t.Errorf("expected finalizer orkestra.io/cleanup, got %v", actual)
	}
}

func TestPatchFinalizers_RemoveFinalizer(t *testing.T) {
	ctx := context.Background()
	kube := newKube(t)
	obj := createProbe(t, ctx, "fin-remove", "default")

	// Add first
	if err := kube.PatchFinalizers(ctx, obj, []string{"orkestra.io/cleanup"}, noOpPatchOpts); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Fetch fresh — PatchFinalizers reads name/namespace from the object passed in,
	// not resourceVersion, so the original obj is fine for the remove call
	if err := kube.PatchFinalizers(ctx, obj, []string{}, noOpPatchOpts); err != nil {
		t.Fatalf("remove: %v", err)
	}

	got, err := testEnv.Dynamic.Resource(probeGVR).Namespace("default").
		Get(ctx, "fin-remove", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get after remove: %v", err)
	}
	if len(got.GetFinalizers()) != 0 {
		t.Errorf("expected no finalizers, got %v", got.GetFinalizers())
	}
}

func TestPatchFinalizers_Idempotent(t *testing.T) {
	ctx := context.Background()
	kube := newKube(t)
	obj := createProbe(t, ctx, "fin-idem", "default")

	finalizers := []string{"orkestra.io/cleanup"}
	// Patch twice with same value — must not error
	if err := kube.PatchFinalizers(ctx, obj, finalizers,noOpPatchOpts); err != nil {
		t.Fatalf("first patch: %v", err)
	}
	if err := kube.PatchFinalizers(ctx, obj, finalizers, noOpPatchOpts); err != nil {
		t.Fatalf("second patch (idempotent): %v", err)
	}

	got, _ := testEnv.Dynamic.Resource(probeGVR).Namespace("default").
		Get(ctx, "fin-idem", metav1.GetOptions{})
	// Merge patch: setting the same list twice must not duplicate
	if len(got.GetFinalizers()) != 1 {
		t.Errorf("expected exactly 1 finalizer after idempotent patch, got %v", got.GetFinalizers())
	}
}

func TestPatchFinalizers_DoesNotTouchOtherFields(t *testing.T) {
	ctx := context.Background()
	kube := newKube(t)
	obj := createProbe(t, ctx, "fin-isolation", "default")

	if err := kube.PatchFinalizers(ctx, obj, []string{"orkestra.io/test"}, noOpPatchOpts); err != nil {
		t.Fatalf("patch: %v", err)
	}

	got, _ := testEnv.Dynamic.Resource(probeGVR).Namespace("default").
		Get(ctx, "fin-isolation", metav1.GetOptions{})

	// spec must be untouched — merge patch only touches metadata.finalizers
	img, _, _ := unstructured.NestedString(got.Object, "spec", "image")
	if img != "nginx:1.25" {
		t.Errorf("spec.image was modified by finalizer patch, got %q", img)
	}
}

// ── PatchLabels ───────────────────────────────────────────────────────────────

func TestPatchLabels_AddsLabels(t *testing.T) {
	ctx := context.Background()
	kube := newKube(t)
	obj := createProbe(t, ctx, "lbl-add", "default")

	labels := map[string]string{"env": "test", "managed-by": "orkestra"}
	if err := kube.PatchLabels(ctx, obj, nil, labels, noOpPatchOpts); err != nil {
		t.Fatalf("PatchLabels: %v", err)
	}

	got, _ := testEnv.Dynamic.Resource(probeGVR).Namespace("default").
		Get(ctx, "lbl-add", metav1.GetOptions{})
	for k, want := range labels {
		if got.GetLabels()[k] != want {
			t.Errorf("label %s: expected %q, got %q", k, want, got.GetLabels()[k])
		}
	}
}

func TestPatchLabels_DoesNotRemoveExistingLabels(t *testing.T) {
	ctx := context.Background()
	kube := newKube(t)

	// Create with an initial label
	raw := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "integration.orkestra.io/v1",
		"kind":       "Probe",
		"metadata": map[string]interface{}{
			"name":      "lbl-preserve",
			"namespace": "default",
			"labels":    map[string]interface{}{"existing": "keep-me"},
		},
		"spec": map[string]interface{}{"image": "nginx:1.25", "replicas": int64(1)},
	}}
	created, err := testEnv.Dynamic.Resource(probeGVR).Namespace("default").Create(ctx, raw, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := kube.PatchLabels(ctx, created, nil, map[string]string{"new": "label"}, noOpPatchOpts); err != nil {
		t.Fatalf("PatchLabels: %v", err)
	}

	got, _ := testEnv.Dynamic.Resource(probeGVR).Namespace("default").
		Get(ctx, "lbl-preserve", metav1.GetOptions{})

	// Merge patch merges labels — existing keys must survive
	if got.GetLabels()["existing"] != "keep-me" {
		t.Errorf("existing label was removed, labels: %v", got.GetLabels())
	}
	if got.GetLabels()["new"] != "label" {
		t.Errorf("new label not set, labels: %v", got.GetLabels())
	}
}

// ── PatchStatus ───────────────────────────────────────────────────────────────

func TestPatchStatus_SetsStatusFields(t *testing.T) {
	ctx := context.Background()
	kube := newKube(t)
	obj := createProbe(t, ctx, "status-set", "default")

	status := map[string]interface{}{
		"phase":   "Running",
		"message": "all good",
	}
	if err := kube.PatchStatus(ctx, obj, status, noOpPatchOpts); err != nil {
		t.Fatalf("PatchStatus: %v", err)
	}

	got, _ := testEnv.Dynamic.Resource(probeGVR).Namespace("default").
		Get(ctx, "status-set", metav1.GetOptions{})

	phase, _, _ := unstructured.NestedString(got.Object, "status", "phase")
	if phase != "Running" {
		t.Errorf("expected status.phase=Running, got %q", phase)
	}
}

func TestPatchStatus_EmptyFieldsIsNoOp(t *testing.T) {
	ctx := context.Background()
	kube := newKube(t)
	obj := createProbe(t, ctx, "status-noop", "default")

	// PatchStatus with empty map must return nil and touch nothing
	if err := kube.PatchStatus(ctx, obj, map[string]interface{}{}, noOpPatchOpts); err != nil {
		t.Errorf("empty PatchStatus must not error: %v", err)
	}
}

func TestPatchStatus_DoesNotTouchSpec(t *testing.T) {
	ctx := context.Background()
	kube := newKube(t)
	obj := createProbe(t, ctx, "status-isolation", "default")

	if err := kube.PatchStatus(ctx, obj, map[string]interface{}{"phase": "Ready"}, noOpPatchOpts); err != nil {
		t.Fatalf("PatchStatus: %v", err)
	}

	got, _ := testEnv.Dynamic.Resource(probeGVR).Namespace("default").
		Get(ctx, "status-isolation", metav1.GetOptions{})

	img, _, _ := unstructured.NestedString(got.Object, "spec", "image")
	if img != "nginx:1.25" {
		t.Errorf("spec.image was modified by status patch, got %q", img)
	}
}
