// pkg/kubeclient/orkadapter/crud.go
package orkadapter

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime"
	sigs "sigs.k8s.io/controller-runtime/pkg/client"
)

// ── Reader ────────────────────────────────────────────────────────────────────

// Get delegates to kubeclient
func (a *ctrlClientAdapter) Get(ctx context.Context, key sigs.ObjectKey, obj sigs.Object, opts ...sigs.GetOption) error {
	return a.k.Get(ctx, key, obj, getOptions(opts...))
}

// ── Writer ────────────────────────────────────────────────────────────────────

// Apply delegates to kubeclient.Apply
func (a *ctrlClientAdapter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...sigs.ApplyOption) error {
	return a.k.Apply(ctx, obj, applyOptions(opts...))
}

// Create delegates to kubeclient.Create
func (a *ctrlClientAdapter) Create(ctx context.Context, obj sigs.Object, opts ...sigs.CreateOption) error {
	return a.k.Create(ctx, obj, createOptions(opts...))
}

// Update delegates to kubeclient.Update
func (a *ctrlClientAdapter) Update(ctx context.Context, obj sigs.Object, opts ...sigs.UpdateOption) error {
	return a.k.Update(ctx, obj, updateOptions(opts...))
}

// Patch delegates to kubeclient.Patch
func (a *ctrlClientAdapter) Patch(ctx context.Context, obj sigs.Object, patch sigs.Patch, opts ...sigs.PatchOption) error {
	return a.k.Patch(ctx, obj, patch, patchOptions(opts...))
}

// Delete delegates to kubeclient.Delete
func (a *ctrlClientAdapter) Delete(ctx context.Context, obj sigs.Object, opts ...sigs.DeleteOption) error {
	return a.k.Delete(ctx, obj, deleteOptions(opts...))
}

// DeleteAllOf delegates to kubeclient.DeleteAllOf
func (a *ctrlClientAdapter) DeleteAllOf(ctx context.Context, obj sigs.Object, opts ...sigs.DeleteAllOfOption) error {
	deleteOpts, listOpts := deleteAllOfOptions(opts...)
	return a.k.DeleteAllOf(ctx, obj, deleteOpts, listOpts)
}
