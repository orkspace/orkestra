package simulate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// CRUD stubs — record operations. Get always returns NotFound so the reconciler
// takes the Create path on every simulated cycle, producing visible create ops.

func (f *FakeKubeclient) Get(_ context.Context, key domain.ObjectKey, obj domain.Object, opts metav1.GetOptions) error {
	f.shared.mu.Lock()
	f.shared.ops = append(f.shared.ops, Op{
		Cycle:     f.shared.currentCycle,
		Verb:      "get",
		Resource:  resourceNameFromObject(obj),
		Namespace: key.Namespace,
		Name:      key.Name,
		At:        time.Now(),
	})
	f.shared.mu.Unlock()
	return fakeNotFound(key.Name)
}

func (f *FakeKubeclient) Create(_ context.Context, obj domain.Object, opts metav1.CreateOptions) error {
	f.shared.mu.Lock()
	f.shared.ops = append(f.shared.ops, Op{
		Cycle:     f.shared.currentCycle,
		Verb:      "create",
		Resource:  resourceNameFromObject(obj),
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
		At:        time.Now(),
	})
	f.shared.mu.Unlock()
	return nil
}

func (f *FakeKubeclient) Update(_ context.Context, obj domain.Object, opts metav1.UpdateOptions) error {
	f.shared.mu.Lock()
	f.shared.ops = append(f.shared.ops, Op{
		Cycle:     f.shared.currentCycle,
		Verb:      "update",
		Resource:  resourceNameFromObject(obj),
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		At:        time.Now(),
	})
	f.shared.mu.Unlock()
	return nil
}

func (f *FakeKubeclient) Apply(_ context.Context, cfg runtime.ApplyConfiguration, _ metav1.ApplyOptions) error {
	f.shared.mu.Lock()
	defer f.shared.mu.Unlock()
	f.shared.ops = append(f.shared.ops, Op{
		Cycle: f.shared.currentCycle,
		Verb:  "apply",
		At:    time.Now(),
	})
	return nil
}

func (f *FakeKubeclient) ApplySubResource(_ context.Context, cfg runtime.ApplyConfiguration, subresource string, _ metav1.ApplyOptions) error {
	f.shared.mu.Lock()
	defer f.shared.mu.Unlock()

	f.shared.ops = append(f.shared.ops, Op{
		Cycle:       f.shared.currentCycle,
		Verb:        "apply",
		Subresource: subresource,
		At:          time.Now(),
	})

	return nil
}

func (f *FakeKubeclient) Patch(_ context.Context, obj domain.Object, patch kubeclient.Patch, opts metav1.PatchOptions) error {
	f.shared.mu.Lock()
	defer f.shared.mu.Unlock()

	f.shared.ops = append(f.shared.ops, Op{
		Cycle:     f.shared.currentCycle,
		Verb:      "patch",
		Resource:  resourceNameFromObject(obj),
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		At:        time.Now(),
	})
	return nil
}

func (f *FakeKubeclient) Delete(_ context.Context, obj domain.Object, opts metav1.DeleteOptions) error {
	f.shared.mu.Lock()
	defer f.shared.mu.Unlock()
	f.shared.ops = append(f.shared.ops, Op{
		Cycle:     f.shared.currentCycle,
		Verb:      "delete",
		Resource:  resourceNameFromObject(obj),
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		At:        time.Now(),
	})
	return nil
}

func (f *FakeKubeclient) DeleteAllOf(_ context.Context, obj domain.Object, _ metav1.DeleteOptions, _ metav1.ListOptions) error {
	f.shared.mu.Lock()
	defer f.shared.mu.Unlock()
	f.shared.ops = append(f.shared.ops, Op{
		Cycle:     f.shared.currentCycle,
		Verb:      "delete",
		Resource:  resourceNameFromObject(obj),
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		At:        time.Now(),
	})
	return nil
}

// fakeNotFound returns an error that satisfies k8s.io/apimachinery/pkg/api/errors.IsNotFound.
func fakeNotFound(name string) error {
	return k8serrors.NewNotFound(schema.GroupResource{}, name)
}

func resourceNameFromObject(obj runtime.Object) string {
	t := fmt.Sprintf("%T", obj)
	if idx := strings.LastIndex(t, "."); idx >= 0 {
		t = t[idx+1:]
	}
	return strings.ToLower(t) + "s"
}

func (f *FakeKubeclient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return kubeclient.GroupVersionKindFor(f, obj)
}

func (f *FakeKubeclient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return kubeclient.IsObjectNamespaced(f, obj)
}

// compile-time check
var _ kubeclient.Interface = (*FakeKubeclient)(nil)
