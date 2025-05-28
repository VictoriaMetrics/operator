package v1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

const (
	healthPath = "/health"
	metricPath = "/metrics"
)

func prefixedName(name, prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, name)
}

// TLSServerConfig defines VictoriaMetrics TLS configuration for the application's server
type TLSServerConfig struct {
	// CertSecretRef defines reference for secret with certificate content under given key
	// mutually exclusive with CertFile
	// +optional
	CertSecret *corev1.SecretKeySelector `json:"certSecret,omitempty"`
	// CertFile defines path to the pre-mounted file with certificate
	// mutually exclusive with CertSecretRef
	// +optional
	CertFile string `json:"certFile,omitempty"`
	// Key defines reference for secret with certificate key content under given key
	// mutually exclusive with KeyFile
	// +optional
	KeySecret *corev1.SecretKeySelector `json:"keySecret,omitempty"`
	// KeyFile defines path to the pre-mounted file with certificate key
	// mutually exclusive with KeySecretRef
	// +optional
	KeyFile string `json:"keyFile,omitempty"`
}
