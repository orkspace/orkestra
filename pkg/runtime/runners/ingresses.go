// pkg/runners/ingresses.go
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/gateway/certmanager"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orkingress "github.com/orkspace/orkestra/pkg/resources/ingresses"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RunIngresses resolves and applies Ingress template declarations.
// When tls.enabled is true on an Ingress, a kubernetes.io/tls Secret is created
// before the Ingress so the Ingress can reference it immediately.
func RunIngresses(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.IngressTemplateSource,
	update bool,
	guard func(ctx context.Context, obj domain.Object, ns string) bool,
) error {
	activeNames := make(map[string]bool, len(srcs))
	for _, s := range srcs {
		if !orktypes.EvaluateConditions(resolver.Data(), s.Conditions, s.Or, resolver.TemplateEvaluator()) {
			continue
		}
		n, _ := resolver.Resolve(s.Name)
		nsp, _ := resolver.Resolve(s.Namespace)
		if nsp == "" {
			nsp = owner.GetNamespace()
		}
		activeNames[nsp+"/"+n] = true
	}

	for i, src := range srcs {
		conditionPassed := orktypes.EvaluateConditions(resolver.Data(), src.Conditions, src.Or, resolver.TemplateEvaluator())

		name, _ := resolver.Resolve(src.Name)
		ns, _ := resolver.Resolve(src.Namespace)
		if ns == "" {
			ns = owner.GetNamespace()
		}

		if guard != nil && !guard(ctx, owner, ns) {
			continue
		}

		if !conditionPassed {
			if update || src.Reconcile {
				if !activeNames[ns+"/"+name] {
					if err := orkingress.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("ingresses[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "Ingress").
				Int("index", i).
				Msg("conditions not met — skipping resource")
			continue
		}

		resolved, err := resolver.ResolveIngressTemplate(src)
		if err != nil {
			return fmt.Errorf("ingresses[%d]: %w", i, err)
		}

		spec := orkingress.Resolve(resolved, resolver.OwnerName())

		// Ensure TLS secret exists before applying the Ingress.
		if spec.TLS != nil && spec.TLS.Create {
			if err := ensureIngressTLSSecret(ctx, kube, owner, spec, ns); err != nil {
				return fmt.Errorf("ingresses[%d]: tls secret: %w", i, err)
			}
		}

		if update {
			if err := orkingress.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("ingresses[%d].update: %w", i, err)
			}
		} else {
			if err := orkingress.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("ingresses[%d].create: %w", i, err)
			}
			if src.Reconcile {
				if err := orkingress.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("ingresses[%d].reconcile: %w", i, err)
				}
			}
		}
	}
	return nil
}

// ensureIngressTLSSecret creates a kubernetes.io/tls Secret for the Ingress if
// it does not already exist. Idempotent — safe to call on every reconcile.
//
// CN  = first declared host (or Ingress name when Hosts is Empty().
// SANs = all declared hosts.
func ensureIngressTLSSecret(
	ctx context.Context,
	kube kubeclient.Interface,
	owner domain.Object,
	spec orkingress.ResolvedIngressSpec,
	namespace string,
) error {
	secretName := spec.TLS.SecretName
	if secretName == "" {
		secretName = spec.Name + "-tls"
	}

	// Idempotency check — skip if the Secret already exists.
	_, err := kube.Clientset().CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		logger.FromContext(ctx).Debug().
			Str("secret", secretName).
			Str("namespace", namespace).
			Msg("ingress tls secret already exists — skipping")
		return nil
	}
	if !IsNotFoundErr(err) {
		return fmt.Errorf("checking tls secret %q: %w", secretName, err)
	}

	// Determine CN and SANs from the TLS hosts list.
	hosts := spec.TLS.Hosts
	if len(hosts) == 0 && spec.Host != "" {
		hosts = []string{spec.Host}
	}

	cn := secretName
	if len(hosts) > 0 {
		cn = hosts[0]
	}

	bundle, err := certmanager.GenerateTLSBundle(cn, hosts, spec.TLS.ValidFor)
	if err != nil {
		return fmt.Errorf("generating tls bundle for %q: %w", secretName, err)
	}

	return createTLSSecret(ctx, kube, owner, secretName, namespace, "", bundle)
}
