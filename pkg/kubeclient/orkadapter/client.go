// pkg/kubeclient/orkadapter/client.go
package orkadapter

import (
	"context"
	"strings"

	"github.com/orkspace/orkestra/pkg/kubeclient"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	sigs "sigs.k8s.io/controller-runtime/pkg/client"
)

// ToClient wraps a kubeclient.Interface as a controller-runtime Client.
func ToClient(k kubeclient.Interface) sigs.Client {
	if k == nil {
		return nil
	}
	status, ok := k.(kubeclient.Subresource)
	if !ok {
		return &ctrlClientAdapter{k: k}
	}
	return &ctrlClientAdapter{k: k, s: status}

}

type ctrlClientAdapter struct {
	k kubeclient.Interface
	s kubeclient.Subresource
}

var _ sigs.Client = (*ctrlClientAdapter)(nil)

// ── Scheme / Mapper / GVK ─────────────────────────────────────────────────────

// Scheme returns the kubeclient runtime scheme.
func (a *ctrlClientAdapter) Scheme() *runtime.Scheme {
	return a.k.Scheme()
}

// RESTMapper returns the kubeclient REST mapper.
func (a *ctrlClientAdapter) RESTMapper() apimeta.RESTMapper {
	return a.k.RESTMapper()
}

// GroupVersionKindFor resolves the object's GVK from the kubeclient scheme.
func (a *ctrlClientAdapter) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return a.k.GroupVersionKindFor(obj)
}

// IsObjectNamespaced reports whether the object's resource is namespace-scoped.
func (a *ctrlClientAdapter) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return a.k.IsObjectNamespaced(obj)
}

// ── StatusClient ──────────────────────────────────────────────────────────────

// Status returns the controller-runtime-compatible status writer.
func (a *ctrlClientAdapter) Status() sigs.SubResourceWriter {
	if a.k == nil {
		return nil
	}

	return &ctrlStatusAdapter{s: a.s}
}

// ── SubResourceClientConstructor ─────────────────────────────────────────────

// SubResource returns the status client for the only currently supported subresource.
func (a *ctrlClientAdapter) SubResource(s string) sigs.SubResourceClient {
	if strings.ToLower(s) != "status" {
		return nil
	}
	if a.s == nil {
		return nil
	}
	return &ctrlStatusAdapter{s: a.s}
}

// ── Status adapter ────────────────────────────────────────────────────────────

type ctrlStatusAdapter struct {
	s kubeclient.Subresource
}

var _ sigs.SubResourceClient = (*ctrlStatusAdapter)(nil)

const statusSubresource = "status"

func (s *ctrlStatusAdapter) Get(ctx context.Context, obj sigs.Object, into sigs.Object, opts ...sigs.SubResourceGetOption) error {
	do := sigs.SubResourceGetOptions{}
	for _, opt := range opts {
		opt.ApplyToSubResourceGet(&do)
	}

	return s.s.GetSubResource(ctx, obj, into, statusSubresource, *do.AsGetOptions())
}

// Patch applies the patch specifically to the /status subresource.
func (s *ctrlStatusAdapter) Patch(ctx context.Context, obj sigs.Object, patch sigs.Patch, opts ...sigs.SubResourcePatchOption) error {
	do := sigs.SubResourcePatchOptions{}
	for _, opt := range opts {
		opt.ApplyToSubResourcePatch(&do)
	}

	return s.s.PatchSubResource(ctx, obj, patch, statusSubresource, *do.AsPatchOptions())
}

// Create on the status subresource is not supported by Kubernetes.
func (s *ctrlStatusAdapter) Create(ctx context.Context, obj sigs.Object, into sigs.Object, opts ...sigs.SubResourceCreateOption) error {
	do := sigs.SubResourceCreateOptions{}
	for _, opt := range opts {
		opt.ApplyToSubResourceCreate(&do)
	}
	return s.s.CreateSubResource(ctx, obj, into, statusSubresource, *do.AsCreateOptions())
}

// Update currently implements status update through a status patch.
func (s *ctrlStatusAdapter) Update(ctx context.Context, obj sigs.Object, opts ...sigs.SubResourceUpdateOption) error {
	do := sigs.SubResourceUpdateOptions{}
	for _, opt := range opts {
		opt.ApplyToSubResourceUpdate(&do)
	}
	return s.s.UpdateSubResource(ctx, obj, statusSubresource, *do.AsUpdateOptions())
}

func (s *ctrlStatusAdapter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...sigs.SubResourceApplyOption) error {
	do := sigs.SubResourceApplyOptions{}
	for _, opt := range opts {
		opt.ApplyToSubResourceApply(&do)
	}

	// The ApplyOptions are embedded directly in the SubResourceApplyOptions.
	// Use them to build metav1.ApplyOptions for the subresource call.
	applyOpts := metav1.ApplyOptions{
		DryRun:       do.DryRun,
		Force:        *do.Force,
		FieldManager: do.FieldManager,
	}

	return s.s.ApplySubResource(ctx, obj, statusSubresource, applyOpts)
}
