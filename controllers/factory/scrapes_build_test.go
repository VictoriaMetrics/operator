package factory

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

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
			got := addTLStoYaml(tt.args.cfg, tt.args.namespace, tt.args.tls)
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
		m                        *victoriametricsv1beta1.VMServiceScrape
		ep                       victoriametricsv1beta1.Endpoint
		i                        int
		apiserverConfig          *victoriametricsv1beta1.APIServerConfig
		basicAuthSecrets         map[string]BasicAuthCredentials
		bearerTokens             map[string]BearerToken
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
				basicAuthSecrets:         nil,
				bearerTokens:             map[string]BearerToken{},
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
				basicAuthSecrets:         nil,
				bearerTokens:             map[string]BearerToken{},
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
				basicAuthSecrets:         nil,
				bearerTokens:             map[string]BearerToken{},
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
				basicAuthSecrets:         nil,
				bearerTokens:             map[string]BearerToken{},
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateServiceScrapeConfig(tt.args.m, tt.args.ep, tt.args.i, tt.args.apiserverConfig, tt.args.basicAuthSecrets, tt.args.bearerTokens, tt.args.overrideHonorLabels, tt.args.overrideHonorTimestamps, tt.args.ignoreNamespaceSelectors, tt.args.enforcedNamespaceLabel)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal ServiceScrapeConfig to yaml,err :%e", err)
				return
			}
			if !reflect.DeepEqual(string(gotBytes), tt.want) {
				t.Errorf("generateServiceScrapeConfig() \ngot = \n%v, \nwant \n%v", string(gotBytes), tt.want)
			}
		})
	}
}

func Test_generateNodeScrapeConfig(t *testing.T) {
	type args struct {
		m                       *victoriametricsv1beta1.VMNodeScrape
		i                       int
		apiserverConfig         *victoriametricsv1beta1.APIServerConfig
		basicAuthSecrets        map[string]BasicAuthCredentials
		bearerTokens            map[string]BearerToken
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
				apiserverConfig:  nil,
				basicAuthSecrets: nil,
				i:                1,
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
  - __meta_kubernetes_node_address_InternalIP
  target_label: __address__
- source_labels:
  - __meta_kubernetes_node_name
  target_label: node
- target_label: job
  replacement: default/nodes-basic
- source_labels:
  - __address__
  target_label: __address__
  regex: (.*)
  replacement: ${1}:9100
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateNodeScrapeConfig(tt.args.m, tt.args.i, tt.args.apiserverConfig, tt.args.basicAuthSecrets, tt.args.bearerTokens, tt.args.ignoreHonorLabels, tt.args.overrideHonorTimestamps, tt.args.enforcedNamespaceLabel)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal NodeScrapeConfig to yaml,err :%e", err)
				return
			}
			if !reflect.DeepEqual(string(gotBytes), tt.want) {
				t.Errorf("generateNoeScrapeConfig() \ngot = \n%v, \nwant \n%v", string(gotBytes), tt.want)
			}

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
			if !reflect.DeepEqual(string(gotBytes), tt.want) {
				t.Errorf("generateRelabelConfig() \ngot = \n%v, \nwant \n%v", string(gotBytes), tt.want)
			}
		})
	}
}
