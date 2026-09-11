package v1

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	corev1 "k8s.io/api/core/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const (
	healthPath  = "/health"
	metricsPath = "/metrics"
	// OTLPGRPCPortName is the Service/container port name generated for the OTLP gRPC listener.
	OTLPGRPCPortName = "otlp-grpc"
)

// TLSServerConfig defines VictoriaMetrics TLS configuration for the application's server
type TLSServerConfig struct {
	// CertSecretRef defines reference for secret with certificate content under given key
	// mutually exclusive with CertFile
	// +optional
	CertSecret *corev1.SecretKeySelector `json:"certSecret,omitempty"`
	// CertFile defines path to the pre-mounted file with certificate
	// mutually exclusive with CertSecret
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
	// MinVersion is the minimum TLS version to use for the server.
	// +optional
	// +kubebuilder:validation:Enum=TLS10;TLS11;TLS12;TLS13
	MinVersion string `json:"minVersion,omitempty"`
	// CipherSuites is an optional list of TLS cipher suites for the server.
	// +optional
	CipherSuites []string `json:"cipherSuites,omitempty"`
}

// OTLPGRPCSpec defines OTLP gRPC ingestion server configuration
type OTLPGRPCSpec struct {
	// ListenPort defines listen port for OTLP gRPC requests
	ListenPort int32 `json:"listenPort"`
	// TLSConfig enables TLS for incoming gRPC requests at the given ListenPort.
	// If unset, TLS is disabled for the gRPC listener.
	// +optional
	TLSConfig *TLSServerConfig `json:"tlsConfig,omitempty"`
}

// Validate checks that ListenPort doesn't collide with httpPort or any configured HTTPListener,
// and that no HTTPListener is named OTLPGRPCPortName.
func (g *OTLPGRPCSpec) Validate(httpPort string, listeners []vmv1beta1.HTTPListener) error {
	if g == nil {
		return nil
	}
	if len(listeners) == 0 {
		if portsEqual(strconv.Itoa(int(g.ListenPort)), httpPort) {
			return fmt.Errorf("spec.grpcSpec.listenPort=%d must not be equal to the HTTP listen port=%s", g.ListenPort, httpPort)
		}
		return nil
	}
	for i := range listeners {
		if listeners[i].Name == OTLPGRPCPortName {
			return fmt.Errorf("httpListeners[%d].name cannot be %q while spec.grpcSpec is enabled, since it collides with the generated OTLP gRPC port name", i, OTLPGRPCPortName)
		}
		if port := listeners[i].AddrPort(); portsEqual(strconv.Itoa(int(g.ListenPort)), port) {
			return fmt.Errorf("spec.grpcSpec.listenPort=%d must not be equal to httpListeners[%d].addr port=%s", g.ListenPort, i, port)
		}
	}
	return nil
}

// portsEqual compares two port strings numerically when both parse as integers,
// falling back to a plain string comparison otherwise.
func portsEqual(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr != nil || berr != nil {
		return a == b
	}
	return ai == bi
}

// OAuth2 defines OAuth2 configuration parameters
// with optional references to secrets with corresponding sensitive values
type OAuth2 struct {
	// ClientIDSecret defines secret or configmap containing the OAuth2 client id
	// +optional
	ClientIDSecret *corev1.SecretKeySelector `json:"clientIDSecret,omitempty"`
	// ClientIDFile defines path to pre-mounted OAuth2 client id
	// +optional
	ClientIDFile string `json:"clientIDFile,omitempty"`
	// The secret containing the OAuth2 client secret
	// +optional
	ClientSecret *corev1.SecretKeySelector `json:"clientSecret,omitempty"`
	// ClientSecretFile defines path to pre-mounted OAuth2 client secret
	// +optional
	ClientSecretFile string `json:"clientSecretFile,omitempty"`
	// TokenURL defines URL to fetch the token from
	// +kubebuilder:validation:MinLength=1
	// +required
	TokenURL string `json:"tokenURL"`
	// Scopes used for the token request
	// +optional
	Scopes []string `json:"scopes,omitempty"`
	// EndpointParams to append to the token URL
	// +optional
	EndpointParams map[string]string `json:"endpointParams,omitempty"`
}

// Validate performs syntax
func (o *OAuth2) Validate() error {
	if o == nil {
		return nil
	}
	if o.ClientIDFile == "" && o.ClientIDSecret == nil {
		return fmt.Errorf("expect one of clientIDFile or clientIDSecret, got none")
	}
	if o.ClientIDFile != "" && o.ClientIDSecret != nil {
		return fmt.Errorf("expect one of clientIDFile or clientIDSecret, got both")
	}
	if o.ClientSecretFile == "" && o.ClientSecret == nil {
		return fmt.Errorf("expect one of clientSecretFile or clientSecret, got none")
	}
	if o.ClientSecretFile != "" && o.ClientSecret != nil {
		return fmt.Errorf("expect one of clientSecretFile or clientSecret, got both")
	}
	if _, err := url.Parse(o.TokenURL); err != nil {
		return fmt.Errorf("cannot parse tokenURL: %w", err)
	}
	if len(o.EndpointParams) > 0 {
		if _, err := json.Marshal(o.EndpointParams); err != nil {
			return fmt.Errorf("cannot marshal EndpointParams as json: %w", err)
		}
	}

	return nil
}

// TLSConfig specifies TLS configuration parameters
// with optional references to secrets with corresponding sensitive values
type TLSConfig struct {
	// CASecret defines secret reference with tls CA key by given key
	// +optional
	CASecret *corev1.SecretKeySelector `json:"caSecretKeyRef,omitempty"`
	// CAFile defines path to the pre-mounted file with TLS ca certificate
	// +optional
	CAFile string `json:"caFile,omitempty"`
	// CertSecret defines secret reference with TLS cert by given key
	// mutually exclusive with CASecret
	// +optional
	CertSecret *corev1.SecretKeySelector `json:"certSecretKeyRef,omitempty"`
	// CertFile defines path to the pre-mounted file with TLS certificate
	// mutually exclusive with CertSecret
	// +optional
	CertFile string `json:"certFile,omitempty"`
	// CertSecret defines secret reference with TLS key by given key
	// +optional
	KeySecret *corev1.SecretKeySelector `json:"keySecretKeyRef,omitempty" yaml:"-"`
	// KeyFile defines path to the pre-mounted file with TLS cert key
	// mutually exclusive with CertSecret
	// +optional
	KeyFile string `json:"keyFile,omitempty"`
	// Used to verify the hostname for the targets.
	// +optional
	ServerName string `json:"serverName,omitempty"`
	// Disable target certificate validation.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// Validate performs syntax
func (t *TLSConfig) Validate() error {
	if t == nil {
		return nil
	}
	if t.CAFile != "" && t.CASecret != nil {
		return fmt.Errorf("expect one of caFile or caSecret, got both")
	}
	if t.CertFile != "" && t.CertSecret != nil {
		return fmt.Errorf("expect one of certFile or certSecret, got both")
	}
	if t.KeyFile != "" && t.KeySecret != nil {
		return fmt.Errorf("expect one of keyFile or keySecret, got both")
	}
	return nil
}
