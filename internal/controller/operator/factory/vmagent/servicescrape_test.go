package vmagent

import (
	"context"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func Test_generateServiceScrapeConfig(t *testing.T) {
	type args struct {
		cr              vmv1beta1.VMAgent
		m               *vmv1beta1.VMServiceScrape
		ep              vmv1beta1.Endpoint
		i               int
		apiserverConfig *vmv1beta1.APIServerConfig
		ssCache         *scrapesSecretsCache
		se              vmv1beta1.VMAgentSecurityEnforcements
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "generate simple config",
			args: args{
				m: &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "8080",
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CA: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-secret",
												},
												Key: "ca",
											},
										},
									},
									BearerTokenFile: "/var/run/tolen",
								},
							},
						},
					},
				},
				ep: vmv1beta1.Endpoint{
					AttachMetadata: vmv1beta1.AttachMetadata{
						Node: ptr.To(true),
					},
					Port: "8080",
					EndpointAuth: vmv1beta1.EndpointAuth{
						TLSConfig: &vmv1beta1.TLSConfig{
							Cert: vmv1beta1.SecretOrConfigMap{},
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "ca",
								},
							},
						},
						BearerTokenFile: "/var/run/tolen",
					},
				},
				i:               0,
				apiserverConfig: nil,
				ssCache:         &scrapesSecretsCache{},
				se: vmv1beta1.VMAgentSecurityEnforcements{
					OverrideHonorLabels:      false,
					OverrideHonorTimestamps:  false,
					IgnoreNamespaceSelectors: false,
					EnforcedNamespaceLabel:   "",
				},
			},
			want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: endpoints
  attach_metadata:
    node: true
  namespaces:
    names:
    - default
honor_labels: false
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_endpoint_port_name
  regex: "8080"
- source_labels:
  - __meta_kubernetes_endpoint_address_target_kind
  - __meta_kubernetes_endpoint_address_target_name
  separator: ;
  regex: Node;(.*)
  replacement: ${1}
  target_label: node
- source_labels:
  - __meta_kubernetes_endpoint_address_target_kind
  - __meta_kubernetes_endpoint_address_target_name
  separator: ;
  regex: Pod;(.*)
  replacement: ${1}
  target_label: pod
- source_labels:
  - __meta_kubernetes_pod_name
  target_label: pod
- source_labels:
  - __meta_kubernetes_pod_container_name
  target_label: container
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_service_name
  target_label: service
- source_labels:
  - __meta_kubernetes_service_name
  target_label: job
  replacement: ${1}
- target_label: endpoint
  replacement: "8080"
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
`,
		},

		{
			name: "generate config with scrape interval limit",
			args: args{
				cr: vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						MaxScrapeInterval: ptr.To("40m"),
					},
				},
				m: &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "8080",
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CA: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-secret",
												},
												Key: "ca",
											},
										},
									},
									BearerTokenFile: "/var/run/tolen",
								},
							},
						},
					},
				},
				ep: vmv1beta1.Endpoint{
					Port: "8080",
					EndpointAuth: vmv1beta1.EndpointAuth{
						TLSConfig: &vmv1beta1.TLSConfig{
							Cert: vmv1beta1.SecretOrConfigMap{},
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "ca",
								},
							},
						},
						BearerTokenFile: "/var/run/tolen",
					},
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						ScrapeInterval: "60m",
					},
				},
				i:               0,
				apiserverConfig: nil,
				ssCache:         &scrapesSecretsCache{},
			},
			want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: endpoints
  namespaces:
    names:
    - default
honor_labels: false
scrape_interval: 40m
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_endpoint_port_name
  regex: "8080"
- source_labels:
  - __meta_kubernetes_endpoint_address_target_kind
  - __meta_kubernetes_endpoint_address_target_name
  separator: ;
  regex: Node;(.*)
  replacement: ${1}
  target_label: node
- source_labels:
  - __meta_kubernetes_endpoint_address_target_kind
  - __meta_kubernetes_endpoint_address_target_name
  separator: ;
  regex: Pod;(.*)
  replacement: ${1}
  target_label: pod
- source_labels:
  - __meta_kubernetes_pod_name
  target_label: pod
- source_labels:
  - __meta_kubernetes_pod_container_name
  target_label: container
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_service_name
  target_label: service
- source_labels:
  - __meta_kubernetes_service_name
  target_label: job
  replacement: ${1}
- target_label: endpoint
  replacement: "8080"
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
`,
		},
		{
			name: "generate config with scrape interval limit - reach min",
			args: args{
				cr: vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						MinScrapeInterval: ptr.To("1m"),
					},
				},
				m: &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "8080",
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CA: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-secret",
												},
												Key: "ca",
											},
										},
									},
									BearerTokenFile: "/var/run/tolen",
								},
							},
						},
					},
				},
				ep: vmv1beta1.Endpoint{
					Port: "8080",
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						ScrapeInterval: "10s",
					},

					EndpointAuth: vmv1beta1.EndpointAuth{
						TLSConfig: &vmv1beta1.TLSConfig{
							Cert: vmv1beta1.SecretOrConfigMap{},
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "ca",
								},
							},
						},
						BearerTokenFile: "/var/run/tolen",
					},
				},
				i:               0,
				apiserverConfig: nil,
				ssCache:         &scrapesSecretsCache{},
			},
			want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: endpoints
  namespaces:
    names:
    - default
honor_labels: false
scrape_interval: 1m
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_endpoint_port_name
  regex: "8080"
- source_labels:
  - __meta_kubernetes_endpoint_address_target_kind
  - __meta_kubernetes_endpoint_address_target_name
  separator: ;
  regex: Node;(.*)
  replacement: ${1}
  target_label: node
- source_labels:
  - __meta_kubernetes_endpoint_address_target_kind
  - __meta_kubernetes_endpoint_address_target_name
  separator: ;
  regex: Pod;(.*)
  replacement: ${1}
  target_label: pod
- source_labels:
  - __meta_kubernetes_pod_name
  target_label: pod
- source_labels:
  - __meta_kubernetes_pod_container_name
  target_label: container
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_service_name
  target_label: service
- source_labels:
  - __meta_kubernetes_service_name
  target_label: job
  replacement: ${1}
- target_label: endpoint
  replacement: "8080"
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
`,
		},
		{
			name: "config with discovery role endpointslices",
			args: args{
				m: &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						DiscoveryRole: kubernetesSDRoleEndpointSlices,
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "8080",
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CA: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-secret",
												},
												Key: "ca",
											},
										},
									},
									BearerTokenFile: "/var/run/tolen",
								},
							},
						},
					},
				},
				ep: vmv1beta1.Endpoint{
					Port: "8080",
					EndpointAuth: vmv1beta1.EndpointAuth{
						TLSConfig: &vmv1beta1.TLSConfig{
							Cert: vmv1beta1.SecretOrConfigMap{},
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "ca",
								},
							},
						},
						BearerTokenFile: "/var/run/tolen",
					},
				},
				i:               0,
				apiserverConfig: nil,
				ssCache:         &scrapesSecretsCache{},
			},
			want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: endpointslices
  namespaces:
    names:
    - default
honor_labels: false
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_endpointslice_port_name
  regex: "8080"
- source_labels:
  - __meta_kubernetes_endpointslice_address_target_kind
  - __meta_kubernetes_endpointslice_address_target_name
  separator: ;
  regex: Node;(.*)
  replacement: ${1}
  target_label: node
- source_labels:
  - __meta_kubernetes_endpointslice_address_target_kind
  - __meta_kubernetes_endpointslice_address_target_name
  separator: ;
  regex: Pod;(.*)
  replacement: ${1}
  target_label: pod
- source_labels:
  - __meta_kubernetes_pod_name
  target_label: pod
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_service_name
  target_label: service
- source_labels:
  - __meta_kubernetes_service_name
  target_label: job
  replacement: ${1}
- target_label: endpoint
  replacement: "8080"
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
`,
		},
		{
			name: "config with discovery role services",
			args: args{
				m: &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						DiscoveryRole: kubernetesSDRoleService,
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "8080",
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CA: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-secret",
												},
												Key: "ca",
											},
										},
									},
									BearerTokenFile: "/var/run/tolen",
								},
							},
						},
					},
				},
				ep: vmv1beta1.Endpoint{
					AttachMetadata: vmv1beta1.AttachMetadata{
						Node: ptr.To(true),
					},
					Port: "8080",
					EndpointAuth: vmv1beta1.EndpointAuth{
						TLSConfig: &vmv1beta1.TLSConfig{
							Cert: vmv1beta1.SecretOrConfigMap{},
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "ca",
								},
							},
						},
						BearerTokenFile: "/var/run/tolen",
					},
				},
				i:               0,
				apiserverConfig: nil,
				ssCache:         &scrapesSecretsCache{},
			},
			want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: service
  namespaces:
    names:
    - default
honor_labels: false
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_service_port_name
  regex: "8080"
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_service_name
  target_label: service
- source_labels:
  - __meta_kubernetes_service_name
  target_label: job
  replacement: ${1}
- target_label: endpoint
  replacement: "8080"
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
`,
		},
		{
			name: "bad discovery role service without port name",
			args: args{
				m: &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						DiscoveryRole: kubernetesSDRoleService,
						Endpoints: []vmv1beta1.Endpoint{
							{
								TargetPort: func() *intstr.IntOrString {
									v := intstr.FromString("8080")
									return &v
								}(),
							},
						},
					},
				},
				ep: vmv1beta1.Endpoint{
					TargetPort: func() *intstr.IntOrString {
						v := intstr.FromString("8080")
						return &v
					}(),
				},
				i:               0,
				apiserverConfig: nil,
				ssCache:         &scrapesSecretsCache{},
			},
			want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: service
  namespaces:
    names:
    - default
honor_labels: false
relabel_configs:
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_service_name
  target_label: service
- source_labels:
  - __meta_kubernetes_service_name
  target_label: job
  replacement: ${1}
`,
		},
		{
			name: "config with tls insecure",
			args: args{
				m: &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						DiscoveryRole: kubernetesSDRoleService,
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "8080",
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										InsecureSkipVerify: true,
									},
								},
							},
						},
					},
				},
				ep: vmv1beta1.Endpoint{
					Port: "8080",
					EndpointAuth: vmv1beta1.EndpointAuth{
						TLSConfig: &vmv1beta1.TLSConfig{
							InsecureSkipVerify: true,
						},
						BearerTokenFile: "/var/run/tolen",
					},
				},
				i:               0,
				apiserverConfig: nil,
				ssCache:         &scrapesSecretsCache{},
			},
			want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: service
  namespaces:
    names:
    - default
honor_labels: false
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_service_port_name
  regex: "8080"
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_service_name
  target_label: service
- source_labels:
  - __meta_kubernetes_service_name
  target_label: job
  replacement: ${1}
- target_label: endpoint
  replacement: "8080"
tls_config:
  insecure_skip_verify: true
bearer_token_file: /var/run/tolen
`,
		},
		{
			name: "complete config",
			args: args{
				m: &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						DiscoveryRole: kubernetesSDRoleService,
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "8080",
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										InsecureSkipVerify: true,
									},
								},
							},
						},
					},
				},
				ep: vmv1beta1.Endpoint{
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Params:          map[string][]string{"module": {"base"}},
						ScrapeInterval:  "10s",
						ScrapeTimeout:   "5s",
						HonorTimestamps: ptr.To(true),
						FollowRedirects: ptr.To(true),
						ProxyURL:        ptr.To("https://some-proxy"),
						HonorLabels:     true,
						Scheme:          "https",
						Path:            "/metrics",

						VMScrapeParams: &vmv1beta1.VMScrapeParams{
							StreamParse: ptr.To(true),
							ProxyClientConfig: &vmv1beta1.ProxyAuth{
								TLSConfig:       &vmv1beta1.TLSConfig{InsecureSkipVerify: true},
								BearerTokenFile: "/tmp/some-file",
							},
						},
					},
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{},
						RelabelConfigs:       []*vmv1beta1.RelabelConfig{},
					},
					EndpointAuth: vmv1beta1.EndpointAuth{
						OAuth2: &vmv1beta1.OAuth2{
							Scopes:         []string{"scope-1"},
							TokenURL:       "http://some-token-url",
							EndpointParams: map[string]string{"timeout": "5s"},
							ClientID: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									Key:                  "bearer",
									LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
								},
							},
							ClientSecret: &corev1.SecretKeySelector{
								Key:                  "bearer",
								LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
							},
						},
						BasicAuth: &vmv1beta1.BasicAuth{
							Username: corev1.SecretKeySelector{
								Key:                  "bearer",
								LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
							},
							Password: corev1.SecretKeySelector{
								Key:                  "bearer",
								LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
							},
						},
						TLSConfig: &vmv1beta1.TLSConfig{
							InsecureSkipVerify: true,
						},
						BearerTokenFile: "/var/run/tolen",
						BearerTokenSecret: &corev1.SecretKeySelector{
							Key:                  "bearer",
							LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
						},
					},
					Port: "8080",
				},
				i:               0,
				apiserverConfig: nil,
				ssCache: &scrapesSecretsCache{
					baSecrets: map[string]*k8stools.BasicAuthCredentials{
						"serviceScrape/default/test-scrape/0": {
							Username: "user",
							Password: "pass",
						},
					},
					bearerTokens: map[string]string{},
					oauth2Secrets: map[string]*k8stools.OAuthCreds{
						"serviceScrape/default/test-scrape/0": {ClientSecret: "some-secret", ClientID: "some-id"},
					},
				},
			},
			want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: service
  namespaces:
    names:
    - default
honor_labels: true
honor_timestamps: true
scrape_interval: 10s
scrape_timeout: 5s
metrics_path: /metrics
proxy_url: https://some-proxy
follow_redirects: true
params:
  module:
  - base
scheme: https
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_service_port_name
  regex: "8080"
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_service_name
  target_label: service
- source_labels:
  - __meta_kubernetes_service_name
  target_label: job
  replacement: ${1}
- target_label: endpoint
  replacement: "8080"
stream_parse: true
proxy_tls_config:
  insecure_skip_verify: true
proxy_bearer_token_file: /tmp/some-file
tls_config:
  insecure_skip_verify: true
bearer_token_file: /var/run/tolen
basic_auth:
  username: user
  password: pass
oauth2:
  client_id: some-id
  client_secret: some-secret
  scopes:
  - scope-1
  endpoint_params:
    timeout: 5s
  token_url: http://some-token-url
`,
		},
		{
			name: "with templateRelabel",
			args: args{
				cr: vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeRelabelTemplate: []*vmv1beta1.RelabelConfig{
							{
								TargetLabel:  "node",
								SourceLabels: []string{"__meta_kubernetes_node_name"},
								Regex:        []string{".+"},
							},
						},
					},
				},
				m: &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "8080",
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CA: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-secret",
												},
												Key: "ca",
											},
										},
									},
									BearerTokenFile: "/var/run/tolen",
								},
							},
						},
					},
				},
				ep: vmv1beta1.Endpoint{
					Port: "8080",
					EndpointAuth: vmv1beta1.EndpointAuth{
						TLSConfig: &vmv1beta1.TLSConfig{
							Cert: vmv1beta1.SecretOrConfigMap{},
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "ca",
								},
							},
						},
						BearerTokenFile: "/var/run/tolen",
					},
				},
				i:               0,
				apiserverConfig: nil,
				ssCache:         &scrapesSecretsCache{},
			},
			want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: endpoints
  namespaces:
    names:
    - default
honor_labels: false
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_endpoint_port_name
  regex: "8080"
- source_labels:
  - __meta_kubernetes_endpoint_address_target_kind
  - __meta_kubernetes_endpoint_address_target_name
  separator: ;
  regex: Node;(.*)
  replacement: ${1}
  target_label: node
- source_labels:
  - __meta_kubernetes_endpoint_address_target_kind
  - __meta_kubernetes_endpoint_address_target_name
  separator: ;
  regex: Pod;(.*)
  replacement: ${1}
  target_label: pod
- source_labels:
  - __meta_kubernetes_pod_name
  target_label: pod
- source_labels:
  - __meta_kubernetes_pod_container_name
  target_label: container
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_service_name
  target_label: service
- source_labels:
  - __meta_kubernetes_service_name
  target_label: job
  replacement: ${1}
- target_label: endpoint
  replacement: "8080"
- source_labels:
  - __meta_kubernetes_node_name
  target_label: node
  regex: .+
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateServiceScrapeConfig(context.Background(), &tt.args.cr, tt.args.m, tt.args.ep, tt.args.i, tt.args.apiserverConfig, tt.args.ssCache, tt.args.se)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal ServiceScrapeConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))
		})
	}
}
