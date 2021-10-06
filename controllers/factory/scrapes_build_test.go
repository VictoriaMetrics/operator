package factory

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_addTLStoYaml(t *testing.T) {
	type args struct {
		cfg       yaml.MapSlice
		namespace string
		tls       *victoriametricsv1beta1.TLSConfig
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "check ca only added to config",
			args: args{
				namespace: "default",
				cfg:       yaml.MapSlice{},
				tls: &victoriametricsv1beta1.TLSConfig{
					CA: victoriametricsv1beta1.SecretOrConfigMap{
						Secret: &v1.SecretKeySelector{
							Key: "ca",
							LocalObjectReference: v1.LocalObjectReference{
								Name: "tls-secret",
							},
						},
					},
					Cert: victoriametricsv1beta1.SecretOrConfigMap{},
				},
			},
			want: `tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
`,
		},
		{
			name: "check ca,cert and key added to config",
			args: args{
				namespace: "default",
				cfg:       yaml.MapSlice{},
				tls: &victoriametricsv1beta1.TLSConfig{
					CA: victoriametricsv1beta1.SecretOrConfigMap{
						Secret: &v1.SecretKeySelector{
							Key: "ca",
							LocalObjectReference: v1.LocalObjectReference{
								Name: "tls-secret",
							},
						},
					},
					Cert: victoriametricsv1beta1.SecretOrConfigMap{
						Secret: &v1.SecretKeySelector{
							Key: "cert",
							LocalObjectReference: v1.LocalObjectReference{
								Name: "tls-secret",
							},
						},
					},
					KeySecret: &v1.SecretKeySelector{
						Key: "key",
						LocalObjectReference: v1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			want: `tls_config:
  insecure_skip_verify: false
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
  cert_file: /etc/vmagent-tls/certs/default_tls-secret_cert
  key_file: /etc/vmagent-tls/certs/default_tls-secret_key
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addTLStoYaml(tt.args.cfg, tt.args.namespace, tt.args.tls, false)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal tlsConfig to yaml format: %e", err)
				return
			}
			if !reflect.DeepEqual(string(gotBytes), tt.want) {
				t.Errorf("addTLStoYaml() \ngot: \n%v \nwant \n%v", string(gotBytes), tt.want)
			}
		})
	}
}

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
			want: `job_name: default/test-scrape/0
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
			want: `job_name: default/test-scrape/0
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
			want: `job_name: default/test-scrape/0
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
			want: `job_name: default/test-scrape/0
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
			want: `job_name: default/test-scrape/0
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
					Params:          map[string][]string{"module": []string{"base"}},
					ScrapeInterval:  "10s",
					ScrapeTimeout:   "5s",
					HonorTimestamps: pointer.Bool(true),
					FollowRedirects: pointer.Bool(true),
					ProxyURL:        pointer.String("https://some-proxy"),
					HonorLabels:     true,
					Scheme:          "https",
					Path:            "/metrics",
					BearerTokenSecret: v1.SecretKeySelector{
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
					baSecrets: map[string]*BasicAuthCredentials{
						"serviceScrape/default/test-scrape/0": &BasicAuthCredentials{
							username: "user",
							password: "pass",
						},
					},
					bearerTokens: map[string]string{},
					oauth2Secrets: map[string]*oauthCreds{
						"serviceScrape/default/test-scrape/0": &oauthCreds{clientSecret: "some-secret", clientID: "some-id"},
					},
				},
				overrideHonorLabels:      false,
				overrideHonorTimestamps:  false,
				ignoreNamespaceSelectors: false,
				enforcedNamespaceLabel:   "",
			},
			want: `job_name: default/test-scrape/0
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
								Regex:        ".+",
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
			want: `job_name: default/test-scrape/0
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
			got := generateServiceScrapeConfig(&tt.args.cr, tt.args.m, tt.args.ep, tt.args.i, tt.args.apiserverConfig, tt.args.ssCache, tt.args.overrideHonorLabels, tt.args.overrideHonorTimestamps, tt.args.ignoreNamespaceSelectors, tt.args.enforcedNamespaceLabel)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal ServiceScrapeConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))

		})
	}
}

func Test_generateNodeScrapeConfig(t *testing.T) {
	type args struct {
		cr                      victoriametricsv1beta1.VMAgent
		m                       *victoriametricsv1beta1.VMNodeScrape
		i                       int
		apiserverConfig         *victoriametricsv1beta1.APIServerConfig
		ssCache                 *scrapesSecretsCache
		ignoreHonorLabels       bool
		overrideHonorTimestamps bool
		enforcedNamespaceLabel  string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "ok build node",
			args: args{
				apiserverConfig: nil,
				ssCache:         &scrapesSecretsCache{},
				i:               1,
				m: &victoriametricsv1beta1.VMNodeScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nodes-basic",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMNodeScrapeSpec{
						Port:     "9100",
						Path:     "/metrics",
						Interval: "30s",
					},
				},
			},
			want: `job_name: default/nodes-basic/1
honor_labels: false
kubernetes_sd_configs:
- role: node
scrape_interval: 30s
metrics_path: /metrics
relabel_configs:
- source_labels:
  - __meta_kubernetes_node_name
  target_label: node
- target_label: job
  replacement: default/nodes-basic
- source_labels:
  - __address__
  target_label: __address__
  regex: ^(.*):(.*)
  replacement: ${1}:9100
`,
		},
		{
			name: "complete ok build node",
			args: args{
				apiserverConfig: nil,
				ssCache: &scrapesSecretsCache{
					oauth2Secrets: map[string]*oauthCreds{},
					bearerTokens:  map[string]string{},
					baSecrets: map[string]*BasicAuthCredentials{
						"nodeScrape/default/nodes-basic": &BasicAuthCredentials{
							username: "username",
						},
					},
				},
				i: 1,
				m: &victoriametricsv1beta1.VMNodeScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nodes-basic",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMNodeScrapeSpec{
						Port:            "9100",
						Path:            "/metrics",
						Interval:        "30s",
						Scheme:          "https",
						HonorLabels:     true,
						ProxyURL:        pointer.String("https://some-url"),
						SampleLimit:     50,
						FollowRedirects: pointer.Bool(true),
						ScrapeTimeout:   "10s",
						ScrapeInterval:  "5s",
						Params:          map[string][]string{"module": []string{"client"}},
						JobLabel:        "env",
						HonorTimestamps: pointer.Bool(true),
						TargetLabels:    []string{"app", "env"},
						BearerTokenFile: "/tmp/bearer",
						BasicAuth: &victoriametricsv1beta1.BasicAuth{
							Username: v1.SecretKeySelector{Key: "username", LocalObjectReference: v1.LocalObjectReference{Name: "ba-secret"}},
						},
						TLSConfig: &victoriametricsv1beta1.TLSConfig{
							InsecureSkipVerify: true,
						},
						OAuth2: &victoriametricsv1beta1.OAuth2{},
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"job": "prod"},
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{Key: "external", Operator: metav1.LabelSelectorOpIn, Values: []string{"world"}},
							},
						},
						VMScrapeParams: &victoriametricsv1beta1.VMScrapeParams{

							StreamParse: pointer.Bool(true),
							ProxyClientConfig: &victoriametricsv1beta1.ProxyAuth{
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
								BearerTokenFile: "/tmp/proxy-token",
							},
						},
						RelabelConfigs:       []*victoriametricsv1beta1.RelabelConfig{},
						MetricRelabelConfigs: []*victoriametricsv1beta1.RelabelConfig{},
					},
				},
			},
			want: `job_name: default/nodes-basic/1
honor_labels: true
honor_timestamps: true
kubernetes_sd_configs:
- role: node
scrape_interval: 5s
scrape_timeout: 10s
metrics_path: /metrics
proxy_url: https://some-url
sample_limit: 50
params:
  module:
  - client
follow_redirects: true
scheme: https
tls_config:
  insecure_skip_verify: true
bearer_token_file: /tmp/bearer
basic_auth:
  username: username
  password: ""
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_node_label_job
  regex: prod
- action: keep
  source_labels:
  - __meta_kubernetes_node_label_external
  regex: world
- source_labels:
  - __meta_kubernetes_node_name
  target_label: node
- source_labels:
  - __meta_kubernetes_node_label_app
  target_label: app
  regex: (.+)
  replacement: ${1}
- source_labels:
  - __meta_kubernetes_node_label_env
  target_label: env
  regex: (.+)
  replacement: ${1}
- target_label: job
  replacement: default/nodes-basic
- source_labels:
  - __meta_kubernetes_node_label_env
  target_label: job
  regex: (.+)
  replacement: ${1}
- source_labels:
  - __address__
  target_label: __address__
  regex: ^(.*):(.*)
  replacement: ${1}:9100
sample_limit: 50
metric_relabel_configs: []
stream_parse: true
proxy_tls_config:
  insecure_skip_verify: true
proxy_bearer_token_file: /tmp/proxy-token
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateNodeScrapeConfig(&tt.args.cr, tt.args.m, tt.args.i, tt.args.apiserverConfig, tt.args.ssCache, tt.args.ignoreHonorLabels, tt.args.overrideHonorTimestamps, tt.args.enforcedNamespaceLabel)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal NodeScrapeConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))

		})
	}
}

func Test_generateRelabelConfig(t *testing.T) {
	type args struct {
		rc *victoriametricsv1beta1.RelabelConfig
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "ok base cfg",
			args: args{rc: &victoriametricsv1beta1.RelabelConfig{
				TargetLabel:  "address",
				SourceLabels: []string{"__address__"},
				Action:       "replace",
			}},
			want: `source_labels:
- __address__
target_label: address
action: replace
`,
		},
		{
			name: "ok base with underscore",
			args: args{rc: &victoriametricsv1beta1.RelabelConfig{
				UnderScoreTargetLabel:  "address",
				UnderScoreSourceLabels: []string{"__address__"},
				Action:                 "replace",
			}},
			want: `source_labels:
- __address__
target_label: address
action: replace
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateRelabelConfig(tt.args.rc)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal generateRelabelConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))
		})
	}
}

func Test_generatePodScrapeConfig(t *testing.T) {
	type args struct {
		cr                       victoriametricsv1beta1.VMAgent
		m                        *victoriametricsv1beta1.VMPodScrape
		ep                       victoriametricsv1beta1.PodMetricsEndpoint
		i                        int
		apiserverConfig          *victoriametricsv1beta1.APIServerConfig
		ssCache                  *scrapesSecretsCache
		ignoreHonorLabels        bool
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
			name: "simple test",
			args: args{
				m: &victoriametricsv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-1",
						Namespace: "default",
					},
				},
				ep: victoriametricsv1beta1.PodMetricsEndpoint{
					Path: "/metric",
					Port: "web",
				},
				ssCache: &scrapesSecretsCache{},
			},
			want: `job_name: default/test-1/0
honor_labels: false
kubernetes_sd_configs:
- role: pod
  namespaces:
    names:
    - default
metrics_path: /metric
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_pod_container_port_name
  regex: web
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_pod_container_name
  target_label: container
- source_labels:
  - __meta_kubernetes_pod_name
  target_label: pod
- target_label: job
  replacement: default/test-1
- target_label: endpoint
  replacement: web
`,
		},
		{
			name: "test with selector",
			args: args{
				m: &victoriametricsv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMPodScrapeSpec{
						NamespaceSelector: victoriametricsv1beta1.NamespaceSelector{
							Any: true,
						},
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"label-1": "value-1",
								"label-2": "value-2",
								"label-3": "value-3",
							},
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "some-label",
									Operator: metav1.LabelSelectorOpExists,
								},
							},
						},
					},
				},
				ep: victoriametricsv1beta1.PodMetricsEndpoint{
					Path: "/metric",
					Port: "web",
				},
				ssCache: &scrapesSecretsCache{},
			},
			want: `job_name: default/test-1/0
honor_labels: false
kubernetes_sd_configs:
- role: pod
  selectors:
  - role: pod
    label: label-1=value-1,label-2=value-2,label-3=value-3
metrics_path: /metric
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_pod_label_label_1
  regex: value-1
- action: keep
  source_labels:
  - __meta_kubernetes_pod_label_label_2
  regex: value-2
- action: keep
  source_labels:
  - __meta_kubernetes_pod_label_label_3
  regex: value-3
- action: keep
  source_labels:
  - __meta_kubernetes_pod_label_some_label
  regex: .+
- action: keep
  source_labels:
  - __meta_kubernetes_pod_container_port_name
  regex: web
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_pod_container_name
  target_label: container
- source_labels:
  - __meta_kubernetes_pod_name
  target_label: pod
- target_label: job
  replacement: default/test-1
- target_label: endpoint
  replacement: web
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generatePodScrapeConfig(&tt.args.cr, tt.args.m, tt.args.ep, tt.args.i, tt.args.apiserverConfig, tt.args.ssCache, tt.args.ignoreHonorLabels, tt.args.overrideHonorTimestamps, tt.args.ignoreNamespaceSelectors, tt.args.enforcedNamespaceLabel)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal PodScrapeConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))

		})
	}
}
