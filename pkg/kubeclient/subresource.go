package kubeclient

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Subresource provides operations for interacting with a Kubernetes
// subresource (typically /status) on a domain.Object. Implementations
// must route requests through the subresource path rather than the
// main resource endpoint.
type Subresource interface {
	// GetSubResource retrieves the subresource of obj into the provided domain.Object.
	GetSubResource(ctx context.Context, obj domain.Object, into domain.Object, subresource string, opts metav1.GetOptions) error

	// CreateSubResource creates the subresource for obj and writes the result into into.
	CreateSubResource(ctx context.Context, obj domain.Object, into domain.Object, subresource string, opts metav1.CreateOptions) error

	// UpdateSubResource updates the subresource of obj.
	UpdateSubResource(ctx context.Context, obj domain.Object, subresource string, opts metav1.UpdateOptions) error

	// PatchSubResource applies a patch to the subresource of obj.
	PatchSubResource(ctx context.Context, obj domain.Object, patch Patch, subresource string, opts metav1.PatchOptions) error

	// ApplySubResource applies a server-side apply configuration to the subresource.
	ApplySubResource(ctx context.Context, obj runtime.ApplyConfiguration, subresource string, opts metav1.ApplyOptions) error
}

var _ Subresource = (*Kubeclient)(nil)

// PatchSubResource applies the patch specifically to the /status subresource.
func (k *Kubeclient) PatchSubResource(ctx context.Context, obj domain.Object, patch Patch, subresource string, opts metav1.PatchOptions) error {
	data, err := patch.Data(obj)
	if err != nil {
		return fmt.Errorf("ctrlclient Status.Patch: compute patch: %w", err)
	}

	resource, err := k.resourceFor(obj)
	if err != nil {
		return err
	}

	_, err = resource.Patch(ctx, obj.GetName(), patch.Type(), data, opts, subresource)

	return err
}

// GetSubResource fetches the /status subresource of obj and decodes
// it into the provided domain.Object.
func (k *Kubeclient) GetSubResource(ctx context.Context, obj domain.Object, into domain.Object, subresource string, opts metav1.GetOptions) error {
	resource, err := k.resourceFor(obj)
	if err != nil {
		return err
	}

	result, err := resource.Get(ctx, obj.GetName(), opts, subresource)
	if err != nil {
		return err
	}

	return runtime.DefaultUnstructuredConverter.FromUnstructured(result.Object, into)
}

// CreateSubResource creates the /status subresource for the given object
// and decodes the server response into into.
func (k *Kubeclient) CreateSubResource(ctx context.Context, obj domain.Object, into domain.Object, subresource string, opts metav1.CreateOptions) error {
	resource, err := k.resourceFor(obj)
	if err != nil {
		return err
	}

	u, err := ToUnstructured(into)
	if err != nil {
		return err
	}

	result, err := resource.Create(ctx, u, opts, subresource)
	if err != nil {
		return err
	}

	return runtime.DefaultUnstructuredConverter.FromUnstructured(result.Object, into)
}

// UpdateSubResource updates the /status subresource of obj.
func (k *Kubeclient) UpdateSubResource(ctx context.Context, obj domain.Object, subresource string, opts metav1.UpdateOptions) error {
	resource, err := k.resourceFor(obj)
	if err != nil {
		return err
	}

	u, err := ToUnstructured(obj)
	if err != nil {
		return err
	}

	_, err = resource.Update(ctx, u, opts, subresource)

	return err
}

// ApplySubResource applies the provided configuration to the /status
// subresource via server-side apply.
func (k *Kubeclient) ApplySubResource(ctx context.Context, cfg runtime.ApplyConfiguration, subresource string, opts metav1.ApplyOptions) error {
	return k.runApply(ctx, cfg, opts, subresource)
}
