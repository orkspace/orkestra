package shared

import (
	corev1 "k8s.io/api/core/v1"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// BuildVolumes converts a slice of VolumeSource declarations into Kubernetes
// corev1.Volume objects ready to set on PodSpec.Volumes.
// Returns nil when the input is empty.
func BuildVolumes(vols []orktypes.VolumeSource) []corev1.Volume {
	if len(vols) == 0 {
		return nil
	}
	result := make([]corev1.Volume, 0, len(vols))
	for _, v := range vols {
		vol := corev1.Volume{Name: v.Name}
		switch {
		case v.ConfigMap != nil:
			vol.VolumeSource = corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: v.ConfigMap.Name},
				},
			}
		case v.Secret != nil:
			vol.VolumeSource = corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: v.Secret.Name,
				},
			}
		case v.EmptyDir != nil:
			vol.VolumeSource = corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			}
		case v.PersistentVolumeClaim != nil:
			vol.VolumeSource = corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: v.PersistentVolumeClaim.ClaimName,
					ReadOnly:  v.PersistentVolumeClaim.ReadOnly,
				},
			}
		case v.HostPath != nil:
			vs := corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: v.HostPath.Path,
				},
			}
			if v.HostPath.Type != "" {
				t := corev1.HostPathType(v.HostPath.Type)
				vs.HostPath.Type = &t
			}
			vol.VolumeSource = vs
		}
		result = append(result, vol)
	}
	return result
}

// BuildVolumeMounts converts a slice of VolumeMount declarations into
// Kubernetes corev1.VolumeMount objects ready to set on a Container.
// Returns nil when the input is empty.
func BuildVolumeMounts(mounts []orktypes.VolumeMount) []corev1.VolumeMount {
	if len(mounts) == 0 {
		return nil
	}
	result := make([]corev1.VolumeMount, 0, len(mounts))
	for _, m := range mounts {
		result = append(result, corev1.VolumeMount{
			Name:      m.Name,
			MountPath: m.MountPath,
			SubPath:   m.SubPath,
			ReadOnly:  m.ReadOnly,
		})
	}
	return result
}
