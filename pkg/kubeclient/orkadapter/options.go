// pkg/kubeclient/orkadapter/helper.go
package orkadapter

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sigs "sigs.k8s.io/controller-runtime/pkg/client"
)

// getOptions converts controller-runtime GetOptions to client-go GetOptions.
func getOptions(opts ...sigs.GetOption) metav1.GetOptions {
	o := &sigs.GetOptions{}
	for _, opt := range opts {
		opt.ApplyToGet(o)
	}
	return *o.AsGetOptions()
}

// createOptions converts controller-runtime CreateOptions to client-go CreateOptions.
func createOptions(opts ...sigs.CreateOption) metav1.CreateOptions {
	o := &sigs.CreateOptions{}
	for _, opt := range opts {
		opt.ApplyToCreate(o)
	}
	return *o.AsCreateOptions()
}

// applyOptions converts controller-runtime CreateOptions to client-go ApplyOptions.
func applyOptions(opts ...sigs.ApplyOption) metav1.ApplyOptions {
	do := &sigs.ApplyOptions{}
	for _, opt := range opts {
		opt.ApplyToApply(do)
	}

	return metav1.ApplyOptions{
		DryRun:       do.DryRun,
		Force:        *do.Force,
		FieldManager: do.FieldManager,
	}
}

// patchOptions converts controller-runtime PatchOptions to client-go PatchOptions.
func patchOptions(opts ...sigs.PatchOption) metav1.PatchOptions {
	o := &sigs.PatchOptions{
		Raw: &metav1.PatchOptions{},
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt.ApplyToPatch(o)
	}

	if o.Raw == nil {
		o.Raw = &metav1.PatchOptions{}
	}

	return *o.Raw
}

// deleteOptions converts controller-runtime DeleteOptions to client-go DeleteOptions.
func deleteOptions(opts ...sigs.DeleteOption) metav1.DeleteOptions {
	o := &sigs.DeleteOptions{}
	for _, opt := range opts {
		opt.ApplyToDelete(o)
	}
	return *o.AsDeleteOptions()
}

// deleteAllOfOptions converts controller-runtime DeleteAllOfOptions to client-go DeleteOptions.
// returns metav1.DeleteOptions and sigs.DeleteAllOfOptions
func deleteAllOfOptions(opts ...sigs.DeleteAllOfOption) (metav1.DeleteOptions, metav1.ListOptions) {
	o := &sigs.DeleteAllOfOptions{}
	o.ApplyOptions(opts)

	return *o.AsDeleteOptions(), *o.AsListOptions()
}

// updateOptions converts controller-runtime UpdateOptions to client-go UpdateOptions.
func updateOptions(opts ...sigs.UpdateOption) metav1.UpdateOptions {
	o := &sigs.UpdateOptions{}
	for _, opt := range opts {
		opt.ApplyToUpdate(o)
	}
	return *o.AsUpdateOptions()
}
