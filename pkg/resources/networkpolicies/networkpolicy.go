// pkg/resources/networkpolicies/networkpolicy.go
package networkpolicies

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/profiles"
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ResolvedNetworkPolicySpec is the fully resolved NetworkPolicy specification.
type ResolvedNetworkPolicySpec struct {
	Name              string
	Namespace         string
	PodSelector       map[string]string
	Ingress           []orktypes.NetworkPolicyIngressRule
	Egress            []orktypes.NetworkPolicyEgressRule
	PolicyTypes       []string
	FromNetworkPolicy string
	FromNamespace     string
	Labels            map[string]string
	Sleep             string

	// ForceConflict, when true, sets Force: true when applying this resource,
	// taking ownership of conflicting fields instead of returning a conflict error.
	// Overrides the CRD-level ForceConflict setting.
	ForceConflict *bool
}

// Create creates a NetworkPolicy if it does not already exist.
// Idempotent — skips if it already exists.
// Owner reference set for cascade deletion.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedNetworkPolicySpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().NetworkingV1().NetworkPolicies(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("networkpolicy.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("networkpolicy", spec.Name).
			Str("namespace", namespace).
			Msg("networkpolicy already exists — skipping create")
		return nil
	}

	np, err := buildNetworkPolicy(ctx, kube, owner, spec, namespace)
	if err != nil {
		return fmt.Errorf("networkpolicy.Create: building %q: %w", spec.Name, err)
	}

	_, err = kube.Clientset().NetworkingV1().NetworkPolicies(namespace).Create(ctx, np, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("networkpolicy.Create: creating %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("networkpolicy", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("networkpolicy created")

	return nil
}

// Apply creates or updates a NetworkPolicy using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedNetworkPolicySpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	np, err := buildNetworkPolicy(ctx, kube, owner, spec, namespace)
	if err != nil {
		return fmt.Errorf("networkpolicy.Apply: building %q: %w", spec.Name, err)
	}

	np.TypeMeta = metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"}

	body, err := json.Marshal(np)
	if err != nil {
		return fmt.Errorf("networkpolicy.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().NetworkingV1().NetworkPolicies(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("networkpolicy.Apply: %w", err)
	}

	logger.Debug().
		Str("networkpolicy", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("networkpolicy applied")

	return nil
}

// Update applies the NetworkPolicy via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedNetworkPolicySpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the NetworkPolicy if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedNetworkPolicySpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().NetworkingV1().NetworkPolicies(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("networkpolicy.Delete: deleting %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("networkpolicy", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("networkpolicy deleted")

	return nil
}

// DeleteIfOwned deletes the NetworkPolicy only if it is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().NetworkingV1().NetworkPolicies(namespace).
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
	return kube.Clientset().NetworkingV1().NetworkPolicies(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// CopyToNamespaces copies a NetworkPolicy to multiple target namespaces.
// Reads the source policy once and creates copies in each namespace.
// Idempotent — skips namespaces where the policy already exists.
func CopyToNamespaces(
	ctx context.Context,
	kube kubeclient.Interface,
	owner domain.Object,
	spec ResolvedNetworkPolicySpec,
	toNamespaces []string,
) error {
	sourceSpec, err := resolveSpec(ctx, kube, spec, owner)
	if err != nil {
		return fmt.Errorf("networkpolicy.CopyToNamespaces: reading source: %w", err)
	}

	for _, ns := range toNamespaces {
		if ns == "" {
			continue
		}

		_, err := kube.Clientset().NetworkingV1().NetworkPolicies(ns).Get(ctx, spec.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("networkpolicy.CopyToNamespaces: checking %q in %q: %w", spec.Name, ns, err)
		}
		if err == nil {
			logger.Debug().
				Str("networkpolicy", spec.Name).
				Str("namespace", ns).
				Msg("networkpolicy already exists in namespace — skipping")
			continue
		}

		np := buildNetworkPolicyFromSpec(owner, spec, ns, sourceSpec)
		_, err = kube.Clientset().NetworkingV1().NetworkPolicies(ns).Create(ctx, np, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("networkpolicy.CopyToNamespaces: creating %q in %q: %w", spec.Name, ns, err)
		}

		logger.Info().
			Str("networkpolicy", spec.Name).
			Str("namespace", ns).
			Str("owner", owner.GetName()).
			Msg("networkpolicy copied to namespace")
	}

	return nil
}

// Resolve builds a ResolvedNetworkPolicySpec from a NetworkPolicyTemplateSource.
// Template expressions must already be evaluated by template.Resolver before calling.
func Resolve(src orktypes.NetworkPolicyTemplateSource, ownerName string, reg orktypes.ProfileRegistry) ResolvedNetworkPolicySpec {
	ingress := src.Ingress
	egress := src.Egress
	policyTypes := src.PolicyTypes

	if src.Profile != "" {
		if expanded, err := profiles.ApplyNetworkPolicyProfile(src.Profile, reg); err != nil {
			logger.Warn().Str("profile", src.Profile).Err(err).Msg("unknown networkpolicy profile — skipping")
		} else {
			ingress = expanded.Ingress
			egress = expanded.Egress
			policyTypes = expanded.PolicyTypes
		}
	}

	spec := ResolvedNetworkPolicySpec{
		Name:              src.Name,
		Namespace:         src.Namespace,
		PodSelector:       src.PodSelector,
		Ingress:           ingress,
		Egress:            egress,
		PolicyTypes:       policyTypes,
		FromNetworkPolicy: src.FromNetworkPolicy,
		FromNamespace:     src.FromNamespace,
		Labels:            make(map[string]string),
		Sleep:             src.Sleep,
		ForceConflict:     src.ForceConflict,
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// resolveSpec returns the k8s NetworkPolicySpec to apply.
// When FromNetworkPolicy is set, copies the source policy's spec.
// Otherwise builds spec from the declared fields.
func resolveSpec(
	ctx context.Context,
	kube kubeclient.Interface,
	spec ResolvedNetworkPolicySpec,
	owner domain.Object,
) (networkingv1.NetworkPolicySpec, error) {
	if spec.FromNetworkPolicy != "" {
		fromNS := spec.FromNamespace
		if fromNS == "" {
			fromNS = owner.GetNamespace()
		}
		if fromNS == "" {
			fromNS = "default"
		}

		source, err := kube.Clientset().NetworkingV1().NetworkPolicies(fromNS).
			Get(ctx, spec.FromNetworkPolicy, metav1.GetOptions{})
		if err != nil {
			return networkingv1.NetworkPolicySpec{}, fmt.Errorf(
				"reading source networkpolicy %q from %q: %w", spec.FromNetworkPolicy, fromNS, err)
		}
		return source.Spec, nil
	}

	return buildNetworkPolicySpec(spec), nil
}

func buildNetworkPolicy(
	ctx context.Context,
	kube kubeclient.Interface,
	owner domain.Object,
	spec ResolvedNetworkPolicySpec,
	namespace string,
) (*networkingv1.NetworkPolicy, error) {
	npSpec, err := resolveSpec(ctx, kube, spec, owner)
	if err != nil {
		return nil, err
	}
	return buildNetworkPolicyFromSpec(owner, spec, namespace, npSpec), nil
}

// specsEqual compares two NetworkPolicySpecs using JSON marshaling so that nil
// and empty maps/slices are treated as equal (both serialize to absent with omitempty).
func specsEqual(a, b networkingv1.NetworkPolicySpec) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aj) == string(bj)
}

func buildNetworkPolicyFromSpec(
	owner domain.Object,
	spec ResolvedNetworkPolicySpec,
	namespace string,
	npSpec networkingv1.NetworkPolicySpec,
) *networkingv1.NetworkPolicy {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: npSpec,
	}
}

// buildNetworkPolicySpec translates our declaration types to k8s NetworkPolicySpec.
func buildNetworkPolicySpec(spec ResolvedNetworkPolicySpec) networkingv1.NetworkPolicySpec {
	npSpec := networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: spec.PodSelector,
		},
	}

	// Ingress rules
	for _, r := range spec.Ingress {
		rule := networkingv1.NetworkPolicyIngressRule{}
		for _, peer := range r.From {
			rule.From = append(rule.From, translatePeer(peer))
		}
		for _, p := range r.Ports {
			rule.Ports = append(rule.Ports, translatePort(p))
		}
		npSpec.Ingress = append(npSpec.Ingress, rule)
	}

	// Egress rules
	for _, r := range spec.Egress {
		rule := networkingv1.NetworkPolicyEgressRule{}
		for _, peer := range r.To {
			rule.To = append(rule.To, translatePeer(peer))
		}
		for _, p := range r.Ports {
			rule.Ports = append(rule.Ports, translatePort(p))
		}
		npSpec.Egress = append(npSpec.Egress, rule)
	}

	// PolicyTypes — explicit or auto-derived
	if len(spec.PolicyTypes) > 0 {
		for _, pt := range spec.PolicyTypes {
			npSpec.PolicyTypes = append(npSpec.PolicyTypes, networkingv1.PolicyType(pt))
		}
	} else {
		if spec.Ingress != nil {
			npSpec.PolicyTypes = append(npSpec.PolicyTypes, networkingv1.PolicyTypeIngress)
		}
		if spec.Egress != nil {
			npSpec.PolicyTypes = append(npSpec.PolicyTypes, networkingv1.PolicyTypeEgress)
		}
	}

	return npSpec
}

func translatePeer(peer orktypes.NetworkPolicyPeer) networkingv1.NetworkPolicyPeer {
	p := networkingv1.NetworkPolicyPeer{}

	hasNS := len(peer.NamespaceSelector) > 0
	hasIP := peer.IPBlock != nil

	if peer.PodSelector != nil {
		p.PodSelector = &metav1.LabelSelector{MatchLabels: peer.PodSelector}
	} else if !hasNS && !hasIP {
		// podSelector: {} was declared but its empty map was dropped by omitempty
		// during the bundle serialization round-trip. An all-nil peer is never valid
		// in Kubernetes, so the only sensible interpretation is "select all pods in
		// the namespace" — which is exactly what an empty LabelSelector means.
		p.PodSelector = &metav1.LabelSelector{}
	}
	if hasNS {
		p.NamespaceSelector = &metav1.LabelSelector{MatchLabels: peer.NamespaceSelector}
	}
	if hasIP {
		p.IPBlock = &networkingv1.IPBlock{
			CIDR:   peer.IPBlock.CIDR,
			Except: peer.IPBlock.Except,
		}
	}
	return p
}

func translatePort(p orktypes.NetworkPolicyPort) networkingv1.NetworkPolicyPort {
	np := networkingv1.NetworkPolicyPort{}
	if p.Protocol != "" {
		proto := corev1.Protocol(p.Protocol)
		np.Protocol = &proto
	}
	if p.Port != "" {
		// Kubernetes requires string ports to be named ports (containing at least one letter).
		// Numeric port values must be sent as integers.
		var port intstr.IntOrString
		if n, err := strconv.Atoi(p.Port); err == nil {
			port = intstr.FromInt32(int32(n))
		} else {
			port = intstr.FromString(p.Port)
		}
		np.Port = &port
	}
	return np
}
