// pkg/resources/shared/probes.go
package shared

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/orkspace/orkestra/pkg/profiles"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// BuildProbe constructs a Kubernetes Probe from a ProbeConfig.
// containerPort is used as the probe target when cfg.Port is 0.
// Returns nil if cfg is nil or no probe type can be inferred.
func BuildProbe(cfg *orktypes.ProbeConfig, containerPort int32, reg orktypes.ProfileRegistry) *corev1.Probe {
	if cfg == nil {
		return nil
	}

	isHTTP := cfg.Type == "http" || (cfg.Type == "" && cfg.Path != "")
	isTCP := cfg.Type == "tcp"

	if !isHTTP && !isTCP {
		return nil
	}

	t := profiles.DefaultProbeTimings
	if pt, ok := profiles.ApplyProbeProfile(cfg.Profile, reg); ok {
		t = pt
	}

	if cfg.InitialDelaySeconds != nil {
		t.InitialDelaySeconds = *cfg.InitialDelaySeconds
	}
	if cfg.PeriodSeconds != nil {
		t.PeriodSeconds = *cfg.PeriodSeconds
	}
	if cfg.FailureThreshold != nil {
		t.FailureThreshold = *cfg.FailureThreshold
	}
	if cfg.SuccessThreshold != nil {
		t.SuccessThreshold = *cfg.SuccessThreshold
	}
	if cfg.TimeoutSeconds != nil {
		t.TimeoutSeconds = *cfg.TimeoutSeconds
	}

	port := containerPort
	if cfg.Port > 0 {
		port = cfg.Port
	}

	probe := &corev1.Probe{
		InitialDelaySeconds: t.InitialDelaySeconds,
		PeriodSeconds:       t.PeriodSeconds,
		FailureThreshold:    t.FailureThreshold,
		SuccessThreshold:    t.SuccessThreshold,
		TimeoutSeconds:      t.TimeoutSeconds,
	}

	if isHTTP {
		probe.ProbeHandler = corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   cfg.Path,
				Port:   intstr.FromInt32(port),
				Scheme: corev1.URISchemeHTTP,
			},
		}
	} else {
		probe.ProbeHandler = corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt32(port),
			},
		}
	}

	return probe
}

// ApplyProbes sets startup, liveness, and readiness probes on c.
// containerPort is used for probes that do not declare their own port.
// No-op when probes is nil.
func ApplyProbes(c *corev1.Container, probes *orktypes.ProbesConfig, containerPort int32, reg orktypes.ProfileRegistry) {
	if probes == nil {
		return
	}
	c.StartupProbe = BuildProbe(probes.Startup, containerPort, reg)
	c.LivenessProbe = BuildProbe(probes.Liveness, containerPort, reg)
	c.ReadinessProbe = BuildProbe(probes.Readiness, containerPort, reg)
}
