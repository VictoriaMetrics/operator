package migrate

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// serviceHTTPPort returns the numeric port for the named ServicePort (e.g. "http").
func serviceHTTPPort(svc *corev1.Service, portName string) (int32, bool) {
	for _, p := range svc.Spec.Ports {
		if p.Name == portName {
			return p.Port, true
		}
	}
	return 0, false
}

// ServiceBaseURL builds the in-cluster base URL (scheme://service.namespace.svc:port) for a
// Service's named port.
func ServiceBaseURL(svc *corev1.Service, scheme, portName string) (string, error) {
	port, ok := serviceHTTPPort(svc, portName)
	if !ok {
		return "", fmt.Errorf("service %s/%s has no %q port", svc.Namespace, svc.Name, portName)
	}
	return fmt.Sprintf("%s://%s.%s.svc:%d", scheme, svc.Name, svc.Namespace, port), nil
}

// ForceMergeURL builds the admin force-merge URL for a Service. VictoriaMetrics and
// VictoriaLogs both expose this same endpoint at the same path (see
// https://docs.victoriametrics.com/victorialogs/#forced-merge), so it's shared by both engines.
func ForceMergeURL(svc *corev1.Service) (string, error) {
	base, err := ServiceBaseURL(svc, "http", "http")
	if err != nil {
		return "", err
	}
	return base + "/internal/force_merge", nil
}
