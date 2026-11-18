package vtcluster

import (
	"context"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestVTClusterHTTPListeners(t *testing.T) {
	cr := &vmv1.VTCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "listeners", Namespace: "default"},
		Spec: vmv1.VTClusterSpec{
			Insert: &vmv1.VTInsert{CommonAppsParams: vmv1beta1.CommonAppsParams{
				Port: "10481", ReplicaCount: ptr.To(int32(1)),
				HTTPListeners: []vmv1beta1.HTTPListener{{
					Name: "secure", Port: 10482,
					TLS: &vmv1beta1.HTTPListenerTLS{CertFile: "/tls/crt", KeyFile: "/tls/key"},
				}},
			}},
			Select: &vmv1.VTSelect{CommonAppsParams: vmv1beta1.CommonAppsParams{
				Port: "10471", ReplicaCount: ptr.To(int32(1)),
				HTTPListeners: []vmv1beta1.HTTPListener{{Name: "proxy", Port: 10472, UseProxyProtocol: true}},
			}},
			Storage: &vmv1.VTStorage{CommonAppsParams: vmv1beta1.CommonAppsParams{
				Port: "10491", ReplicaCount: ptr.To(int32(1)),
				HTTPListeners: []vmv1beta1.HTTPListener{{Name: "extra", Port: 10492}},
			}},
			RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
				Enabled: true,
				Spec: vmv1beta1.VMAuthLoadBalancerSpec{CommonAppsParams: vmv1beta1.CommonAppsParams{
					Port: "8427", ReplicaCount: ptr.To(int32(1)),
					HTTPListeners: []vmv1beta1.HTTPListener{{Name: "mtls", Port: 8429,
						TLS: &vmv1beta1.HTTPListenerTLS{CertFile: "/tls/crt", KeyFile: "/tls/key", MTLS: true, MTLSCAFile: "/tls/ca"},
					}},
				}},
			},
		},
	}
	fclient := k8stools.GetTestClientWithObjects(nil)
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	tests := []struct {
		name         string
		component    vmv1beta1.ClusterComponent
		primaryPort  int32
		listener     vmv1beta1.HTTPListener
		listenerArgs []string
		statefulSet  bool
		internalSvc  bool
	}{
		{
			name: "insert", component: vmv1beta1.ClusterComponentInsert, primaryPort: 10481,
			listener: cr.Spec.Insert.HTTPListeners[0], internalSvc: true,
			listenerArgs: []string{"-httpListenAddr=:10481,:10482", "-tls=false,true", "-tlsCertFile=,/tls/crt", "-tlsKeyFile=,/tls/key"},
		},
		{
			name: "select", component: vmv1beta1.ClusterComponentSelect, primaryPort: 10471,
			listener: cr.Spec.Select.HTTPListeners[0], internalSvc: true,
			listenerArgs: []string{"-httpListenAddr=:10471,:10472", "-httpListenAddr.useProxyProtocol=false,true"},
		},
		{
			name: "storage", component: vmv1beta1.ClusterComponentStorage, primaryPort: 10491,
			listener: cr.Spec.Storage.HTTPListeners[0], statefulSet: true,
			listenerArgs: []string{"-httpListenAddr=:10491,:10492"},
		},
		{
			name: "vmauth_lb", component: vmv1beta1.ClusterComponentBalancer, primaryPort: 8427,
			listener:     cr.Spec.RequestsLoadBalancer.Spec.HTTPListeners[0],
			listenerArgs: []string{"-httpListenAddr=:8427,:8429", "-tls=false,true", "-tlsCertFile=,/tls/crt", "-tlsKeyFile=,/tls/key", "-mtls=false,true", "-mtlsCAFile=,/tls/ca"},
		},
	}

	synctest.Test(t, func(t *testing.T) {
		require.NoError(t, CreateOrUpdate(context.Background(), fclient, cr))
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := cr.PrefixedName(tt.component)
			key := types.NamespacedName{Name: name, Namespace: cr.Namespace}
			var containers []corev1.Container
			if tt.statefulSet {
				var sts appsv1.StatefulSet
				require.NoError(t, fclient.Get(context.Background(), key, &sts))
				containers = sts.Spec.Template.Spec.Containers
			} else {
				var dep appsv1.Deployment
				require.NoError(t, fclient.Get(context.Background(), key, &dep))
				containers = dep.Spec.Template.Spec.Containers
			}
			require.Len(t, containers, 1)
			wantPorts := []corev1.ContainerPort{
				{Name: "http", Protocol: corev1.ProtocolTCP, ContainerPort: tt.primaryPort},
				{Name: tt.listener.Name, Protocol: corev1.ProtocolTCP, ContainerPort: tt.listener.Port},
			}
			require.Equal(t, wantPorts, containers[0].Ports)
			var listenerArgs []string
			for _, arg := range containers[0].Args {
				flag, _, _ := strings.Cut(strings.TrimPrefix(arg, "-"), "=")
				if slices.Contains(vmv1beta1.HTTPListenerFlags, flag) {
					listenerArgs = append(listenerArgs, arg)
				}
			}
			require.ElementsMatch(t, tt.listenerArgs, listenerArgs)

			if tt.internalSvc {
				key.Name = cr.PrefixedInternalName(tt.component)
			}
			var svc corev1.Service
			require.NoError(t, fclient.Get(context.Background(), key, &svc))
			require.Equal(t, []corev1.ServicePort{
				{Name: "http", Protocol: corev1.ProtocolTCP, Port: tt.primaryPort, TargetPort: intstr.FromInt32(tt.primaryPort)},
				{Name: tt.listener.Name, Protocol: corev1.ProtocolTCP, Port: tt.listener.Port, TargetPort: intstr.FromInt32(tt.listener.Port)},
			}, svc.Spec.Ports)
		})
	}
}
