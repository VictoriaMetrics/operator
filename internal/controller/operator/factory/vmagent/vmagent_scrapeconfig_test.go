package vmagent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_generateRelabelConfig(t *testing.T) {
	tests := []struct {
		name string
		rc   *vmv1beta1.RelabelConfig
		want string
	}{
		{
			name: "ok base cfg",
			rc: &vmv1beta1.RelabelConfig{
				TargetLabel:  "address",
				SourceLabels: []string{"__address__"},
				Action:       "replace",
			},
			want: `source_labels:
- __address__
target_label: address
action: replace
`,
		},
		{
			name: "ok base with underscore",
			rc: &vmv1beta1.RelabelConfig{
				UnderScoreTargetLabel:  "address",
				UnderScoreSourceLabels: []string{"__address__"},
				Action:                 "replace",
			},
			want: `source_labels:
- __address__
target_label: address
action: replace
`,
		},
		{
			name: "ok base with graphite match labels",
			rc: &vmv1beta1.RelabelConfig{
				UnderScoreTargetLabel:  "address",
				UnderScoreSourceLabels: []string{"__address__"},
				Action:                 "graphite",
				Labels:                 map[string]string{"job": "$1", "instance": "${2}:8080"},
				Match:                  `foo.*.*.bar`,
			},
			want: `source_labels:
- __address__
target_label: address
action: graphite
match: foo.*.*.bar
labels:
  instance: ${2}:8080
  job: $1
`,
		},
		{
			name: "with empty replacement and separator",
			rc: &vmv1beta1.RelabelConfig{
				UnderScoreTargetLabel:  "address",
				UnderScoreSourceLabels: []string{"__address__"},
				Action:                 "graphite",
				Labels:                 map[string]string{"job": "$1", "instance": "${2}:8080"},
				Match:                  `foo.*.*.bar`,
				Separator:              ptr.To(""),
				Replacement:            ptr.To(""),
			},
			want: `source_labels:
- __address__
separator: ""
target_label: address
replacement: ""
action: graphite
match: foo.*.*.bar
labels:
  instance: ${2}:8080
  job: $1
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// related fields only filled during json unmarshal
			j, err := json.Marshal(tt.rc)
			if err != nil {
				t.Fatalf("cannot serialize relabelConfig : %s", err)
			}
			var rlbCfg vmv1beta1.RelabelConfig
			if err := json.Unmarshal(j, &rlbCfg); err != nil {
				t.Fatalf("cannot parse relabelConfig : %s", err)
			}
			got := generateRelabelConfig(&rlbCfg)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal generateRelabelConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))
		})
	}
}

func TestCreateOrUpdateConfigurationSecret(t *testing.T) {
	tests := []struct {
		name              string
		cr                *vmv1beta1.VMAgent
		c                 *config.BaseOperatorConf
		predefinedObjects []runtime.Object
		wantConfig        string
		wantErr           bool
	}{
		{
			name: "complete test",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAgentSpec{
					ServiceScrapeNamespaceSelector: &metav1.LabelSelector{},
					ServiceScrapeSelector:          &metav1.LabelSelector{},
					PodScrapeSelector:              &metav1.LabelSelector{},
					PodScrapeNamespaceSelector:     &metav1.LabelSelector{},
					NodeScrapeNamespaceSelector:    &metav1.LabelSelector{},
					NodeScrapeSelector:             &metav1.LabelSelector{},
					StaticScrapeNamespaceSelector:  &metav1.LabelSelector{},
					StaticScrapeSelector:           &metav1.LabelSelector{},
					ProbeNamespaceSelector:         &metav1.LabelSelector{},
					ProbeSelector:                  &metav1.LabelSelector{},
				},
			},
			c: config.MustGetBaseConfig(),
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kube-system",
					},
				},
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vms",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Selector:          metav1.LabelSelector{},
						JobLabel:          "app",
						NamespaceSelector: vmv1beta1.NamespaceSelector{},
						Endpoints: []vmv1beta1.Endpoint{
							{
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics",
								},
								Port: "8085",
								EndpointAuth: vmv1beta1.EndpointAuth{
									BearerTokenSecret: &corev1.SecretKeySelector{
										Key: "bearer",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "access-creds",
										},
									},
								},
							},
							{
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics-2",
								},
								Port: "8083",
							},
						},
					},
				},
				&vmv1beta1.VMProbe{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kube-system",
						Name:      "test-vmp",
					},
					Spec: vmv1beta1.VMProbeSpec{
						Targets: vmv1beta1.VMProbeTargets{
							StaticConfig: &vmv1beta1.VMProbeTargetStaticConfig{
								Targets: []string{"localhost:8428"},
							},
						},
						VMProberSpec: vmv1beta1.VMProberSpec{URL: "http://blackbox"},
					},
				},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vps",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{
						JobLabel:          "app",
						NamespaceSelector: vmv1beta1.NamespaceSelector{},
						Selector: metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"prod"},
								},
							},
						},
						SampleLimit: 10,
						PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
							{
								Port: ptr.To("805"),
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics-3",

									VMScrapeParams: &vmv1beta1.VMScrapeParams{
										StreamParse: ptr.To(true),
										ProxyClientConfig: &vmv1beta1.ProxyAuth{
											TLSConfig: &vmv1beta1.TLSConfig{
												InsecureSkipVerify: true,
												KeySecret: &corev1.SecretKeySelector{
													Key: "key",
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "access-creds",
													},
												},
												Cert: vmv1beta1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{
													Key: "cert",
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "access-creds",
													},
												}},
												CA: vmv1beta1.SecretOrConfigMap{
													Secret: &corev1.SecretKeySelector{
														Key: "ca",
														LocalObjectReference: corev1.LocalObjectReference{
															Name: "access-creds",
														},
													},
												},
											},
										},
									},
								},
							},
							{
								Port: ptr.To("801"),
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics-5",
								},
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										InsecureSkipVerify: true,
										KeySecret: &corev1.SecretKeySelector{
											Key: "key",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "access-creds",
											},
										},
										Cert: vmv1beta1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{
											Key: "cert",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "access-creds",
											},
										}},
										CA: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												Key: "ca",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "access-creds",
												},
											},
										},
									},
								},
							},
						},
					},
				},
				&vmv1beta1.VMNodeScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vms",
					},
					Spec: vmv1beta1.VMNodeScrapeSpec{
						EndpointAuth: vmv1beta1.EndpointAuth{
							BasicAuth: &vmv1beta1.BasicAuth{
								Username: corev1.SecretKeySelector{
									Key: "username",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "access-creds",
									},
								},
								Password: corev1.SecretKeySelector{
									Key: "password",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "access-creds",
									},
								},
							},
						},
					},
				},
				&vmv1beta1.VMStaticScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vmstatic",
					},
					Spec: vmv1beta1.VMStaticScrapeSpec{
						TargetEndpoints: []*vmv1beta1.TargetEndpoint{
							{
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path:     "/metrics-3",
									Scheme:   "https",
									ProxyURL: ptr.To("https://some-proxy-1"),
								},
								EndpointAuth: vmv1beta1.EndpointAuth{
									OAuth2: &vmv1beta1.OAuth2{
										TokenURL: "https://some-tr",
										ClientSecret: &corev1.SecretKeySelector{
											Key: "cs",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "access-creds",
											},
										},
										ClientID: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												Key: "cid",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "access-creds",
												},
											},
										},
									},
								},
							},
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "access-creds",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"cid":      []byte(`some-client-id`),
						"cs":       []byte(`some-client-secret`),
						"username": []byte(`some-username`),
						"password": []byte(`some-password`),
						"ca":       []byte(`some-ca-cert`),
						"cert":     []byte(`some-cert`),
						"key":      []byte(`some-key`),
						"bearer":   []byte(`some-bearer`),
					},
				},
			},
			wantConfig: `global:
  scrape_interval: 30s
  external_labels:
    prometheus: default/test
scrape_configs:
- job_name: serviceScrape/default/test-vms/0
  kubernetes_sd_configs:
  - role: endpoints
    namespaces:
      names:
      - default
  honor_labels: false
  metrics_path: /metrics
  relabel_configs:
  - action: keep
    source_labels:
    - __meta_kubernetes_endpoint_port_name
    regex: "8085"
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
  - source_labels:
    - __meta_kubernetes_service_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "8085"
  bearer_token: some-bearer
- job_name: serviceScrape/default/test-vms/1
  kubernetes_sd_configs:
  - role: endpoints
    namespaces:
      names:
      - default
  honor_labels: false
  metrics_path: /metrics-2
  relabel_configs:
  - action: keep
    source_labels:
    - __meta_kubernetes_endpoint_port_name
    regex: "8083"
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
  - source_labels:
    - __meta_kubernetes_service_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "8083"
- job_name: podScrape/default/test-vps/0
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names:
      - default
  honor_labels: false
  metrics_path: /metrics-3
  sample_limit: 10
  relabel_configs:
  - action: drop
    source_labels:
    - __meta_kubernetes_pod_phase
    regex: (Failed|Succeeded)
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_label_app
    regex: prod
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_container_port_name
    regex: "805"
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
    replacement: default/test-vps
  - source_labels:
    - __meta_kubernetes_pod_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "805"
  stream_parse: true
  proxy_tls_config:
    insecure_skip_verify: true
    ca_file: /etc/vmagent-tls/certs/default_access-creds_ca
    cert_file: /etc/vmagent-tls/certs/default_access-creds_cert
    key_file: /etc/vmagent-tls/certs/default_access-creds_key
- job_name: podScrape/default/test-vps/1
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names:
      - default
  honor_labels: false
  metrics_path: /metrics-5
  sample_limit: 10
  relabel_configs:
  - action: drop
    source_labels:
    - __meta_kubernetes_pod_phase
    regex: (Failed|Succeeded)
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_label_app
    regex: prod
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_container_port_name
    regex: "801"
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
    replacement: default/test-vps
  - source_labels:
    - __meta_kubernetes_pod_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "801"
  tls_config:
    insecure_skip_verify: true
    ca_file: /etc/vmagent-tls/certs/default_access-creds_ca
    cert_file: /etc/vmagent-tls/certs/default_access-creds_cert
    key_file: /etc/vmagent-tls/certs/default_access-creds_key
- job_name: probe/kube-system/test-vmp/0
  honor_labels: false
  metrics_path: /probe
  static_configs:
  - targets:
    - localhost:8428
  relabel_configs:
  - source_labels:
    - __address__
    target_label: __param_target
  - source_labels:
    - __param_target
    target_label: instance
  - target_label: __address__
    replacement: http://blackbox
- job_name: nodeScrape/default/test-vms
  kubernetes_sd_configs:
  - role: node
  honor_labels: false
  relabel_configs:
  - source_labels:
    - __meta_kubernetes_node_name
    target_label: node
  - target_label: job
    replacement: default/test-vms
  basic_auth:
    username: some-username
    password: some-password
- job_name: staticScrape/default/test-vmstatic/0
  static_configs:
  - targets: []
  honor_labels: false
  metrics_path: /metrics-3
  proxy_url: https://some-proxy-1
  scheme: https
  relabel_configs: []
  oauth2:
    client_id: some-client-id
    client_secret: some-client-secret
    token_url: https://some-tr
`,
		},
		{
			name: "with missing secret references",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAgentSpec{
					ServiceScrapeNamespaceSelector: &metav1.LabelSelector{},
					ServiceScrapeSelector:          &metav1.LabelSelector{},
					PodScrapeSelector:              &metav1.LabelSelector{},
					PodScrapeNamespaceSelector:     &metav1.LabelSelector{},
					NodeScrapeNamespaceSelector:    &metav1.LabelSelector{},
					NodeScrapeSelector:             &metav1.LabelSelector{},
					StaticScrapeNamespaceSelector:  &metav1.LabelSelector{},
					StaticScrapeSelector:           &metav1.LabelSelector{},
					ProbeNamespaceSelector:         &metav1.LabelSelector{},
					ProbeSelector:                  &metav1.LabelSelector{},
				},
			},
			c: config.MustGetBaseConfig(),
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default",
					},
				},

				&vmv1beta1.VMNodeScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-bad-0",
					},
					Spec: vmv1beta1.VMNodeScrapeSpec{
						EndpointAuth: vmv1beta1.EndpointAuth{
							BasicAuth: &vmv1beta1.BasicAuth{
								Username: corev1.SecretKeySelector{
									Key: "username",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "access-credentials",
									},
								},
								Password: corev1.SecretKeySelector{
									Key: "password",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "access-credentials",
									},
								},
							},
						},
					},
				},

				&vmv1beta1.VMNodeScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-good",
					},
					Spec: vmv1beta1.VMNodeScrapeSpec{},
				},

				&vmv1beta1.VMNodeScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "bad-1",
					},
					Spec: vmv1beta1.VMNodeScrapeSpec{
						EndpointAuth: vmv1beta1.EndpointAuth{
							BearerTokenSecret: &corev1.SecretKeySelector{
								Key: "username",
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "access-credentials",
								},
							},
						},
					},
				},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vps-mixed",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{
						JobLabel:          "app",
						NamespaceSelector: vmv1beta1.NamespaceSelector{},
						Selector: metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"prod"},
								},
							},
						},
						SampleLimit: 10,
						PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
							{
								Port: ptr.To("805"),
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics-3",
									VMScrapeParams: &vmv1beta1.VMScrapeParams{
										StreamParse: ptr.To(true),
										ProxyClientConfig: &vmv1beta1.ProxyAuth{
											BearerToken: &corev1.SecretKeySelector{
												Key: "username",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "access-credentials",
												},
											},
										},
									},
								},
							},
							{
								Port: ptr.To("801"),
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics-5",
								},
								EndpointAuth: vmv1beta1.EndpointAuth{
									BasicAuth: &vmv1beta1.BasicAuth{
										Username: corev1.SecretKeySelector{
											Key: "username",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "access-credentials",
											},
										},
										Password: corev1.SecretKeySelector{
											Key: "password",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "access-credentials",
											},
										},
									},
								},
							},
							{
								Port: ptr.To("801"),
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics-5-good",
								},
							},
						},
					},
				},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vps-good",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{
						JobLabel:          "app",
						NamespaceSelector: vmv1beta1.NamespaceSelector{},
						Selector: metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"prod"},
								},
							},
						},
						PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
							{
								Port: ptr.To("8011"),
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics-1-good",
								},
							},
						},
					},
				},
				&vmv1beta1.VMStaticScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vmstatic-bad",
					},
					Spec: vmv1beta1.VMStaticScrapeSpec{
						TargetEndpoints: []*vmv1beta1.TargetEndpoint{
							{
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path:     "/metrics-3",
									Scheme:   "https",
									ProxyURL: ptr.To("https://some-proxy-1"),
								},
								EndpointAuth: vmv1beta1.EndpointAuth{
									OAuth2: &vmv1beta1.OAuth2{
										TokenURL: "https://some-tr",
										ClientSecret: &corev1.SecretKeySelector{
											Key: "cs",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "access-credentials",
											},
										},
										ClientID: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												Key: "cid",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "access-credentials",
												},
											},
										},
									},
								},
							},
						},
					},
				},
				&vmv1beta1.VMStaticScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vmstatic-bad-tls",
					},
					Spec: vmv1beta1.VMStaticScrapeSpec{
						TargetEndpoints: []*vmv1beta1.TargetEndpoint{
							{
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path:     "/metrics-3",
									Scheme:   "https",
									ProxyURL: ptr.To("https://some-proxy-1"),
								},
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										Cert: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												Key: "cert",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-credentials",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantConfig: `global:
  scrape_interval: 30s
  external_labels:
    prometheus: default/test
scrape_configs:
- job_name: podScrape/default/test-vps-good/0
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names:
      - default
  honor_labels: false
  metrics_path: /metrics-1-good
  relabel_configs:
  - action: drop
    source_labels:
    - __meta_kubernetes_pod_phase
    regex: (Failed|Succeeded)
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_label_app
    regex: prod
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_container_port_name
    regex: "8011"
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
    replacement: default/test-vps-good
  - source_labels:
    - __meta_kubernetes_pod_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "8011"
- job_name: nodeScrape/default/test-good
  kubernetes_sd_configs:
  - role: node
  honor_labels: false
  relabel_configs:
  - source_labels:
    - __meta_kubernetes_node_name
    target_label: node
  - target_label: job
    replacement: default/test-good
`,
		},
		{
			name: "with changed default config value",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAgentSpec{
					ServiceScrapeNamespaceSelector: &metav1.LabelSelector{},
					ServiceScrapeSelector:          &metav1.LabelSelector{},
					PodScrapeSelector:              &metav1.LabelSelector{},
					PodScrapeNamespaceSelector:     &metav1.LabelSelector{},
					NodeScrapeNamespaceSelector:    &metav1.LabelSelector{},
					NodeScrapeSelector:             &metav1.LabelSelector{},
					StaticScrapeNamespaceSelector:  &metav1.LabelSelector{},
					StaticScrapeSelector:           &metav1.LabelSelector{},
					ProbeNamespaceSelector:         &metav1.LabelSelector{},
					ProbeSelector:                  &metav1.LabelSelector{},
				},
			},
			c: func() *config.BaseOperatorConf {
				cfg := *config.MustGetBaseConfig()
				cfg.VMServiceScrapeDefault.EnforceEndpointSlices = true
				return &cfg
			}(),
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default",
					},
				},
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vms",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Selector:          metav1.LabelSelector{},
						JobLabel:          "app",
						NamespaceSelector: vmv1beta1.NamespaceSelector{},
						Endpoints: []vmv1beta1.Endpoint{
							{
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics",
								},
								Port: "8085",
								EndpointAuth: vmv1beta1.EndpointAuth{
									BearerTokenSecret: &corev1.SecretKeySelector{
										Key: "bearer",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "access-creds",
										},
									},
								},
							},
							{
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics-2",
								},
								Port: "8083",
							},
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "access-creds",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"cid":      []byte(`some-client-id`),
						"cs":       []byte(`some-client-secret`),
						"username": []byte(`some-username`),
						"password": []byte(`some-password`),
						"ca":       []byte(`some-ca-cert`),
						"cert":     []byte(`some-cert`),
						"key":      []byte(`some-key`),
						"bearer":   []byte(`some-bearer`),
					},
				},
			},
			wantConfig: `global:
  scrape_interval: 30s
  external_labels:
    prometheus: default/test
scrape_configs:
- job_name: serviceScrape/default/test-vms/0
  kubernetes_sd_configs:
  - role: endpointslices
    namespaces:
      names:
      - default
  honor_labels: false
  metrics_path: /metrics
  relabel_configs:
  - action: keep
    source_labels:
    - __meta_kubernetes_endpointslice_port_name
    regex: "8085"
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
  - source_labels:
    - __meta_kubernetes_service_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "8085"
  bearer_token: some-bearer
- job_name: serviceScrape/default/test-vms/1
  kubernetes_sd_configs:
  - role: endpointslices
    namespaces:
      names:
      - default
  honor_labels: false
  metrics_path: /metrics-2
  relabel_configs:
  - action: keep
    source_labels:
    - __meta_kubernetes_endpointslice_port_name
    regex: "8083"
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
  - source_labels:
    - __meta_kubernetes_service_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "8083"
`,
		},
		{
			name: "with oauth2 tls config",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAgentSpec{
					ServiceScrapeNamespaceSelector: &metav1.LabelSelector{},
					ServiceScrapeSelector:          &metav1.LabelSelector{},
					PodScrapeSelector:              &metav1.LabelSelector{},
					PodScrapeNamespaceSelector:     &metav1.LabelSelector{},
					NodeScrapeNamespaceSelector:    &metav1.LabelSelector{},
					NodeScrapeSelector:             &metav1.LabelSelector{},
					StaticScrapeNamespaceSelector:  &metav1.LabelSelector{},
					StaticScrapeSelector:           &metav1.LabelSelector{},
					ProbeNamespaceSelector:         &metav1.LabelSelector{},
					ProbeSelector:                  &metav1.LabelSelector{},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default",
					},
				},
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vms",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Selector:          metav1.LabelSelector{},
						JobLabel:          "app",
						NamespaceSelector: vmv1beta1.NamespaceSelector{},
						Endpoints: []vmv1beta1.Endpoint{
							{
								EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
									Path: "/metrics",
								},
								Port: "8085",
								EndpointAuth: vmv1beta1.EndpointAuth{
									OAuth2: &vmv1beta1.OAuth2{
										ClientID: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												Key: "CLIENT_ID",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "oauth2-access",
												},
											},
										},
										ClientSecret: &corev1.SecretKeySelector{
											Key: "CLIENT_SECRET",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "oauth2-access",
											},
										},
										TokenURL: "http://some-url",
										TLSConfig: &vmv1beta1.TLSConfig{
											CA: vmv1beta1.SecretOrConfigMap{
												ConfigMap: &corev1.ConfigMapKeySelector{
													Key: "CA",
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "tls-default",
													},
												},
											},
											Cert: vmv1beta1.SecretOrConfigMap{
												Secret: &corev1.SecretKeySelector{
													Key: "CERT",
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "tls-auth",
													},
												},
											},
											KeySecret: &corev1.SecretKeySelector{
												Key: "SECRET_KEY",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-auth",
												},
											},
											InsecureSkipVerify: false,
										},
									},
									BearerTokenSecret: &corev1.SecretKeySelector{
										Key: "bearer",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "access-creds",
										},
									},
								},
							},
						},
					},
				},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "dev-pods",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{
						PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
							{
								Port: ptr.To("8081"),
								EndpointAuth: vmv1beta1.EndpointAuth{
									OAuth2: &vmv1beta1.OAuth2{
										ClientID: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												Key: "CLIENT_ID",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "oauth2-access",
												},
											},
										},
										ClientSecret: &corev1.SecretKeySelector{
											Key: "CLIENT_SECRET",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "oauth2-access",
											},
										},
										TokenURL: "http://some-url",
										TLSConfig: &vmv1beta1.TLSConfig{
											CA: vmv1beta1.SecretOrConfigMap{
												ConfigMap: &corev1.ConfigMapKeySelector{
													Key: "CA",
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "tls-default",
													},
												},
											},
											Cert: vmv1beta1.SecretOrConfigMap{
												Secret: &corev1.SecretKeySelector{
													Key: "CERT",
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "tls-auth",
													},
												},
											},
											KeySecret: &corev1.SecretKeySelector{
												Key: "SECRET_KEY",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-auth",
												},
											},
											InsecureSkipVerify: false,
										},
									},
								},
							},
						},
					},
				},
				&vmv1beta1.VMNodeScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "k8s-nodes",
					},
					Spec: vmv1beta1.VMNodeScrapeSpec{
						Port: "9093",
						EndpointAuth: vmv1beta1.EndpointAuth{
							OAuth2: &vmv1beta1.OAuth2{
								ClientID: vmv1beta1.SecretOrConfigMap{
									Secret: &corev1.SecretKeySelector{
										Key: "CLIENT_ID",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "oauth2-access",
										},
									},
								},
								ClientSecret: &corev1.SecretKeySelector{
									Key: "CLIENT_SECRET",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "oauth2-access",
									},
								},
								TokenURL: "http://some-url",
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{
											Key: "CA",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "tls-default",
											},
										},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											Key: "CERT",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "tls-auth",
											},
										},
									},
									KeySecret: &corev1.SecretKeySelector{
										Key: "SECRET_KEY",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "tls-auth",
										},
									},
									InsecureSkipVerify: false,
								},
							},
						},
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-default",
						Namespace: "default",
					},
					Data: map[string]string{
						"CA": "ca data",
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-auth",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"CERT":       []byte(`cert data`),
						"SECRET_KEY": []byte(`key data`),
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "oauth2-access",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"CLIENT_ID":     []byte(`data`),
						"CLIENT_SECRET": []byte(`data`),
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "access-creds",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"cid":      []byte(`some-client-id`),
						"cs":       []byte(`some-client-secret`),
						"username": []byte(`some-username`),
						"password": []byte(`some-password`),
						"ca":       []byte(`some-ca-cert`),
						"cert":     []byte(`some-cert`),
						"key":      []byte(`some-key`),
						"bearer":   []byte(`some-bearer`),
					},
				},
			},
			wantConfig: `global:
  scrape_interval: 30s
  external_labels:
    prometheus: default/test
scrape_configs:
- job_name: serviceScrape/default/test-vms/0
  kubernetes_sd_configs:
  - role: endpoints
    namespaces:
      names:
      - default
  honor_labels: false
  metrics_path: /metrics
  relabel_configs:
  - action: keep
    source_labels:
    - __meta_kubernetes_endpoint_port_name
    regex: "8085"
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
  - source_labels:
    - __meta_kubernetes_service_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "8085"
  bearer_token: some-bearer
  oauth2:
    client_id: data
    client_secret: data
    token_url: http://some-url
    tls_config:
      ca_file: /etc/vmagent-tls/certs/default_configmap_tls-default_CA
      cert_file: /etc/vmagent-tls/certs/default_tls-auth_CERT
      key_file: /etc/vmagent-tls/certs/default_tls-auth_SECRET_KEY
- job_name: podScrape/default/dev-pods/0
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names:
      - default
  honor_labels: false
  relabel_configs:
  - action: drop
    source_labels:
    - __meta_kubernetes_pod_phase
    regex: (Failed|Succeeded)
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_container_port_name
    regex: "8081"
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
    replacement: default/dev-pods
  - target_label: endpoint
    replacement: "8081"
  oauth2:
    client_id: data
    client_secret: data
    token_url: http://some-url
    tls_config:
      ca_file: /etc/vmagent-tls/certs/default_configmap_tls-default_CA
      cert_file: /etc/vmagent-tls/certs/default_tls-auth_CERT
      key_file: /etc/vmagent-tls/certs/default_tls-auth_SECRET_KEY
- job_name: nodeScrape/default/k8s-nodes
  kubernetes_sd_configs:
  - role: node
  honor_labels: false
  relabel_configs:
  - source_labels:
    - __meta_kubernetes_node_name
    target_label: node
  - target_label: job
    replacement: default/k8s-nodes
  - source_labels:
    - __address__
    target_label: __address__
    regex: ^(.*):(.*)
    replacement: ${1}:9093
  oauth2:
    client_id: data
    client_secret: data
    token_url: http://some-url
    tls_config:
      ca_file: /etc/vmagent-tls/certs/default_configmap_tls-default_CA
      cert_file: /etc/vmagent-tls/certs/default_tls-auth_CERT
      key_file: /etc/vmagent-tls/certs/default_tls-auth_SECRET_KEY
`,
		},
		{
			name: "daemonset mode",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "per-node",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAgentSpec{
					DaemonSetMode:      true,
					SelectAllByDefault: true,
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default-2",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "system",
					},
				},
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default-1",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "http",
							},
						},
					},
				},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{
						PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
							{
								Port: ptr.To("web"),
							},
							{
								PortNumber: ptr.To(int32(8085)),
							},
						},
					},
				},
			},
			wantConfig: `global:
  scrape_interval: 30s
  external_labels:
    prometheus: default/per-node
scrape_configs:
- job_name: podScrape/default/pod-1/0
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names:
      - default
    selectors:
    - role: pod
      field: spec.nodeName=%{KUBE_NODE_NAME}
  honor_labels: false
  relabel_configs:
  - action: drop
    source_labels:
    - __meta_kubernetes_pod_phase
    regex: (Failed|Succeeded)
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
    replacement: default/pod-1
  - target_label: endpoint
    replacement: web
- job_name: podScrape/default/pod-1/1
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names:
      - default
    selectors:
    - role: pod
      field: spec.nodeName=%{KUBE_NODE_NAME}
  honor_labels: false
  relabel_configs:
  - action: drop
    source_labels:
    - __meta_kubernetes_pod_phase
    regex: (Failed|Succeeded)
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_container_port_number
    regex: 8085
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
    replacement: default/pod-1
`,
		},
		{
			name: "with invalid objects syntax",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "select-all",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAgentSpec{
					SelectAllByDefault: true,
					VMAgentSecurityEnforcements: vmv1beta1.VMAgentSecurityEnforcements{
						ArbitraryFSAccessThroughSMs: vmv1beta1.ArbitraryFSAccessThroughSMsConfig{
							Deny: true,
						},
					},
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "http://some-single.example.com",
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "fs-access",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "8080",
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CAFile: "/etc/passwd",
									},
								},
							},
						},
					},
				},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "bad-syntax",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{
						Selector: *metav1.SetAsLabelSelector(map[string]string{
							"alb.ingress.kubernetes.io/tags": "Environment=devl",
						}),
					},
				},
				&vmv1beta1.VMProbe{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "bad-syntax",
					},
					Spec: vmv1beta1.VMProbeSpec{
						Targets: vmv1beta1.VMProbeTargets{
							Ingress: &vmv1beta1.ProbeTargetIngress{
								Selector: *metav1.SetAsLabelSelector(map[string]string{
									"alb.ingress.kubernetes.io/tags": "Environment=devl",
								}),
							},
						},
					},
				},
			},
			wantConfig: `global:
  scrape_interval: 30s
  external_labels:
    prometheus: default/select-all
scrape_configs: []
`,
		},
		{
			name: "with partial missing refs",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "select-all",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAgentSpec{
					SelectAllByDefault: true,
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "http://some-single.example.com",
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "partially-correct",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Endpoints: []vmv1beta1.Endpoint{
							{
								Port: "8080",
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CA: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												Key: "ca",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-auth",
												},
											},
										},
									},
								},
							},
							{
								Port: "8081",
							},
						},
					},
				},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "partially-correct",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{
						PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
							{
								Port: ptr.To("8035"),
							},
							{
								Port: ptr.To("8080"),
								EndpointAuth: vmv1beta1.EndpointAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CA: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												Key: "ca",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tls-auth",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantConfig: `global:
  scrape_interval: 30s
  external_labels:
    prometheus: default/select-all
scrape_configs: []
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			cfgO := *config.MustGetBaseConfig()
			if tt.c != nil {
				*config.MustGetBaseConfig() = *tt.c
				defer func() {
					*config.MustGetBaseConfig() = cfgO
				}()
			}

			build.AddDefaults(testClient.Scheme())
			ac := getAssetsCache(ctx, testClient, tt.cr)
			if err := createOrUpdateConfigurationSecret(ctx, testClient, tt.cr, nil, nil, ac); (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateConfigurationSecret() error = %v, wantErr %v", err, tt.wantErr)
			}
			var expectSecret corev1.Secret
			if err := testClient.Get(ctx, types.NamespacedName{Namespace: tt.cr.Namespace, Name: tt.cr.PrefixedName()}, &expectSecret); err != nil {
				t.Fatalf("cannot get vmagent config secret: %s", err)
			}
			gotCfg := expectSecret.Data[vmagentGzippedFilename]
			cfgB := bytes.NewBuffer(gotCfg)
			gr, err := gzip.NewReader(cfgB)
			if err != nil {
				t.Fatalf("er: %s", err)
			}
			data, err := io.ReadAll(gr)
			if err != nil {
				t.Fatalf("cannot read cfg: %s", err)
			}
			gr.Close()
			assert.Equal(t, tt.wantConfig, string(data))
		})
	}
}

func TestScrapeObjectFailedStatus(t *testing.T) {

	type getStatusMeta interface {
		GetStatusMetadata() *vmv1beta1.StatusMetadata
	}
	f := func(so client.Object) {
		t.Helper()
		expectedConfig := `global:
  scrape_interval: 30s
  external_labels:
    prometheus: default/vmagent
scrape_configs: []
`
		ctx := context.TODO()
		testClient := k8stools.GetTestClientWithClientObjects([]client.Object{so})
		build.AddDefaults(testClient.Scheme())

		cr := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "http://vmsingle.example.com",
					},
				},
				SelectAllByDefault: true,
			},
		}
		ac := getAssetsCache(ctx, testClient, cr)
		if err := createOrUpdateConfigurationSecret(ctx, testClient, cr, nil, nil, ac); err != nil {
			t.Errorf("CreateOrUpdateConfigurationSecret() error = %s", err)
		}
		var configSecret corev1.Secret
		if err := testClient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &configSecret); err != nil {
			t.Fatalf("cannot get vmagent config secret: %s", err)
		}

		gotCfg := configSecret.Data[vmagentGzippedFilename]
		cfgB := bytes.NewBuffer(gotCfg)
		gr, err := gzip.NewReader(cfgB)
		if err != nil {
			t.Fatalf("er: %s", err)
		}
		data, err := io.ReadAll(gr)
		if err != nil {
			t.Fatalf("cannot read cfg: %s", err)
		}
		gr.Close()
		assert.Equal(t, expectedConfig, string(data))

		if err := testClient.Get(ctx, types.NamespacedName{Name: so.GetName(), Namespace: so.GetNamespace()}, so); err != nil {
			t.Fatalf("cannot reload object: %s", err)
		}
		status := so.(getStatusMeta).GetStatusMetadata()
		assert.Equal(t, vmv1beta1.UpdateStatusFailed, status.UpdateStatus)
		assert.NotEmpty(t, status.Reason)
		assert.Len(t, status.Conditions, 1)

	}
	commonMeta := metav1.ObjectMeta{
		Name:      "invalid",
		Namespace: "default",
	}

	// invalid selector
	f(&vmv1beta1.VMProbe{
		ObjectMeta: commonMeta,
		Spec: vmv1beta1.VMProbeSpec{
			Targets: vmv1beta1.VMProbeTargets{
				Ingress: &vmv1beta1.ProbeTargetIngress{
					Selector: *metav1.SetAsLabelSelector(map[string]string{"alb.ingress.kubernetes.io/tags": "Environment=devl"}),
				},
			},
		},
	},
	)
	// missing refs
	f(&vmv1beta1.VMScrapeConfig{
		ObjectMeta: commonMeta,
		Spec: vmv1beta1.VMScrapeConfigSpec{
			ConsulSDConfigs: []vmv1beta1.ConsulSDConfig{
				{
					Server: "http://consul.example.com",
					BasicAuth: &vmv1beta1.BasicAuth{
						Username: corev1.SecretKeySelector{
							Key: "username",
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "auth",
							},
						},
					},
				},
			},
		},
	},
	)
	commonEndpointAuthWithMissingRef := vmv1beta1.EndpointAuth{
		BasicAuth: &vmv1beta1.BasicAuth{
			Username: corev1.SecretKeySelector{
				Key: "username",
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "auth",
				},
			},
		},
	}
	f(&vmv1beta1.VMProbe{
		ObjectMeta: commonMeta,
		Spec: vmv1beta1.VMProbeSpec{
			EndpointAuth: commonEndpointAuthWithMissingRef,
		},
	})

	f(&vmv1beta1.VMStaticScrape{
		ObjectMeta: commonMeta,
		Spec: vmv1beta1.VMStaticScrapeSpec{
			TargetEndpoints: []*vmv1beta1.TargetEndpoint{
				{
					EndpointAuth: commonEndpointAuthWithMissingRef,
				},
			},
		},
	})
	f(&vmv1beta1.VMNodeScrape{
		ObjectMeta: commonMeta,
		Spec: vmv1beta1.VMNodeScrapeSpec{
			EndpointAuth: commonEndpointAuthWithMissingRef,
		},
	})

	f(&vmv1beta1.VMPodScrape{
		ObjectMeta: commonMeta,
		Spec: vmv1beta1.VMPodScrapeSpec{
			PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
				{
					EndpointAuth: commonEndpointAuthWithMissingRef,
				},
			},
		},
	})

	f(&vmv1beta1.VMServiceScrape{
		ObjectMeta: commonMeta,
		Spec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{
				{
					EndpointAuth: commonEndpointAuthWithMissingRef,
				},
			},
		},
	})

}
