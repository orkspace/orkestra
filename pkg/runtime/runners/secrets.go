// pkg/runners/secrets.go
//
// Adds to the previous version:
//   - orktypes.EvaluateConditions instead of evaluateConditions (fixes or: being ignored)
//   - rotateAfter: <duration> support — time-based credential rotation
//   - tls: {...} support — self-signed CA + signed certificate generation
//
// Execution order per secret declaration:
//
//  1. EvaluateConditions(when:, or:)          — skip if conditions fail
//  2. Once/rotation check
//     a. once: true, rotateAfter set       — check annotation, delete if expired
//     b. once: true, no rotateAfter        — skip if exists
//     c. once: false                       — standard create/update
//  3. Resolve template expressions         — randomAlphanumeric evaluated here
//  4. TLS generation (if tls: is set)      — generate CA + cert, skip data resolution
//  5. Create/Update/CopyToNamespaces
//  6. Annotate with generated-at           — only when rotateAfter is set
package runners

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/gateway/certmanager"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orksecrets "github.com/orkspace/orkestra/pkg/resources/secrets"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

func RunSecrets(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	srcs []orktypes.SecretTemplateSource,
	update bool,
	guard func(ctx context.Context, obj domain.Object, ns string) bool,
) error {
	activeNames := make(map[string]bool, len(srcs))
	for _, s := range srcs {
		if !orktypes.EvaluateConditions(resolver.Data(), s.Conditions, s.Or, resolver.TemplateEvaluator()) {
			continue
		}
		n, _ := resolver.Resolve(s.Name)
		if n == "" && s.TLS != nil {
			n = owner.GetName() + "-" + konfig.DefaultWorkloadSecretName()
		}
		nsp, _ := resolver.Resolve(s.Namespace)
		if nsp == "" {
			nsp = owner.GetNamespace()
		}
		activeNames[nsp+"/"+n] = true
	}

	for i, src := range srcs {
		// ── Step 1: condition evaluation ────────────────────────────────────────
		// EvaluateConditions checks both when: (AND) and or: (OR).
		// IMPORTANT: must use resolver.Data() not the owner object directly.
		// resolver.Data() includes .children.*, .external.*, .cross.* — the owner
		// object alone does not have these injected fields.
		conditionPassed := orktypes.EvaluateConditions(resolver.Data(), src.Conditions, src.Or, resolver.TemplateEvaluator())

		// Resolve name and namespace early — needed for guard check, once: checks,
		// and DeleteIfOwned cleanup. ResolveSecretTemplate resolves these again
		// internally — intentional, cheap.
		name, _ := resolver.Resolve(src.Name)
		if name == "" && src.TLS != nil {
			// TLS secrets default to "orkestra-tls" when no name declared
			name = owner.GetName() + "-" + konfig.DefaultWorkloadSecretName()
		}
		ns, _ := resolver.Resolve(src.Namespace)
		if ns == "" {
			ns = owner.GetNamespace()
		}

		// ── Namespace guard ───────────────────────────────────────────────────
		if guard != nil && !guard(ctx, owner, ns) {
			continue // skipped — CheckNamespace already logged the reason
		}

		if !conditionPassed {
			if update || src.Reconcile {
				if !activeNames[ns+"/"+name] {
					if err := orksecrets.DeleteIfOwned(ctx, kube, owner, name, ns); err != nil {
						return fmt.Errorf("secrets[%d]: conditional cleanup: %w", i, err)
					}
				}
			}
			logger.FromContext(ctx).Debug().
				Str("resource", "Secret").
				Int("index", i).
				Msg("conditions not met — skipping secret")
			continue
		}

		// ── Step 2: once: / rotateAfter: logic ──────────────────────────────────
		if src.Once && !update && !src.Reconcile {
			if src.RotateAfter != "" {
				// Rotation mode: check if the Secret has exceeded its threshold
				needsRotation, err := SecretNeedsRotation(ctx, kube, ns, name, src.RotateAfter)
				if err != nil {
					return fmt.Errorf("secrets[%d]: rotation check: %w", i, err)
				}
				if needsRotation {
					logger.FromContext(ctx).Info().
						Str("secret", name).
						Str("rotateAfter", src.RotateAfter).
						Msg("secret rotation threshold exceeded — regenerating")
					if err := DeleteSecretForRotation(ctx, kube, ns, name); err != nil {
						return fmt.Errorf("secrets[%d]: rotation delete: %w", i, err)
					}
					// Fall through to create with fresh values
				} else {
					// Check plain existence (Secret may not exist yet)
					exists, err := SecretExists(ctx, kube, ns, name)
					if err != nil {
						return fmt.Errorf("secrets[%d]: existence check: %w", i, err)
					}
					if exists {
						logger.FromContext(ctx).Debug().
							Str("secret", name).
							Msg("once: secret exists and rotation not due — skipping")
						continue
					}
					// Does not exist — fall through to create
				}
			} else {
				// Plain once: no rotation — skip if exists
				if src.Reconcile {
					logOnceReconcileConflict(ctx, name)
				} else {
					exists, err := SecretExists(ctx, kube, ns, name)
					if err != nil {
						return fmt.Errorf("secrets[%d]: once: existence check: %w", i, err)
					}
					if exists {
						logger.FromContext(ctx).Debug().
							Str("secret", name).
							Msg("once: secret already exists — skipping (values preserved)")
						continue
					}
				}
			}
		}

		// ── Step 3 + 4: TLS generation path ─────────────────────────────────────
		// When tls: is declared, ignore the data: block entirely and generate
		// a self-signed CA + signed certificate instead.
		if src.TLS != nil {
			if err := RunTLSSecret(ctx, kube, resolver, owner, src, name, ns, update); err != nil {
				return fmt.Errorf("secrets[%d].tls: %w", i, err)
			}
			continue
		}

		// ── Step 3: resolve template expressions ────────────────────────────────
		// randomAlphanumeric, randomHex, randomBase64 are evaluated here.
		// The once: check above ensures this only runs on first creation or after rotation.
		resolved, err := resolver.ResolveSecretTemplate(src)
		if err != nil {
			return fmt.Errorf("secrets[%d]: %w", i, err)
		}
		// Apply resolved name/ns (may have been resolved before ResolveSecretTemplate)
		if resolved.Name == "" {
			resolved.Name = name
		}
		if resolved.Namespace == "" {
			resolved.Namespace = ns
		}

		// ── Step 5: apply ────────────────────────────────────────────────────────
		spec := orksecrets.Resolve(resolved, resolver.OwnerName())

		// Attach rotation annotation when rotateAfter is declared
		if src.RotateAfter != "" && spec.Annotations == nil {
			spec.Annotations = GenerationAnnotations(src.RotateAfter)
		} else if src.RotateAfter != "" {
			for k, v := range GenerationAnnotations(src.RotateAfter) {
				spec.Annotations[k] = v
			}
		}

		if len(resolved.ToNamespaces) > 0 {
			shouldSync := update || src.Reconcile
			for _, targetNs := range resolved.ToNamespaces {
				// Guard per target namespace — skip restricted, continue to allowed
				if guard != nil && !guard(ctx, owner, targetNs) {
					continue
				}
				nsSpec := spec
				nsSpec.Namespace = targetNs
				if shouldSync {
					if err := orksecrets.Update(ctx, kube, owner, nsSpec); err != nil {
						return fmt.Errorf("secrets[%d].sync namespace=%s: %w", i, targetNs, err)
					}
				} else {
					if err := orksecrets.Create(ctx, kube, owner, nsSpec); err != nil {
						return fmt.Errorf("secrets[%d].create namespace=%s: %w", i, targetNs, err)
					}
				}
			}
			continue
		}

		if update {
			if err := orksecrets.Update(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("secrets[%d].update: %w", i, err)
			}
		} else {
			if err := orksecrets.Create(ctx, kube, owner, spec); err != nil {
				return fmt.Errorf("secrets[%d].create: %w", i, err)
			}
			if src.Reconcile {
				if err := orksecrets.Update(ctx, kube, owner, spec); err != nil {
					return fmt.Errorf("secrets[%d].reconcile: %w", i, err)
				}
			}
		}
	}
	return nil
}

// RunTLSSecret handles the tls: path — generates a self-signed CA and server
// certificate and creates/updates a kubernetes.io/tls Secret.
func RunTLSSecret(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	owner domain.Object,
	src orktypes.SecretTemplateSource,
	name, namespace string,
	update bool,
) error {
	// Resolve DNS names — template expressions supported per entry
	dnsNames := make([]string, 0, len(src.TLS.DNSNames))
	for _, raw := range src.TLS.DNSNames {
		resolved, err := resolver.Resolve(raw)
		if err != nil {
			return fmt.Errorf("resolving dnsName %q: %w", raw, err)
		}
		if resolved != "" {
			dnsNames = append(dnsNames, resolved)
		}
	}

	commonName, err := resolver.Resolve(src.TLS.CommonName)
	if err != nil {
		return fmt.Errorf("resolving commonName: %w", err)
	}
	if commonName == "" {
		// Default: <name>.<namespace>.svc
		commonName = name + "." + namespace + ".svc"
	}

	validFor := src.TLS.ValidFor
	if validFor == "" {
		validFor = src.RotateAfter // use rotation period as validity duration
	}
	if validFor == "" {
		validFor = "1y"
	}

	bundle, err := certmanager.GenerateTLSBundle(commonName, dnsNames, validFor)
	if err != nil {
		return fmt.Errorf("generating TLS bundle: %w", err)
	}

	return createTLSSecret(ctx, kube, owner, name, namespace, src.RotateAfter, bundle)
}
