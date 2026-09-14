//go:build integration

package informer_test

import (
	"context"
	"testing"
	"time"

	"github.com/orkspace/orkestra/pkg/konfig"
	informerpkg "github.com/orkspace/orkestra/pkg/runtime/informer"
	"github.com/orkspace/orkestra/pkg/runtime/queue"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

var (
	probeGVR = schema.GroupVersionResource{
		Group:    "integration.orkestra.io",
		Version:  "v1",
		Resource: "probes",
	}
	probeGVK = schema.GroupVersionKind{
		Group:   "integration.orkestra.io",
		Version: "v1",
		Kind:    "Probe",
	}
	nsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
)

// newTestFactory builds an informer Factory wired to the envtest REST config.
// The scheme uses the default runtime.NewScheme() — the k8s scheme has special
// handling for unstructured objects: when the object's Kind is set,
// scheme.ObjectKinds returns the GVK from the object itself.
func newTestFactory(reg *queue.QueueRegistry, defaultWq *queue.Workqueue) *informerpkg.Factory {
	return informerpkg.SharedInformerFactory(testCfg, informerpkg.FactoryOptions{
		QueueRegistry: reg,
		DefaultWq:     defaultWq,
		Scheme:        runtime.NewScheme(),
		Konfig:        konfig.NewDefaultKonfig(),
	})
}

// probeExampleObj returns an *unstructured.Unstructured with the Probe GVK set.
// Passing an example obj with Kind set is required for the factory to resolve
// the GVK via scheme.ObjectKinds — the scheme reads Kind from unstructured objects.
func probeExampleObj() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(probeGVK)
	return obj
}

// createNamespace creates a namespace in envtest, ignoring AlreadyExists errors.
func createNamespace(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	ns := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]interface{}{"name": name},
	}}
	_, err := testEnv.Dynamic.Resource(nsGVR).Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !isAlreadyExists(err) {
		t.Fatalf("create namespace %s: %v", name, err)
	}
}

// createProbeInNs creates a Probe CR in the given namespace.
func createProbeInNs(t *testing.T, ctx context.Context, name, namespace string) {
	t.Helper()
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "integration.orkestra.io/v1",
		"kind":       "Probe",
		"metadata":   map[string]interface{}{"name": name, "namespace": namespace},
		"spec":       map[string]interface{}{"image": "nginx:1.25", "replicas": int64(1)},
	}}
	if _, err := testEnv.Dynamic.Resource(probeGVR).Namespace(namespace).
		Create(ctx, obj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create probe %s/%s: %v", namespace, name, err)
	}
}

// isAlreadyExists returns true for 409 Conflict (already exists) API errors.
func isAlreadyExists(err error) bool {
	return err != nil && containsAny(err.Error(), "already exists")
}

func containsAny(s string, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// clusterScopedLW returns a ListerWatcher that watches ALL namespaces for Probe CRs.
func clusterScopedLW(ctx context.Context) *cache.ListWatch {
	return &cache.ListWatch{
		ListWithContextFunc: func(lCtx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
			return testEnv.Dynamic.Resource(probeGVR).Namespace("").List(lCtx, opts)
		},
		WatchFuncWithContext: func(wCtx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			return testEnv.Dynamic.Resource(probeGVR).Namespace("").Watch(wCtx, opts)
		},
	}
}

// drainQueue collects all items currently in the queue (non-blocking).
func drainQueue(wq *queue.Workqueue) []queue.QueueItem {
	var items []queue.QueueItem
	for wq.Depth() > 0 {
		item, shutdown := wq.Queue().Get()
		if shutdown {
			break
		}
		items = append(items, item)
		wq.Queue().Done(item)
	}
	return items
}

// waitForQueueDepth polls until the queue reaches at least minDepth or the deadline passes.
func waitForQueueDepth(wq *queue.Workqueue, minDepth int, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		if wq.Depth() >= minDepth {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestNamespaceFilter_AllowedEventsReachQueue verifies that a Probe created in
// the allowed namespace is enqueued by the informer factory.
func TestNamespaceFilter_AllowedEventsReachQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reg := queue.NewQueueRegistry()
	defaultWq := queue.NewWorkqueue("default")
	factory := newTestFactory(reg, defaultWq)

	probeGVKStr := probeGVK.String()

	// Register per-CRD queue and namespace filter before starting
	wq := reg.Register(probeGVKStr, nil)
	factory.RegisterNamespaceFilter(probeGVKStr, &informerpkg.NamespaceFilter{
		AllowedNamespaces: []string{"filter-allowed"},
	})

	// Register informer using the cluster-scoped dynamic ListerWatcher
	factory.ForListerWatcher(clusterScopedLW(ctx), probeExampleObj(), ctx, informerpkg.Options{
		Name: "probe-filter-allowed-test",
	})

	if err := factory.Start(ctx); err != nil {
		t.Fatalf("factory.Start: %v", err)
	}
	if !factory.WaitForCacheSync(ctx) {
		t.Fatal("cache sync timed out")
	}

	// Create namespaces
	createNamespace(t, ctx, "filter-allowed")

	// Create a Probe in the allowed namespace
	createProbeInNs(t, ctx, "probe-in-allowed", "filter-allowed")

	// Wait for the event to arrive in the queue
	if !waitForQueueDepth(wq, 1, time.Now().Add(10*time.Second)) {
		t.Fatal("timed out waiting for allowed-namespace event to reach queue")
	}

	items := drainQueue(wq)
	if len(items) == 0 {
		t.Fatal("expected at least one item in queue, got none")
	}
	expectedKey := "filter-allowed/probe-in-allowed"
	for _, item := range items {
		if item.Key == expectedKey {
			return // found — test passes
		}
	}
	t.Errorf("expected queue item with key %q, queue contained: %v", expectedKey, items)
}

// TestNamespaceFilter_BlockedEventsNeverReachQueue verifies that a Probe created
// in a blocked namespace is dropped by the namespace filter and never enqueued.
func TestNamespaceFilter_BlockedEventsNeverReachQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reg := queue.NewQueueRegistry()
	defaultWq := queue.NewWorkqueue("default")
	factory := newTestFactory(reg, defaultWq)

	probeGVKStr := probeGVK.String()

	wq := reg.Register(probeGVKStr, nil)
	factory.RegisterNamespaceFilter(probeGVKStr, &informerpkg.NamespaceFilter{
		// Only "filter-allowed2" is permitted — "filter-blocked" must be dropped.
		AllowedNamespaces: []string{"filter-allowed2"},
	})

	factory.ForListerWatcher(clusterScopedLW(ctx), probeExampleObj(), ctx, informerpkg.Options{
		Name: "probe-filter-blocked-test",
	})

	if err := factory.Start(ctx); err != nil {
		t.Fatalf("factory.Start: %v", err)
	}
	if !factory.WaitForCacheSync(ctx) {
		t.Fatal("cache sync timed out")
	}

	// Create the blocked namespace and a Probe inside it
	createNamespace(t, ctx, "filter-blocked")
	createProbeInNs(t, ctx, "probe-in-blocked", "filter-blocked")

	// Give the Watch event enough time to arrive and be processed.
	// If the filter is working correctly, nothing should appear in the queue.
	time.Sleep(2 * time.Second)

	if wq.Depth() > 0 {
		items := drainQueue(wq)
		t.Errorf("expected empty queue after blocked-namespace event, got %v", items)
	}
}

// TestNamespaceFilter_MixedNamespacesOnlyAllowedReachQueue creates Probes in both
// an allowed and a blocked namespace and asserts only the allowed one is enqueued.
func TestNamespaceFilter_MixedNamespacesOnlyAllowedReachQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reg := queue.NewQueueRegistry()
	defaultWq := queue.NewWorkqueue("default")
	factory := newTestFactory(reg, defaultWq)

	probeGVKStr := probeGVK.String()

	wq := reg.Register(probeGVKStr, nil)
	factory.RegisterNamespaceFilter(probeGVKStr, &informerpkg.NamespaceFilter{
		AllowedNamespaces: []string{"mix-allowed"},
	})

	factory.ForListerWatcher(clusterScopedLW(ctx), probeExampleObj(), ctx, informerpkg.Options{
		Name: "probe-filter-mixed-test",
	})

	if err := factory.Start(ctx); err != nil {
		t.Fatalf("factory.Start: %v", err)
	}
	if !factory.WaitForCacheSync(ctx) {
		t.Fatal("cache sync timed out")
	}

	createNamespace(t, ctx, "mix-allowed")
	createNamespace(t, ctx, "mix-blocked")

	// Create Probe in blocked namespace first, then allowed
	createProbeInNs(t, ctx, "probe-blocked", "mix-blocked")
	createProbeInNs(t, ctx, "probe-allowed", "mix-allowed")

	// Wait for the allowed-namespace event
	if !waitForQueueDepth(wq, 1, time.Now().Add(10*time.Second)) {
		t.Fatal("timed out waiting for allowed event")
	}

	// Drain and assert: only "mix-allowed/probe-allowed" should be present
	items := drainQueue(wq)
	for _, item := range items {
		if item.Key == "mix-blocked/probe-blocked" {
			t.Errorf("blocked namespace event leaked into queue: %v", item)
		}
	}

	found := false
	for _, item := range items {
		if item.Key == "mix-allowed/probe-allowed" {
			found = true
		}
	}
	if !found {
		t.Errorf("allowed event not found in queue; got: %v", items)
	}
}
