package vlsingle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestHTTPListeners(t *testing.T) {
	cr := &vmv1.VLSingle{
		ObjectMeta: metav1.ObjectMeta{Name: "logs", Namespace: "default"},
		Spec: vmv1.VLSingleSpec{CommonAppsParams: vmv1beta1.CommonAppsParams{
			Port: "9428",
			HTTPListeners: []vmv1beta1.HTTPListener{{
				Name: "https", Port: 9430,
				TLS: &vmv1beta1.HTTPListenerTLS{CertFile: "/tls/crt", KeyFile: "/tls/key"},
			}},
		}},
	}
	fclient := k8stools.GetTestClientWithObjects(nil)
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	pod, err := makePodSpec(cr)
	require.NoError(t, err)
	container := pod.Spec.Containers[0]
	require.Subset(t, container.Args, []string{
		"-httpListenAddr=:9428,:9430", "-tls=false,true",
		"-tlsCertFile=,/tls/crt", "-tlsKeyFile=,/tls/key",
	})
	require.Contains(t, container.Ports, corev1.ContainerPort{Name: "https", Protocol: corev1.ProtocolTCP, ContainerPort: 9430})

	ctx := context.Background()
	require.NoError(t, createOrUpdateService(ctx, fclient, cr, nil))
	var svc corev1.Service
	require.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}, &svc))
	require.Contains(t, svc.Spec.Ports, corev1.ServicePort{
		Name: "https", Protocol: corev1.ProtocolTCP, Port: 9430, TargetPort: intstr.FromInt32(9430),
	})
}
