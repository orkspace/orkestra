// pkg/resources/clusterrolebindings/clusterrolebinding.go
package clusterrolebindings

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// ResolvedClusterRoleBindingSpec is the fully resolved ClusterRoleBinding specification.
type ResolvedClusterRoleBindingSpec struct {
	Name     string
	Labels   map[string]string
	RoleRef  rbacv1.RoleRef
	Subjects []rbacv1.Subject
	Sleep    string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}

// Create creates a ClusterRoleBinding if it does not already exist.
// Idempotent — skips if the ClusterRoleBinding already exists.
// ClusterRoleBindings are cluster-scoped; ownership is tracked via the orkestra.io/owner label.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedClusterRoleBindingSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("clusterrolebinding.Create: invalid spec: %w", err)
	}
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().RbacV1().ClusterRoleBindings().Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("clusterrolebinding.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("clusterrolebinding", spec.Name).
			Msg("clusterrolebinding already exists — skipping create")
		return nil
	}

	crb := buildClusterRoleBinding(owner, spec)

	_, err = kube.Clientset().RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("clusterrolebinding.Create: creating %q: %w", spec.Name, err)
	}

	logger.Info().
		Str("clusterrolebinding", spec.Name).
		Str("owner", owner.GetName()).
		Msg("clusterrolebinding created")

	return nil
}

// Apply creates or updates a ClusterRoleBinding using Server-Side Apply.
// RoleRef is immutable — if SSA is rejected due to a changed roleRef,
// the binding is deleted and recreated.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedClusterRoleBindingSpec) error {
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	crb := buildClusterRoleBinding(owner, spec)
	crb.TypeMeta = metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRoleBinding"}

	body, err := json.Marshal(crb)
	if err != nil {
		return fmt.Errorf("clusterrolebinding.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().RbacV1().ClusterRoleBindings().Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		if errors.IsInvalid(err) {
			// roleRef is immutable — delete and recreate.
			logger.Info().Str("clusterrolebinding", spec.Name).Msg("clusterrolebinding roleRef drifted — delete+recreate")
			if delErr := kube.Clientset().RbacV1().ClusterRoleBindings().Delete(ctx, spec.Name, metav1.DeleteOptions{}); delErr != nil && !errors.IsNotFound(delErr) {
				return fmt.Errorf("clusterrolebinding.Apply: deleting stale binding %q: %w", spec.Name, delErr)
			}
			return Create(ctx, kube, owner, spec)
		}
		return fmt.Errorf("clusterrolebinding.Apply: %w", err)
	}

	logger.Debug().
		Str("clusterrolebinding", spec.Name).
		Str("owner", owner.GetName()).
		Msg("clusterrolebinding applied")

	return nil
}

// Update applies the ClusterRoleBinding via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedClusterRoleBindingSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the ClusterRoleBinding if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedClusterRoleBindingSpec) error {
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().RbacV1().ClusterRoleBindings().Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("clusterrolebinding.Delete: deleting %q: %w", spec.Name, err)
	}

	logger.Info().
		Str("clusterrolebinding", spec.Name).
		Str("owner", owner.GetName()).
		Msg("clusterrolebinding deleted")

	return nil
}

// DeleteIfOwned deletes the ClusterRoleBinding only if it is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name string) error {

	existing, err := kube.Clientset().RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedClusterRoleBindingSpec from a ClusterRoleBindingTemplateSource.
// Template expressions must already be evaluated by template.Resolver before calling.
func Resolve(src orktypes.ClusterRoleBindingTemplateSource, ownerName string) ResolvedClusterRoleBindingSpec {
	spec := ResolvedClusterRoleBindingSpec{
		Name:          src.Name,
		Labels:        make(map[string]string),
		Sleep:         src.Sleep,
		ForceConflict: src.ForceConflict,
	}

	if spec.Name == "" {
		spec.Name = ownerName + "-crb"
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	kind := src.RoleRef.Kind
	if kind == "" {
		kind = "ClusterRole"
	}
	spec.RoleRef = rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     kind,
		Name:     src.RoleRef.Name,
	}

	for _, s := range src.Subjects {
		spec.Subjects = append(spec.Subjects, rbacv1.Subject{
			Kind:      s.Kind,
			Name:      s.Name,
			Namespace: s.Namespace,
		})
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildClusterRoleBinding(owner domain.Object, spec ResolvedClusterRoleBindingSpec) *rbacv1.ClusterRoleBinding {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   spec.Name,
			Labels: spec.Labels,
		},
		RoleRef:  spec.RoleRef,
		Subjects: spec.Subjects,
	}
	if owner.GetNamespace() == "" {
		crb.OwnerReferences = shared.ResolveOwnerReferences(owner)
	}
	return crb
}

func validateSpec(spec ResolvedClusterRoleBindingSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
