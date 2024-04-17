package vmagent

import (
	"context"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

func Test_generateServiceScrapeConfig(t *testing.T) {
	type args struct {
		cr                       victoriametricsv1beta1.VMAgent
		m                        *victoriametricsv1beta1.VMServiceScrape
		ep                       victoriametricsv1beta1.Endpoint
		i                        int
		apiserverConfig          *victoriametricsv1beta1.APIServerConfig
		ssCache                  *scrapesSecretsCache
		overrideHonorLabels      bool
		overrideHonorTimestamps  bool
		ignoreNamespaceSelectors bool
		enforcedNamespaceLabel   string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "generate simple config",
			args: args{
				m: &victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Port: "8080",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{
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
				ep: victoriametricsv1beta1.Endpoint{
					AttachMetadata: victoriametricsv1beta1.AttachMetadata{
						Node: pointer.Bool(true),
					},
					Port: "8080",
					TLSConfig: &victoriametricsv1beta1.TLSConfig{
						Cert: victoriametricsv1beta1.SecretOrConfigMap{},
						CA: victoriametricsv1beta1.SecretOrConfigMap{
							Secret: &v1.SecretKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "tls-secret",
								},
								Key: "ca",
							},
						},
					},
					BearerTokenFile: "/var/run/tolen",
				},
				i:                        0,
				apiserverConfig:          nil,
				ssCache:                  &scrapesSecretsCache{},
				overrideHonorLabels:      false,
				overrideHonorTimestamps:  false,
				ignoreNamespaceSelectors: false,
				enforcedNamespaceLabel:   "",
			},
			want: `job_name: serviceScrape/default/test-scrape/0
honor_labels: false
kubernetes_sd_configs:
- role: endpoints
  attach_metadata:
    node: true
  namespaces:
    names:
    - default
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
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
`,
		},

		{
			name: "generate config with scrape interval limit",
			args: args{
				cr: victoriametricsv1beta1.VMAgent{
					Spec: victoriametricsv1beta1.VMAgentSpec{
						MaxScrapeInterval: pointer.String("40m"),
					},
				},
				m: &victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Port: "8080",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{
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
				ep: victoriametricsv1beta1.Endpoint{
					Port: "8080",
					TLSConfig: &victoriametricsv1beta1.TLSConfig{
						Cert: victoriametricsv1beta1.SecretOrConfigMap{},
						CA: victoriametricsv1beta1.SecretOrConfigMap{
							Secret: &v1.SecretKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "tls-secret",
								},
								Key: "ca",
							},
						},
					},
					BearerTokenFile: "/var/run/tolen",
					ScrapeInterval:  "60m",
				},
				i:                        0,
				apiserverConfig:          nil,
				ssCache:                  &scrapesSecretsCache{},
				overrideHonorLabels:      false,
				overrideHonorTimestamps:  false,
				ignoreNamespaceSelectors: false,
				enforcedNamespaceLabel:   "",
			},
			want: `job_name: serviceScrape/default/test-scrape/0
honor_labels: false
kubernetes_sd_configs:
- role: endpoints
  namespaces:
    names:
    - default
scrape_interval: 40m
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
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
`,
		},
		{
			name: "generate config with scrape interval limit - reach min",
			args: args{
				cr: victoriametricsv1beta1.VMAgent{
					Spec: victoriametricsv1beta1.VMAgentSpec{
						MinScrapeInterval: pointer.String("1m"),
					},
				},
				m: &victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Port: "8080",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{
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
				ep: victoriametricsv1beta1.Endpoint{
					Port: "8080",
					TLSConfig: &victoriametricsv1beta1.TLSConfig{
						Cert: victoriametricsv1beta1.SecretOrConfigMap{},
						CA: victoriametricsv1beta1.SecretOrConfigMap{
							Secret: &v1.SecretKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "tls-secret",
								},
								Key: "ca",
							},
						},
					},
					BearerTokenFile: "/var/run/tolen",
					ScrapeInterval:  "10s",
				},
				i:                        0,
				apiserverConfig:          nil,
				ssCache:                  &scrapesSecretsCache{},
				overrideHonorLabels:      false,
				overrideHonorTimestamps:  false,
				ignoreNamespaceSelectors: false,
				enforcedNamespaceLabel:   "",
			},
			want: `job_name: serviceScrape/default/test-scrape/0
honor_labels: false
kubernetes_sd_configs:
- role: endpoints
  namespaces:
    names:
    - default
scrape_interval: 1m
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
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
`,
		},
		{
			name: "config with discovery role endpointslices",
			args: args{
				m: &victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						DiscoveryRole: kubernetesSDRoleEndpointSlices,
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Port: "8080",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{
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
				ep: victoriametricsv1beta1.Endpoint{
					Port: "8080",
					TLSConfig: &victoriametricsv1beta1.TLSConfig{
						Cert: victoriametricsv1beta1.SecretOrConfigMap{},
						CA: victoriametricsv1beta1.SecretOrConfigMap{
							Secret: &v1.SecretKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "tls-secret",
								},
								Key: "ca",
							},
						},
					},
					BearerTokenFile: "/var/run/tolen",
				},
				i:                        0,
				apiserverConfig:          nil,
				ssCache:                  &scrapesSecretsCache{},
				overrideHonorLabels:      false,
				overrideHonorTimestamps:  false,
				ignoreNamespaceSelectors: false,
				enforcedNamespaceLabel:   "",
			},
			want: `job_name: serviceScrape/default/test-scrape/0
honor_labels: false
kubernetes_sd_configs:
- role: endpointslices
  namespaces:
    names:
    - default
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
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
`,
		},
		{
			name: "config with discovery role services",
			args: args{
				m: &victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						DiscoveryRole: kubernetesSDRoleService,
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Port: "8080",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{
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
				ep: victoriametricsv1beta1.Endpoint{
					AttachMetadata: victoriametricsv1beta1.AttachMetadata{
						Node: pointer.Bool(true),
					},
					Port: "8080",
					TLSConfig: &victoriametricsv1beta1.TLSConfig{
						Cert: victoriametricsv1beta1.SecretOrConfigMap{},
						CA: victoriametricsv1beta1.SecretOrConfigMap{
							Secret: &v1.SecretKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "tls-secret",
								},
								Key: "ca",
							},
						},
					},
					BearerTokenFile: "/var/run/tolen",
				},
				i:                        0,
				apiserverConfig:          nil,
				ssCache:                  &scrapesSecretsCache{},
				overrideHonorLabels:      false,
				overrideHonorTimestamps:  false,
				ignoreNamespaceSelectors: false,
				enforcedNamespaceLabel:   "",
			},
			want: `job_name: serviceScrape/default/test-scrape/0
honor_labels: false
kubernetes_sd_configs:
- role: service
  namespaces:
    names:
    - default
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
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
`,
		},
		{
			name: "bad discovery role service without port name",
			args: args{
				m: &victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						DiscoveryRole: kubernetesSDRoleService,
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								TargetPort: func() *intstr.IntOrString {
									v := intstr.FromString("8080")
									return &v
								}(),
							},
						},
					},
				},
				ep: victoriametricsv1beta1.Endpoint{
					TargetPort: func() *intstr.IntOrString {
						v := intstr.FromString("8080")
						return &v
					}(),
				},
				i:                        0,
				apiserverConfig:          nil,
				ssCache:                  &scrapesSecretsCache{},
				overrideHonorLabels:      false,
				overrideHonorTimestamps:  false,
				ignoreNamespaceSelectors: false,
				enforcedNamespaceLabel:   "",
			},
			want: `job_name: serviceScrape/default/test-scrape/0
honor_labels: false
kubernetes_sd_configs:
- role: service
  namespaces:
    names:
    - default
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
				m: &victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						DiscoveryRole: kubernetesSDRoleService,
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Port: "8080",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
							},
						},
					},
				},
				ep: victoriametricsv1beta1.Endpoint{
					Port: "8080",
					TLSConfig: &victoriametricsv1beta1.TLSConfig{
						InsecureSkipVerify: true,
					},
					BearerTokenFile: "/var/run/tolen",
				},
				i:                        0,
				apiserverConfig:          nil,
				ssCache:                  &scrapesSecretsCache{},
				overrideHonorLabels:      false,
				overrideHonorTimestamps:  false,
				ignoreNamespaceSelectors: false,
				enforcedNamespaceLabel:   "",
			},
			want: `job_name: serviceScrape/default/test-scrape/0
honor_labels: false
kubernetes_sd_configs:
- role: service
  namespaces:
    names:
    - default
tls_config:
  insecure_skip_verify: true
bearer_token_file: /var/run/tolen
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
`,
		},
		{
			name: "complete config",
			args: args{
				m: &victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						DiscoveryRole: kubernetesSDRoleService,
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Port: "8080",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
							},
						},
					},
				},
				ep: victoriametricsv1beta1.Endpoint{
					VMScrapeParams: &victoriametricsv1beta1.VMScrapeParams{
						StreamParse: pointer.Bool(true),
						ProxyClientConfig: &victoriametricsv1beta1.ProxyAuth{
							TLSConfig:       &victoriametricsv1beta1.TLSConfig{InsecureSkipVerify: true},
							BearerTokenFile: "/tmp/some-file",
						},
					},
					MetricRelabelConfigs: []*victoriametricsv1beta1.RelabelConfig{},
					RelabelConfigs:       []*victoriametricsv1beta1.RelabelConfig{},
					OAuth2: &victoriametricsv1beta1.OAuth2{
						Scopes:         []string{"scope-1"},
						TokenURL:       "http://some-token-url",
						EndpointParams: map[string]string{"timeout": "5s"},
						ClientID: victoriametricsv1beta1.SecretOrConfigMap{
							Secret: &v1.SecretKeySelector{
								Key:                  "bearer",
								LocalObjectReference: v1.LocalObjectReference{Name: "access-secret"},
							},
						},
						ClientSecret: &v1.SecretKeySelector{
							Key:                  "bearer",
							LocalObjectReference: v1.LocalObjectReference{Name: "access-secret"},
						},
					},
					BasicAuth: &victoriametricsv1beta1.BasicAuth{
						Username: v1.SecretKeySelector{
							Key:                  "bearer",
							LocalObjectReference: v1.LocalObjectReference{Name: "access-secret"},
						},
						Password: v1.SecretKeySelector{
							Key:                  "bearer",
							LocalObjectReference: v1.LocalObjectReference{Name: "access-secret"},
						},
					},
					Params:          map[string][]string{"module": {"base"}},
					ScrapeInterval:  "10s",
					ScrapeTimeout:   "5s",
					HonorTimestamps: pointer.Bool(true),
					FollowRedirects: pointer.Bool(true),
					ProxyURL:        pointer.String("https://some-proxy"),
					HonorLabels:     true,
					Scheme:          "https",
					Path:            "/metrics",
					BearerTokenSecret: &v1.SecretKeySelector{
						Key:                  "bearer",
						LocalObjectReference: v1.LocalObjectReference{Name: "access-secret"},
					},
					Port: "8080",
					TLSConfig: &victoriametricsv1beta1.TLSConfig{
						InsecureSkipVerify: true,
					},
					BearerTokenFile: "/var/run/tolen",
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
				overrideHonorLabels:      false,
				overrideHonorTimestamps:  false,
				ignoreNamespaceSelectors: false,
				enforcedNamespaceLabel:   "",
			},
			want: `job_name: serviceScrape/default/test-scrape/0
honor_labels: true
honor_timestamps: true
kubernetes_sd_configs:
- role: service
  namespaces:
    names:
    - default
scrape_interval: 10s
scrape_timeout: 5s
metrics_path: /metrics
proxy_url: https://some-proxy
follow_redirects: true
params:
  module:
  - base
scheme: https
tls_config:
  insecure_skip_verify: true
bearer_token_file: /var/run/tolen
basic_auth:
  username: user
  password: pass
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
metric_relabel_configs: []
stream_parse: true
proxy_tls_config:
  insecure_skip_verify: true
proxy_bearer_token_file: /tmp/some-file
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
				cr: victoriametricsv1beta1.VMAgent{
					Spec: victoriametricsv1beta1.VMAgentSpec{
						ServiceScrapeRelabelTemplate: []*victoriametricsv1beta1.RelabelConfig{
							{
								TargetLabel:  "node",
								SourceLabels: []string{"__meta_kubernetes_node_name"},
								Regex:        []string{".+"},
							},
						},
					},
				},
				m: &victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scrape",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Port: "8080",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{
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
				ep: victoriametricsv1beta1.Endpoint{
					Port: "8080",
					TLSConfig: &victoriametricsv1beta1.TLSConfig{
						Cert: victoriametricsv1beta1.SecretOrConfigMap{},
						CA: victoriametricsv1beta1.SecretOrConfigMap{
							Secret: &v1.SecretKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "tls-secret",
								},
								Key: "ca",
							},
						},
					},
					BearerTokenFile: "/var/run/tolen",
				},
				i:                        0,
				apiserverConfig:          nil,
				ssCache:                  &scrapesSecretsCache{},
				overrideHonorLabels:      false,
				overrideHonorTimestamps:  false,
				ignoreNamespaceSelectors: false,
				enforcedNamespaceLabel:   "",
			},
			want: `job_name: serviceScrape/default/test-scrape/0
honor_labels: false
kubernetes_sd_configs:
- role: endpoints
  namespaces:
    names:
    - default
tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/tolen
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
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateServiceScrapeConfig(context.Background(), &tt.args.cr, tt.args.m, tt.args.ep, tt.args.i, tt.args.apiserverConfig, tt.args.ssCache, tt.args.overrideHonorLabels, tt.args.overrideHonorTimestamps, tt.args.ignoreNamespaceSelectors, tt.args.enforcedNamespaceLabel)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal ServiceScrapeConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))
		})
	}
}
