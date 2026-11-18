package build

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestAddHTTPListenerArgs(t *testing.T) {
	f := func(args []string, listeners []vmv1beta1.HTTPListener, want []string) {
		t.Helper()
		got := AddHTTPListenerArgs(args, listeners)
		sort.Strings(got)
		sort.Strings(want)
		assert.Equal(t, want, got)
	}

	// no listeners
	f([]string{"-httpListenAddr=:8428", "-tls"}, nil, []string{"-httpListenAddr=:8428", "-tls"})

	// plain extra listener
	f([]string{"-httpListenAddr=:8428", "-retentionPeriod=1"}, []vmv1beta1.HTTPListener{
		{Name: "plain", Port: 8429},
	}, []string{"-httpListenAddr=:8428,:8429", "-retentionPeriod=1"})

	// primary tls from extraArgs and mtls listener
	f([]string{"-httpListenAddr=:8428", "-tls", "-tlsCertFile=/primary/crt", "-tlsKeyFile=/primary/key"}, []vmv1beta1.HTTPListener{
		{Name: "mtls", Port: 9409, TLS: &vmv1beta1.HTTPListenerTLS{CertFile: "/tls/crt", KeyFile: "/tls/key", MTLS: true, MTLSCAFile: "/tls/ca"}},
	}, []string{
		"-httpListenAddr=:8428,:9409",
		"-mtls=false,true",
		"-mtlsCAFile=,/tls/ca",
		"-tls=true,true",
		"-tlsCertFile=/primary/crt,/tls/crt",
		"-tlsKeyFile=/primary/key,/tls/key",
	})

	// proxy protocol on second listener only
	f([]string{"-httpListenAddr=:8427"}, []vmv1beta1.HTTPListener{
		{Name: "plain", Port: 8428},
		{Name: "proxy", Port: 8429, UseProxyProtocol: true},
	}, []string{
		"-httpListenAddr=:8427,:8428,:8429",
		"-httpListenAddr.useProxyProtocol=false,false,true",
	})

	// tls listener after plain one, mtls with system CAs
	f([]string{"-httpListenAddr=:8427"}, []vmv1beta1.HTTPListener{
		{Name: "plain", Port: 8428},
		{Name: "mtls", Port: 8429, TLS: &vmv1beta1.HTTPListenerTLS{CertFile: "/tls/crt", KeyFile: "/tls/key", MTLS: true}},
	}, []string{
		"-httpListenAddr=:8427,:8428,:8429",
		"-mtls=false,false,true",
		"-tls=false,false,true",
		"-tlsCertFile=,,/tls/crt",
		"-tlsKeyFile=,,/tls/key",
	})
}

func TestAddHTTPListenerPorts(t *testing.T) {
	listeners := []vmv1beta1.HTTPListener{{Name: "mtls", Port: 9409}}
	ports := AddHTTPListenerPorts([]corev1.ContainerPort{{Name: "http", ContainerPort: 8428}}, listeners)
	assert.Equal(t, []corev1.ContainerPort{
		{Name: "http", ContainerPort: 8428},
		{Name: "mtls", Protocol: corev1.ProtocolTCP, ContainerPort: 9409},
	}, ports)

	svc := &corev1.Service{}
	AddHTTPListenerServicePorts(svc, listeners)
	assert.Equal(t, []corev1.ServicePort{
		{Name: "mtls", Protocol: corev1.ProtocolTCP, Port: 9409, TargetPort: intstr.FromInt32(9409)},
	}, svc.Spec.Ports)
}
