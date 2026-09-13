// pkg/resources/roles/role.go
package roles

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

// ResolvedRoleSpec is the fully resolved Role specification.
type ResolvedRoleSpec struct {
	Name      string
	Namespace string
	Labels    map[string]string
	Rules     []rbacv1.PolicyRule

	// Sleep injects an artificial delay into the reconcile of this resource.
	// Useful for autoscale testing, latency simulation, and chaos engineering.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}

// Create creates a Role if it does not already exist.
// Idempotent — skips if the Role already exists.
// Owner reference ensures cleanup when the CR is deleted.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedRoleSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("role.Create: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().RbacV1().Roles(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("role.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("role", spec.Name).
			Str("namespace", namespace).
			Msg("role already exists — skipping create")
		return nil
	}

	role := buildRole(owner, spec, namespace)

	_, err = kube.Clientset().RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("role.Create: creating role %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("role", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("role created")

	return nil
}

// Apply creates or updates a Role using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedRoleSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	role := buildRole(owner, spec, namespace)
	role.TypeMeta = metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "Role"}

	body, err := json.Marshal(role)
	if err != nil {
		return fmt.Errorf("role.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().RbacV1().Roles(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("role.Apply: %w", err)
	}

	logger.Debug().
		Str("role", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("role applied")

	return nil
}

// Update applies the Role via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedRoleSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the Role if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedRoleSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().RbacV1().Roles(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug().
				Str("role", spec.Name).
				Str("namespace", namespace).
				Msg("role already deleted — skipping")
			return nil
		}
		return fmt.Errorf("role.Delete: deleting role %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("role", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("role deleted")

	return nil
}

// DeleteIfOwned deletes the Role only if it is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().RbacV1().Roles(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().RbacV1().Roles(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedRoleSpec from a RoleTemplateSource.
// Template expressions must already be evaluated by template.Resolver before calling.
func Resolve(src orktypes.RoleTemplateSource, ownerName string) ResolvedRoleSpec {
	spec := ResolvedRoleSpec{
		Name:          src.Name,
		Namespace:     src.Namespace,
		Labels:        make(map[string]string),
		Sleep:         src.Sleep,
		ForceConflict: src.ForceConflict,
	}

	if spec.Name == "" {
		spec.Name = ownerName + "-role"
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

func buildRole(owner domain.Object, spec ResolvedRoleSpec, namespace string) *rbacv1.Role {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Rules: spec.Rules,
	}
}

func validateSpec(spec ResolvedRoleSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
