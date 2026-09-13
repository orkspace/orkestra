// pkg/resources/ingresses/ingress.go
package ingresses

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
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// Create creates an Ingress owned by the CR if it does not already exist.
// Idempotent — if the Ingress exists, does nothing and returns nil.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedIngressSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("ingress.Create: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().NetworkingV1().Ingresses(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("ingress.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("ingress", spec.Name).
			Str("namespace", namespace).
			Msg("ingress already exists — skipping create")
		return nil
	}

	ing := buildIngress(owner, spec, namespace)

	_, err = kube.Clientset().NetworkingV1().Ingresses(namespace).Create(ctx, ing, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("ingress.Create: creating ingress %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("ingress", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("ingress created")

	return nil
}

// Apply creates or updates an Ingress using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedIngressSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("ingress.Apply: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	ing := buildIngress(owner, spec, namespace)
	ing.TypeMeta = metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"}

	body, err := json.Marshal(ing)
	if err != nil {
		return fmt.Errorf("ingress.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().NetworkingV1().Ingresses(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("ingress.Apply: %w", err)
	}

	logger.Debug().
		Str("ingress", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("ingress applied")

	return nil
}

// Update applies the Ingress via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedIngressSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the Ingress if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedIngressSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().NetworkingV1().Ingresses(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug().
				Str("ingress", spec.Name).
				Str("namespace", namespace).
				Msg("ingress already deleted — skipping")
			return nil
		}
		return fmt.Errorf("ingress.Delete: deleting ingress %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("ingress", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("ingress deleted")

	return nil
}

// DeleteIfOwned deletes the Ingress if it exists and is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().NetworkingV1().Ingresses(namespace).
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
	return kube.Clientset().NetworkingV1().Ingresses(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedIngressSpec from an IngressTemplateSource.
// All template expressions must be evaluated before calling here.
func Resolve(src orktypes.IngressTemplateSource, ownerName string) ResolvedIngressSpec {
	spec := ResolvedIngressSpec{
		Name:          src.Name,
		Namespace:     src.Namespace,
		Host:          src.Host,
		ServiceName:   src.ServiceName,
		Path:          src.Path,
		PathType:      src.PathType,
		IngressClass:  src.IngressClass,
		Labels:        make(map[string]string),
		Annotations:   make(map[string]string),
		Sleep:         src.Sleep,
		ForceConflict: src.ForceConflict,
	}

	if spec.Name == "" {
		spec.Name = ownerName + "-ingress"
	}
	if spec.Path == "" {
		spec.Path = "/"
	}
	if spec.PathType == "" {
		spec.PathType = "Prefix"
	}

	if src.ServicePort != "" {
		if p, err := strconv.ParseInt(src.ServicePort, 10, 32); err == nil {
			spec.ServicePort = int32(p)
		}
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}
	for k, v := range src.Annotations {
		spec.Annotations[k] = v
	}

	// System labels

	if src.TLS != nil && src.TLS.Create {
		secretName := src.TLS.SecretName
		if secretName == "" {
			secretName = ownerName + "-tls"
		}
		spec.TLS = &ResolvedIngressTLS{
			Create:     true,
			SecretName: secretName,
			Hosts:      src.TLS.Hosts,
			ValidFor:   src.TLS.ValidFor,
		}
	}

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildIngress(owner domain.Object, spec ResolvedIngressSpec, namespace string) *networkingv1.Ingress {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())

	pathType := networkingv1.PathTypePrefix
	switch spec.PathType {
	case "Exact":
		pathType = networkingv1.PathTypeExact
	case "ImplementationSpecific":
		pathType = networkingv1.PathTypeImplementationSpecific
	}

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			Annotations:     spec.Annotations,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: spec.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     spec.Path,
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: spec.ServiceName,
											Port: networkingv1.ServiceBackendPort{
												Number: spec.ServicePort,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if spec.IngressClass != "" {
		ing.Spec.IngressClassName = &spec.IngressClass
	}

	if spec.TLS != nil && spec.TLS.Create {
		hosts := spec.TLS.Hosts
		if len(hosts) == 0 && spec.Host != "" {
			hosts = []string{spec.Host}
		}
		ing.Spec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      hosts,
				SecretName: spec.TLS.SecretName,
			},
		}
	}

	return ing
}

func validateSpec(spec ResolvedIngressSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("missing required field: name")
	}
	return nil
}
