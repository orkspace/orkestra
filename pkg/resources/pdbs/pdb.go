// pkg/resources/pdbs/pdb.go
package pdbs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/profiles"
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Create creates a PDB owned by the CR if it does not already exist.
// Idempotent — if the PDB exists, does nothing and returns nil.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedPDBSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("pdb.Create: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().PolicyV1().PodDisruptionBudgets(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("pdb.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("pdb", spec.Name).
			Str("namespace", namespace).
			Msg("pdb already exists — skipping create")
		return nil
	}

	pdb := buildPDB(owner, spec, namespace)

	_, err = kube.Clientset().PolicyV1().PodDisruptionBudgets(namespace).Create(ctx, pdb, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("pdb.Create: creating pdb %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("pdb", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("pdb created")

	return nil
}

// Apply creates or updates a PodDisruptionBudget using Server-Side Apply.
// PDB selectors are immutable — if SSA is rejected, falls back to delete + recreate.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedPDBSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("pdb.Apply: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	pdb := buildPDB(owner, spec, namespace)
	pdb.TypeMeta = metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"}

	body, err := json.Marshal(pdb)
	if err != nil {
		return fmt.Errorf("pdb.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().PolicyV1().PodDisruptionBudgets(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		if errors.IsInvalid(err) {
			logger.Info().Str("pdb", spec.Name).Msg("pdb selector immutable — delete+recreate")
			if delErr := kube.Clientset().PolicyV1().PodDisruptionBudgets(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{}); delErr != nil && !errors.IsNotFound(delErr) {
				return fmt.Errorf("pdb.Apply: deleting stale pdb %q: %w", spec.Name, delErr)
			}
			return Create(ctx, kube, owner, spec)
		}
		return fmt.Errorf("pdb.Apply: %w", err)
	}

	logger.Debug().
		Str("pdb", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("pdb applied")

	return nil
}

// Update applies the PDB via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedPDBSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the PDB if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedPDBSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().PolicyV1().PodDisruptionBudgets(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug().
				Str("pdb", spec.Name).
				Str("namespace", namespace).
				Msg("pdb already deleted — skipping")
			return nil
		}
		return fmt.Errorf("pdb.Delete: deleting pdb %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("pdb", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("pdb deleted")

	return nil
}

// DeleteIfOwned deletes the PDB if it exists and is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().PolicyV1().PodDisruptionBudgets(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().PolicyV1().PodDisruptionBudgets(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedPDBSpec from a PDBTemplateSource.
// All template expressions must be evaluated before calling here.
func Resolve(src orktypes.PDBTemplateSource, ownerName string, reg orktypes.ProfileRegistry) ResolvedPDBSpec {
	spec := ResolvedPDBSpec{
		Name:           src.Name,
		Namespace:      src.Namespace,
		MinAvailable:   src.MinAvailable,
		MaxUnavailable: src.MaxUnavailable,
		Selector:       make(map[string]string),
		Labels:         make(map[string]string),
		Sleep:          src.Sleep,
		ForceConflict:  src.ForceConflict,
	}

	if spec.Name == "" {
		spec.Name = ownerName + "-pdb"
	}

	if src.Behavior != nil && src.Behavior.Profile != "" {
		expansion, err := profiles.ApplyPDBProfile(src.Behavior.Profile, reg)
		if err != nil {
			logger.Warn().Str("profile", src.Behavior.Profile).Err(err).Msg("unknown pdb behavior profile — skipping")
		} else {
			if spec.MinAvailable == "" {
				spec.MinAvailable = expansion.MinAvailable
			}
			if spec.MaxUnavailable == "" {
				spec.MaxUnavailable = expansion.MaxUnavailable
			}
		}
	}

	for k, v := range src.Selector {
		spec.Selector[k] = v
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	// System labels

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildPDB(owner domain.Object, spec ResolvedPDBSpec, namespace string) *policyv1.PodDisruptionBudget {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())

	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: spec.Selector,
			},
		},
	}

	if spec.MinAvailable != "" {
		v := parseIntOrString(spec.MinAvailable)
		pdb.Spec.MinAvailable = &v
	} else if spec.MaxUnavailable != "" {
		v := parseIntOrString(spec.MaxUnavailable)
		pdb.Spec.MaxUnavailable = &v
	}

	return pdb
}

// parseIntOrString converts a string to intstr.IntOrString.
// Strings ending in "%" are treated as string values; others as integers.
func parseIntOrString(s string) intstr.IntOrString {
	if strings.HasSuffix(s, "%") {
		return intstr.FromString(s)
	}
	if n, err := strconv.Atoi(s); err == nil {
		return intstr.FromInt32(int32(n))
	}
	return intstr.FromString(s)
}

func validateSpec(spec ResolvedPDBSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("missing required field: name")
	}
	return nil
}
