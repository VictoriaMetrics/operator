package vtcluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestBuildVTInsertPodSpec_GRPC(t *testing.T) {
	cr := &vmv1.VTCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "traces-1", Namespace: "default"},
		Spec: vmv1.VTClusterSpec{
			Insert: &vmv1.VTInsert{
				StandardAppsParams: vmv1beta1.StandardAppsParams{
					CommonAppsParams: vmv1beta1.CommonAppsParams{Port: "10428"},
				},
				GRPCSpec: &vmv1.OTLPGRPCSpec{
					ListenPort: 4317,
					TLSConfig: &vmv1.TLSServerConfig{
						CertSecret: &corev1.SecretKeySelector{
							Key:                  "CERT",
							LocalObjectReference: corev1.LocalObjectReference{Name: "tls"},
						},
						KeySecret: &corev1.SecretKeySelector{
							Key:                  "KEY",
							LocalObjectReference: corev1.LocalObjectReference{Name: "tls"},
						},
					},
				},
			},
		},
	}
	spec, err := buildVTInsertPodSpec(cr)
	require.NoError(t, err)
	require.Len(t, spec.Spec.Containers, 1)
	c := spec.Spec.Containers[0]
	assert.Contains(t, c.Args, "-otlpGRPCListenAddr=:4317")
	assert.Contains(t, c.Args, "-otlpGRPC.tls=true")
	assert.Contains(t, c.Args, "-otlpGRPC.tlsCertFile=/etc/vt/tls-server-secrets/tls/CERT")
	assert.Contains(t, c.Args, "-otlpGRPC.tlsKeyFile=/etc/vt/tls-server-secrets/tls/KEY")
	assert.Contains(t, c.Ports, corev1.ContainerPort{Name: "otlp-grpc", Protocol: corev1.ProtocolTCP, ContainerPort: 4317})

	var found bool
	for _, m := range c.VolumeMounts {
		if m.Name == "tls-tls" {
			found = true
			assert.Equal(t, "/etc/vt/tls-server-secrets/tls", m.MountPath)
		}
	}
	assert.True(t, found, "expected tls-tls volume mount")
}

func TestBuildVTInsertService_GRPC(t *testing.T) {
	cr := &vmv1.VTCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "traces-1", Namespace: "default"},
		Spec: vmv1.VTClusterSpec{
			Insert: &vmv1.VTInsert{
				StandardAppsParams: vmv1beta1.StandardAppsParams{
					CommonAppsParams: vmv1beta1.CommonAppsParams{Port: "10428"},
				},
				GRPCSpec: &vmv1.OTLPGRPCSpec{ListenPort: 4317},
			},
		},
	}
	svc := buildVTInsertService(cr)
	require.Len(t, svc.Spec.Ports, 2)
	assert.Equal(t, "otlp-grpc", svc.Spec.Ports[1].Name)
	assert.Equal(t, int32(4317), svc.Spec.Ports[1].Port)
}
