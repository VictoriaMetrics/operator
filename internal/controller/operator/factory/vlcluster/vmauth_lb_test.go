package vlcluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestBuildVMAuthLBDeployment_SecretsAndConfigMaps(t *testing.T) {
	cr := &vmv1.VLCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	cr.Spec.RequestsLoadBalancer = vmv1beta1.VMAuthLoadBalancer{
		Enabled: true,
		Spec: vmv1beta1.VMAuthLoadBalancerSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				Secrets:    []string{"backend-ca"},
				ConfigMaps: []string{"extra-cfg"},
			},
		},
	}

	dep, err := buildVMauthLBDeployment(cr)
	assert.NoError(t, err)

	podSpec := dep.Spec.Template.Spec
	assert.Contains(t, podSpec.Volumes, corev1.Volume{
		Name: "secret-backend-ca",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: "backend-ca"},
		},
	})
	assert.Contains(t, podSpec.Volumes, corev1.Volume{
		Name: "configmap-extra-cfg",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "extra-cfg"},
			},
		},
	})

	mounts := podSpec.Containers[0].VolumeMounts
	assert.Contains(t, mounts, corev1.VolumeMount{
		Name:      "secret-backend-ca",
		ReadOnly:  true,
		MountPath: "/etc/vm/secrets/backend-ca",
	})
	assert.Contains(t, mounts, corev1.VolumeMount{
		Name:      "configmap-extra-cfg",
		ReadOnly:  true,
		MountPath: "/etc/vm/configs/extra-cfg",
	})
}
