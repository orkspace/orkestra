// pkg/resources/services/service.go
package services

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
	"github.com/orkspace/orkestra/pkg/resources/shared"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Create creates a Service owned by the CR if it does not already exist.
// Idempotent — if the Service exists, does nothing and returns nil.
// Owner reference is set so the Service is garbage collected when the CR is deleted.
func Create(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedServiceSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("service.Create: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	_, err := kube.Clientset().CoreV1().Services(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("service.Create: checking existence of %q: %w", spec.Name, err)
	}
	if err == nil {
		logger.Debug().
			Str("service", spec.Name).
			Str("namespace", namespace).
			Msg("service already exists — skipping create")
		return nil
	}

	svc := buildService(owner, spec, namespace)

	_, err = kube.Clientset().CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("service.Create: creating service %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("service", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("service created")

	return nil
}

// Apply creates or updates a Service using Server-Side Apply.
// Sends only the fields Orkestra owns; k8s-injected defaults are invisible.
func Apply(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedServiceSpec) error {
	if err := validateSpec(spec); err != nil {
		return fmt.Errorf("service.Apply: invalid spec: %w", err)
	}

	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	svc := buildService(owner, spec, namespace)
	svc.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}

	body, err := json.Marshal(svc)
	if err != nil {
		return fmt.Errorf("service.Apply: marshal: %w", err)
	}

	if _, err = kube.Clientset().CoreV1().Services(namespace).Patch(
		ctx, spec.Name, k8stypes.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: konfig.FieldManagerRuntime, Force: shared.ResolveForceConflict(kube, spec.ForceConflict)},
	); err != nil {
		return fmt.Errorf("service.Apply: %w", err)
	}

	logger.Debug().
		Str("service", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("service applied")

	return nil
}

// Update applies the Service via SSA. Delegates to Apply.
func Update(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedServiceSpec) error {
	return Apply(ctx, kube, owner, spec)
}

// Delete deletes the Service if it exists.
func Delete(ctx context.Context, kube kubeclient.Interface, owner domain.Object, spec ResolvedServiceSpec) error {
	namespace := shared.ResolveNamespace(owner, spec.Namespace)
	if err := shared.SleepIfNeeded(spec.Sleep); err != nil {
		return err
	}

	err := kube.Clientset().CoreV1().Services(namespace).Delete(ctx, spec.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug().
				Str("service", spec.Name).
				Str("namespace", namespace).
				Msg("service already deleted — skipping")
			return nil
		}
		return fmt.Errorf("service.Delete: deleting service %q in %q: %w", spec.Name, namespace, err)
	}

	logger.Info().
		Str("service", spec.Name).
		Str("namespace", namespace).
		Str("owner", owner.GetName()).
		Msg("service deleted")

	return nil
}

// DeleteIfOwned deletes the Service if it exists and is owned by the CR.
func DeleteIfOwned(ctx context.Context, kube kubeclient.Interface,
	owner domain.Object, name, namespace string) error {

	existing, err := kube.Clientset().CoreV1().Services(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	// Only delete if we own it
	if existing.Labels[labels.OrkestraOwner] != labels.EffectiveOwnerKey(owner.GetName(), owner.GetAnnotations()) {
		return nil
	}
	return kube.Clientset().CoreV1().Services(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
}

// Resolve builds a ResolvedServiceSpec from a ServiceTemplateSource.
// All fields already resolved by template.Resolver before calling here.
// This function assembles the spec and applies defaults.
func Resolve(src orktypes.ServiceTemplateSource, ownerName string) ResolvedServiceSpec {
	spec := ResolvedServiceSpec{
		Name:          src.Name,
		Labels:        make(map[string]string),
		Selector:      make(map[string]string),
		Sleep:         src.Sleep,
		ForceConflict: src.ForceConflict,
	}

	if spec.Name == "" {
		spec.Name = ownerName + "-svc"
	}

	spec.Namespace = src.Namespace
	spec.Sleep = src.Sleep

	spec.Type = src.Type
	if spec.Type == "" {
		spec.Type = "ClusterIP"
	}

	if src.Port != "" {
		if p, err := strconv.ParseInt(src.Port, 10, 32); err == nil {
			spec.Port = int32(p)
		}
	}

	if src.TargetPort != "" {
		if p, err := strconv.ParseInt(src.TargetPort, 10, 32); err == nil {
			spec.TargetPort = int32(p)
		}
	}

	// If TargetPort not set, default to Port
	if spec.TargetPort == 0 {
		spec.TargetPort = spec.Port
	}

	for k, v := range src.Labels {
		spec.Labels[k] = v
	}

	for k, v := range src.Selector {
		spec.Selector[k] = v
	}

	// System labels

	return spec
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func buildService(owner domain.Object, spec ResolvedServiceSpec, namespace string) *corev1.Service {
	spec.Labels = labels.StampOrkestraLabels(spec.Labels, owner.GetName(), owner.GetAnnotations())
	svcType := corev1.ServiceTypeClusterIP
	switch spec.Type {
	case "NodePort":
		svcType = corev1.ServiceTypeNodePort
	case "LoadBalancer":
		svcType = corev1.ServiceTypeLoadBalancer
	}

	if spec.Headless {
		svcType = corev1.ClusterIPNone
	}

	proto := corev1.ProtocolTCP
	switch strings.ToUpper(spec.Protocol) {
	case "UDP":
		proto = corev1.ProtocolUDP
	case "SCTP":
		proto = corev1.ProtocolSCTP
	case "", "TCP":
		proto = corev1.ProtocolTCP
	}

	// Selector matches pods created by deployments with the same owner
	spec.Selector["orkestra-owner"] = owner.GetName()

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            spec.Name,
			Namespace:       namespace,
			Labels:          spec.Labels,
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: spec.Selector,
			Ports: []corev1.ServicePort{
				{
					Port:       spec.Port,
					TargetPort: intstr.FromInt(int(spec.TargetPort)),
					Protocol:   proto,
				},
			},
		},
	}
}

func validateSpec(spec ResolvedServiceSpec) error {
	var missing []string
	if spec.Name == "" {
		missing = append(missing, "name")
	}
	if spec.Port == 0 {
		missing = append(missing, "port")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %v", missing)
	}
	return nil
}
