package vtcluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
)

func TestBuildVMAuthLBDeployment_SecretsAndConfigMaps(t *testing.T) {
	cr := &vmv1.VTCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	cr.Spec.RequestsLoadBalancer.Spec.Secrets = []string{"backend-ca"}
	cr.Spec.RequestsLoadBalancer.Spec.ConfigMaps = []string{"extra-cfg"}

	dep, err := buildVMauthLBDeployment(cr)
	require.NoError(t, err)

	volumes := dep.Spec.Template.Spec.Volumes
	assert.Contains(t, volumes, corev1.Volume{
		Name:         "secret-backend-ca",
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "backend-ca"}},
	})
	assert.Contains(t, volumes, corev1.Volume{
		Name: "configmap-extra-cfg",
		VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "extra-cfg"},
		}},
	})

	mounts := dep.Spec.Template.Spec.Containers[0].VolumeMounts
	assert.Contains(t, mounts, corev1.VolumeMount{Name: "secret-backend-ca", ReadOnly: true, MountPath: "/etc/vm/secrets/backend-ca"})
	assert.Contains(t, mounts, corev1.VolumeMount{Name: "configmap-extra-cfg", ReadOnly: true, MountPath: "/etc/vm/configs/extra-cfg"})
}
