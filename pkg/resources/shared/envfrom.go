package shared

import (
	corev1 "k8s.io/api/core/v1"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ExpandEnvFrom converts an EnvFrom declaration into Kubernetes container
// fields. A ref with no Keys becomes a native corev1.EnvFromSource (blanket
// import, Prefix/Optional supported directly by Kubernetes). A ref with Keys
// set is expanded into individual corev1.EnvVar entries instead — Kubernetes
// has no way to rename or suffix a key during a blanket envFrom import.
// Returns (nil, nil) when ef is nil.
func ExpandEnvFrom(ef *orktypes.EnvFrom) (envFrom []corev1.EnvFromSource, extraEnv []corev1.EnvVar) {
	if ef == nil {
		return nil, nil
	}

	for _, ref := range ef.SecretRef {
		if len(ref.Keys) == 0 {
			envFrom = append(envFrom, corev1.EnvFromSource{
				Prefix: ref.Prefix,
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
					Optional:             ref.Optional,
				},
			})
			continue
		}
		for _, key := range ref.Keys {
			extraEnv = append(extraEnv, corev1.EnvVar{
				Name: ref.Prefix + key + ref.Suffix,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
						Key:                  key,
						Optional:             ref.Optional,
					},
				},
			})
		}
	}

	for _, ref := range ef.ConfigMapRef {
		if len(ref.Keys) == 0 {
			envFrom = append(envFrom, corev1.EnvFromSource{
				Prefix: ref.Prefix,
				ConfigMapRef: &corev1.ConfigMapEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
					Optional:             ref.Optional,
				},
			})
			continue
		}
		for _, key := range ref.Keys {
			extraEnv = append(extraEnv, corev1.EnvVar{
				Name: ref.Prefix + key + ref.Suffix,
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
						Key:                  key,
						Optional:             ref.Optional,
					},
				},
			})
		}
	}

	return envFrom, extraEnv
}
