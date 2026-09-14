// pkg/resources/clusterroles/clusterrole.go
package clusterroles

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

// ResolvedClusterRoleSpec is the fully resolved ClusterRole specification.
type ResolvedClusterRoleSpec struct {
	Name   string
	Labels map[string]string
	Rules  []rbacv1.PolicyRule
	Sleep  string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}

// Create creates a ClusterRole if it does not already exist.
// Idempotent — skips if the ClusterRole already exists.
// ClusterRoles are cluster-scoped; ownership is tracked via the orkestra.io/owner label.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedClusterRoleSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("clusterrole.Create: invalid spec: %w", err)
	}
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().RbacV1().ClusterRoles().Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("clusterrole.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("clusterrole", spec.Name).
			Msg("clusterrole already exists — skipping create")
		return nil
	}

	cr := buildClusterRole(owner, spec)

	_, err = kube.Clientset().RbacV1().ClusterRoles().Create(ctx, cr, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("clusterrole.Create: creating %q: %w", spec.Name, err)
	}

	logger.Info().
		Str("clusterrole", spec.Name).
		Str("owner", owner.GetName()).
		Msg("clusterrole created")

	return nil
}

// Apply creates or updates a ClusterRole using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedClusterRoleSpec) error {
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	cr := buildClusterRole(owner, spec)
	cr.TypeMeta = metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole"}

	body, err := json.Marshal(cr)
	if err != nil {
		return fmt.Errorf("clusterrole.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().RbacV1().ClusterRoles().Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("clusterrole.Apply: %w", err)
	}

	logger.Debug().
		Str("clusterrole", spec.Name).
		Str("owner", owner.GetName()).
		Msg("clusterrole applied")

	return nil
}

// Update applies the ClusterRole via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedClusterRoleSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the ClusterRole if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedClusterRoleSpec) error {
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().RbacV1().ClusterRoles().Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("clusterrole.Delete: deleting %q: %w", spec.Name, err)
	}

	logger.Info().
		Str("clusterrole", spec.Name).
		Str("owner", owner.GetName()).
		Msg("clusterrole deleted")

	return nil
}

// DeleteIfOwned deletes the ClusterRole only if it is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name string) error {

	existing, err := kube.Clientset().RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedClusterRoleSpec from a ClusterRoleTemplateSource.
// Template expressions must already be evaluated by template.Resolver before calling.
func Resolve(src orktypes.ClusterRoleTemplateSource, ownerName string) ResolvedClusterRoleSpec {
	spec := ResolvedClusterRoleSpec{
		Name:          src.Name,
		Labels:        make(map[string]string),
		Sleep:         src.Sleep,
		ForceConflict: src.ForceConflict,
	}

	if spec.Name == "" {
		spec.Name = ownerName + "-cluster-role"
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	for _, r := range src.Rules {
		spec.Rules = append(spec.Rules, rbacv1.PolicyRule{
			APIGroups:     r.APIGroups,
			Resources:     r.Resources,
			Verbs:         r.Verbs,
			ResourceNames: r.ResourceNames,
		})
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildClusterRole(owner domain.Object, spec ResolvedClusterRoleSpec) *rbacv1.ClusterRole {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   spec.Name,
			Labels: spec.Labels,
		},
		Rules: spec.Rules,
	}
	if owner.GetNamespace() == "" {
		cr.OwnerReferences = shared.ResolveOwnerReferences(owner)
	}
	return cr
}

func validateSpec(spec ResolvedClusterRoleSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
