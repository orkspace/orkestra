// pkg/resources/shared/security.go
package shared

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/profiles"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ResolveContainerSecurityContext resolves a ContainerSecurityContext:
// if a profile is set it expands to explicit fields; otherwise the block is
// returned as-is. Returns nil when sc is nil.
func ResolveContainerSecurityContext(sc *orktypes.ContainerSecurityContext, reg orktypes.ProfileRegistry) *orktypes.ContainerSecurityContext {
	if sc == nil {
		return nil
	}
	if sc.Profile != "" {
		expanded, err := profiles.ApplyContainerSecurityProfile(sc.Profile, reg)
		if err != nil {
			logger.Warn().Str("profile", sc.Profile).Err(err).Msg("unknown securityContext.profile — skipping")
			return nil
		}
		return expanded
	}
	return sc
}

// ResolvePodSecurityContext resolves a PodSecurityContext:
// if a profile is set it expands to explicit fields; otherwise the block is
// returned as-is. Returns nil when ps is nil.
func ResolvePodSecurityContext(ps *orktypes.PodSecurityContext, reg orktypes.ProfileRegistry) *orktypes.PodSecurityContext {
	if ps == nil {
		return nil
	}
	if ps.Profile != "" {
		expanded, err := profiles.ApplyPodSecurityProfile(ps.Profile, reg)
		if err != nil {
			logger.Warn().Str("profile", ps.Profile).Err(err).Msg("unknown podSecurity.profile — skipping")
			return nil
		}
		return expanded
	}
	return ps
}

// BuildContainerSecurityContext converts an orktypes.ContainerSecurityContext to
// a Kubernetes corev1.SecurityContext. Returns nil when sc is nil.
func BuildContainerSecurityContext(sc *orktypes.ContainerSecurityContext) *corev1.SecurityContext {
	if sc == nil {
		return nil
	}
	k8s := &corev1.SecurityContext{
		AllowPrivilegeEscalation: sc.AllowPrivilegeEscalation,
		ReadOnlyRootFilesystem:   sc.ReadOnlyRootFilesystem,
		RunAsNonRoot:             sc.RunAsNonRoot,
		RunAsUser:                sc.RunAsUser,
		RunAsGroup:               sc.RunAsGroup,
	}
	if sc.Capabilities != nil {
		k8s.Capabilities = &corev1.Capabilities{}
		for _, cap := range sc.Capabilities.Add {
			k8s.Capabilities.Add = append(k8s.Capabilities.Add, corev1.Capability(cap))
		}
		for _, cap := range sc.Capabilities.Drop {
			k8s.Capabilities.Drop = append(k8s.Capabilities.Drop, corev1.Capability(cap))
		}
	}
	return k8s
}

// BuildPodSecurityContext converts an orktypes.PodSecurityContext to a
// Kubernetes corev1.PodSecurityContext. Returns nil when ps is nil.
func BuildPodSecurityContext(ps *orktypes.PodSecurityContext) *corev1.PodSecurityContext {
	if ps == nil {
		return nil
	}
	return &corev1.PodSecurityContext{
		RunAsNonRoot: ps.RunAsNonRoot,
		RunAsUser:    ps.RunAsUser,
		RunAsGroup:   ps.RunAsGroup,
		FSGroup:      ps.FSGroup,
	}
}

// ApplySecurityContext sets the container security context and pod security context
// on the given container and pod spec. No-op when both are nil.
func ApplySecurityContext(container *corev1.Container, pod *corev1.PodSpec, sc *orktypes.ContainerSecurityContext, ps *orktypes.PodSecurityContext) {
	if sc != nil {
		container.SecurityContext = BuildContainerSecurityContext(sc)
	}
	if ps != nil {
		pod.SecurityContext = BuildPodSecurityContext(ps)
	}
}
