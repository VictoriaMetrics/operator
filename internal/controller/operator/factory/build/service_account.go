package build

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type objectForServiceAccountBuilder interface {
	FinalLabels() map[string]string
	FinalAnnotations() map[string]string
	AsOwner() metav1.OwnerReference
	GetNamespace() string
	GetServiceAccountName() string
	IsOwnsServiceAccount() bool
	PrefixedName() string
}

// ServiceAccount builds service account for CRD
func ServiceAccount(cr objectForServiceAccountBuilder) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetServiceAccountName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
	}
}

const serviceAccountTokenVolume = "kube-api-access"

// AddServiceAccountTokenVolumeMount conditionally adds volumeMount to the provided container if automount is not set
func AddServiceAccountTokenVolumeMount(dst *corev1.Container, automount bool) {
	if automount {
		return
	}
	for _, vm := range dst.VolumeMounts {
		if vm.MountPath == "/var/run/secrets/kubernetes.io/serviceaccount" {
			return
		}
	}
	dst.VolumeMounts = append(dst.VolumeMounts, corev1.VolumeMount{
		Name:      serviceAccountTokenVolume,
		MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
		ReadOnly:  true,
	})
}

// AddServiceAccountTokenVolume conditionally adds volume "kube-api-access" with ServiceAccountToken projection
func AddServiceAccountTokenVolume(dst []corev1.Volume, params *vmv1beta1.CommonApplicationDeploymentParams) []corev1.Volume {
	if !params.DisableAutomountServiceAccountToken {
		return dst
	}

	for _, v := range dst {
		if v.Name == serviceAccountTokenVolume {
			return dst
		}
	}

	dst = append(dst, corev1.Volume{
		Name: serviceAccountTokenVolume,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				DefaultMode: ptr.To(int32(420)),
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Path: "token",
						},
					},
					{
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "kube-root-ca.crt",
							},
						},
					},
					{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									FieldRef: &corev1.ObjectFieldSelector{
										APIVersion: "v1",
										FieldPath:  "metadata.namespace",
									},
									Path: "namespace",
								},
							},
						},
					},
				},
			},
		},
	})
	return dst
}
