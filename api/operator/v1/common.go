package v1

import (
	"encoding/json"
	"fmt"
	"net/url"

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
}

// OAuth2 defines OAuth2 configuration parametrs
// with optional references to secrets with corresponding sensetive values
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
// with optional references to secrets with corresponding sensetive values
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
