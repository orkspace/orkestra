// pkg/runners/cluster_scoped_deletion.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/children"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	orkcrb "github.com/orkspace/orkestra/pkg/resources/clusterrolebindings"
	orkcr "github.com/orkspace/orkestra/pkg/resources/clusterroles"
	orkcust "github.com/orkspace/orkestra/pkg/resources/customresources"
	orkns "github.com/orkspace/orkestra/pkg/resources/namespaces"
	orkpv "github.com/orkspace/orkestra/pkg/resources/pvs"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// DeleteOwnedClusterScopedResources explicitly deletes all cluster-scoped resources declared across
// onCreate, onReconcile, and onDelete that are owned by this CR.
//
// Kubernetes GC does not handle this automatically: owner references from
// namespace-scoped resources (CRs) to cluster-scoped resources (Namespaces, ClusterRoles,
// ClusterRoleBindings, PersistentVolumes) are not honoured by the garbage collector.
// Explicit deletion is required.
//
// This should be called during the deletion path of the reconciler.
func DeleteOwnedClusterScopedResources(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	obj domain.Object,
	box orktypes.OperatorBoxConfig,
) error {
	if err := deleteOwnedNamespaces(ctx, kube, resolver, obj, box); err != nil {
		return err
	}
	if err := deleteOwnedClusterRoles(ctx, kube, resolver, obj, box); err != nil {
		return err
	}
	if err := deleteOwnedClusterRoleBindings(ctx, kube, resolver, obj, box); err != nil {
		return err
	}
	if err := deleteOwnedPersistentVolumes(ctx, kube, resolver, obj, box); err != nil {
		return err
	}
	return deleteOwnedCustomResources(ctx, kube, resolver, obj, box)
}

func deleteOwnedNamespaces(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	obj domain.Object,
	box orktypes.OperatorBoxConfig,
) error {
	var srcs []orktypes.NamespaceTemplateSource
	for _, hook := range allHooks(box) {
		if hook != nil {
			srcs = append(srcs, hook.Namespaces...)
		}
	}
	for i, src := range children.ExpandForEachNamespaces(resolver, srcs) {
		name, _ := resolver.Resolve(src.Name)
		if name == "" {
			continue
		}
		if err := orkns.DeleteIfOwned(ctx, kube, obj, name); err != nil {
			return fmt.Errorf("namespace[%d] %q: %w", i, name, err)
		}
	}
	return nil
}

func deleteOwnedClusterRoles(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	obj domain.Object,
	box orktypes.OperatorBoxConfig,
) error {
	var srcs []orktypes.ClusterRoleTemplateSource
	for _, hook := range allHooks(box) {
		if hook != nil {
			srcs = append(srcs, hook.ClusterRoles...)
		}
	}
	for i, src := range children.ExpandForEachClusterRoles(resolver, srcs) {
		name, _ := resolver.Resolve(src.Name)
		if name == "" {
			continue
		}
		if err := orkcr.DeleteIfOwned(ctx, kube, obj, name); err != nil {
			return fmt.Errorf("clusterrole[%d] %q: %w", i, name, err)
		}
	}
	return nil
}

func deleteOwnedClusterRoleBindings(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	obj domain.Object,
	box orktypes.OperatorBoxConfig,
) error {
	var srcs []orktypes.ClusterRoleBindingTemplateSource
	for _, hook := range allHooks(box) {
		if hook != nil {
			srcs = append(srcs, hook.ClusterRoleBindings...)
		}
	}
	for i, src := range children.ExpandForEachClusterRoleBindings(resolver, srcs) {
		name, _ := resolver.Resolve(src.Name)
		if name == "" {
			continue
		}
		if err := orkcrb.DeleteIfOwned(ctx, kube, obj, name); err != nil {
			return fmt.Errorf("clusterrolebinding[%d] %q: %w", i, name, err)
		}
	}
	return nil
}

func deleteOwnedPersistentVolumes(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	obj domain.Object,
	box orktypes.OperatorBoxConfig,
) error {
	var srcs []orktypes.PVTemplateSource
	for _, hook := range allHooks(box) {
		if hook != nil {
			srcs = append(srcs, hook.PersistentVolumes...)
		}
	}
	for i, src := range children.ExpandForEachPVs(resolver, srcs) {
		name, _ := resolver.Resolve(src.Name)
		if name == "" {
			continue
		}
		if err := orkpv.DeleteIfOwned(ctx, kube, obj, name); err != nil {
			return fmt.Errorf("persistentvolume[%d] %q: %w", i, name, err)
		}
	}
	return nil
}

func deleteOwnedCustomResources(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	obj domain.Object,
	box orktypes.OperatorBoxConfig,
) error {
	var srcs []orktypes.CustomResourceTemplateSource
	for _, hook := range allHooks(box) {
		if hook != nil {
			srcs = append(srcs, hook.CustomResource...)
		}
	}
	for i, src := range children.ExpandForEachCustomResources(resolver, srcs) {
		if src.IsNamespaced() {
			continue // GC handles namespace-scoped custom resources
		}
		name, _ := resolver.Resolve(src.Metadata.Name)
		if name == "" {
			continue
		}
		if err := orkcust.DeleteIfOwned(ctx, kube, obj, name, "", src.APIVersion, src.Kind); err != nil {
			return fmt.Errorf("customresource[%d] %q: %w", i, name, err)
		}
	}
	return nil
}

// allHooks returns all three lifecycle hook blocks in declaration order.
func allHooks(box orktypes.OperatorBoxConfig) []*orktypes.HookTemplates {
	return []*orktypes.HookTemplates{box.OnCreate, box.OnReconcile, box.OnDelete}
}
