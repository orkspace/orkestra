// pkg/runners/secret_tls.go
//
// TLS certificate generation and secret rotation for Orkestra secrets.
//
// Extends the once: true pattern with:
//   - rotateAfter: <duration> — time-based rotation using a generated-at annotation
//   - tls: {...}              — self-signed CA + signed certificate generation
//
// The generated Secret is annotated with:
//
//	orkestra.orkspace.io/generated-at: "2026-04-06T08:00:00Z"
//	orkestra.orkspace.io/rotate-after: "90d"
//
// On each reconcile, secretNeedsRotation reads these annotations and returns
// true when the threshold is crossed. The caller deletes and recreates.
//
// TLS generation produces a Secret of type kubernetes.io/tls with:
//
//	tls.crt — PEM-encoded signed certificate
//	tls.key — PEM-encoded private key (for the server)
//	ca.crt  — PEM-encoded self-signed CA certificate
//
// The CA is unique per Secret — appropriate for self-signed operator webhooks.
// For shared CAs across multiple services, store the CA in a separate Secret
// and reference it (planned: ca.secretRef field).
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/gateway/certmanager"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/resources/shared"
	"github.com/orkspace/orkestra/pkg/secrets"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretNeedsRotation delegates to pkg/secrets.
func SecretNeedsRotation(ctx context.Context, kube kubeclient.Interface, namespace, name, rotateAfter string) (bool, error) {
	return secrets.SecretNeedsRotation(ctx, kube, namespace, name, rotateAfter)
}

// DeleteSecretForRotation delegates to pkg/secrets.
func DeleteSecretForRotation(ctx context.Context, kube kubeclient.Interface, namespace, name string) error {
	return secrets.DeleteSecretForRotation(ctx, kube, namespace, name)
}

// GenerationAnnotations delegates to pkg/secrets.
func GenerationAnnotations(rotateAfter string) map[string]string {
	return secrets.GenerationAnnotations(rotateAfter)
}

// createTLSSecret creates a kubernetes.io/tls Secret from a TLSBundle.
// The Secret name defaults to "owner.GetName()-orkestra-tls" when src.Name resolves to "".
func createTLSSecret(
	ctx context.Context,
	kube kubeclient.Interface,
	owner domain.Object,
	name, namespace, rotateAfter string,
	bundle *certmanager.TLSBundle,
) error {
	if name == "" {
		name = owner.GetName() + "-orkestra-tls"
	}

	annotations := GenerationAnnotations(rotateAfter)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
			Labels: map[string]string{
				"orkestra-owner":               owner.GetName(),
				"app.kubernetes.io/managed-by": "orkestra",
			},
			OwnerReferences: shared.ResolveOwnerReferences(owner),
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": bundle.CertPEM,
			"tls.key": bundle.KeyPEM,
			"ca.crt":  bundle.CACertPEM,
		},
	}

	_, err := kube.Clientset().CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating TLS secret %s/%s: %w", namespace, name, err)
	}

	logger.FromContext(ctx).Info().
		Str("secret", name).
		Str("namespace", namespace).
		Str("commonName", "").
		Msg("TLS secret created")

	return nil
}
