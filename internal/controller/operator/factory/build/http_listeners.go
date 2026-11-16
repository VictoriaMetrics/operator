package build

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// AddHTTPListenerArgs merges primary listener flags from args with additional listeners
func AddHTTPListenerArgs(args []string, listeners []vmv1beta1.HTTPListener) []string {
	if len(listeners) == 0 {
		return args
	}

	// collect and drop primary listener flags
	primary := make(map[string]string)
	dst := make([]string, 0, len(args))
	for _, arg := range args {
		name, value, ok := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !slices.Contains(vmv1beta1.HTTPListenerFlags, name) {
			dst = append(dst, arg)
			continue
		}
		if !ok {
			// boolean flag without value, e.g. -tls
			value = "true"
		}
		primary[name] = value
	}

	// Build a list of primary listener flags
	// boolean flags default to false, so they are omitted unless enabled for some listener
	addr := NewEmptyFlag("-httpListenAddr")
	proxy := NewFlag("-httpListenAddr.useProxyProtocol", "false")
	tls := NewFlag("-tls", "false")
	certFile := NewEmptyFlag("-tlsCertFile")
	keyFile := NewEmptyFlag("-tlsKeyFile")
	mtls := NewFlag("-mtls", "false")
	caFile := NewEmptyFlag("-mtlsCAFile")
	ordered := []*Flag{addr, proxy, tls, certFile, keyFile, mtls, caFile}
	for _, f := range ordered {
		if v, ok := primary[strings.TrimPrefix(f.name, "-")]; ok {
			f.Add(v, 0)
		}
	}

	// add secondary listeners (index 0 is primary)
	for i, l := range listeners {
		idx := i + 1
		addr.Add(fmt.Sprintf(":%d", l.Port), idx)
		proxy.Add(strconv.FormatBool(l.UseProxyProtocol), idx)
		tls.Add(strconv.FormatBool(l.TLS != nil), idx)
		if l.TLS == nil {
			continue
		}
		certFile.Add(l.TLS.CertFile, idx)
		keyFile.Add(l.TLS.KeyFile, idx)
		mtls.Add(strconv.FormatBool(l.TLS.MTLS), idx)
		caFile.Add(l.TLS.MTLSCAFile, idx)
	}
	return AppendFlagsToArgs(dst, len(listeners)+1, ordered...)
}

// AddHTTPListenerPorts adds container ports of additional HTTP listeners
func AddHTTPListenerPorts(dst []corev1.ContainerPort, listeners []vmv1beta1.HTTPListener) []corev1.ContainerPort {
	for _, l := range listeners {
		dst = append(dst, corev1.ContainerPort{
			Name:          l.Name,
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: l.Port,
		})
	}
	return dst
}

// AddHTTPListenerServicePorts adds service ports of additional HTTP listeners
func AddHTTPListenerServicePorts(svc *corev1.Service, listeners []vmv1beta1.HTTPListener) {
	for _, l := range listeners {
		svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
			Name:       l.Name,
			Protocol:   corev1.ProtocolTCP,
			Port:       l.Port,
			TargetPort: intstr.FromInt32(l.Port),
		})
	}
}
