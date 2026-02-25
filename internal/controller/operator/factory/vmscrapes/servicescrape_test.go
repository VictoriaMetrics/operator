package vmscrapes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_generateServiceScrapeConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		sc                *vmv1beta1.VMServiceScrape
		predefinedObjects []runtime.Object
		want              string
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ac := getAssetsCache(ctx, fclient)
		pos := &ParsedObjects{
			Namespace:       o.cr.Namespace,
			APIServerConfig: o.cr.Spec.APIServerConfig,
		}
		sp := &o.cr.Spec.CommonScrapeParams
		got, err := generateServiceScrapeConfig(ctx, sp, pos, o.sc, o.sc.Spec.Endpoints[0], 0, ac)
		if assert.NoError(t, err, "cannot generate ServiceScrapeConfig") {
			gotBytes, err := yaml.Marshal(got)
			if assert.NoError(t, err, "cannot marshal ServiceScrapeConfig to yaml") {
				assert.Equal(t, o.want, string(gotBytes))
			}
		}
	}

	// generate simple config
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					CommonScrapeSecurityEnforcements: vmv1beta1.CommonScrapeSecurityEnforcements{
						OverrideHonorLabels:      false,
						OverrideHonorTimestamps:  false,
						IgnoreNamespaceSelectors: false,
						EnforcedNamespaceLabel:   "",
					},
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
						AttachMetadata: vmv1beta1.AttachMetadata{
							Node: ptr.To(true),
						},
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
							BearerTokenFile: "/var/run/token",
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"ca": []byte("ca-value"),
				},
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
  ca_file: /tls/default_tls-secret_ca
bearer_token_file: /var/run/token
`,
	})

	// generate config with scrape interval limit
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					MaxScrapeInterval: ptr.To("40m"),
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
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
							BearerTokenFile: "/var/run/token",
						},
						EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
							ScrapeInterval: "60m",
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"ca": []byte("ca-value"),
				},
			},
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
  ca_file: /tls/default_tls-secret_ca
bearer_token_file: /var/run/token
`,
	})

	// generate config with scrape interval limit - reach min
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					MinScrapeInterval: ptr.To("1m"),
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
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
							BearerTokenFile: "/var/run/token",
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"ca": []byte("ca-value"),
				},
			},
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
  ca_file: /tls/default_tls-secret_ca
bearer_token_file: /var/run/token
`,
	})

	// config with discovery role endpointslice
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				DiscoveryRole: k8sSDRoleEndpointslice,
				Endpoints: []vmv1beta1.Endpoint{
					{
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
							BearerTokenFile: "/var/run/token",
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"ca": []byte("ca-value"),
				},
			},
		},
		want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: endpointslice
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
  ca_file: /tls/default_tls-secret_ca
bearer_token_file: /var/run/token
`,
	})

	// config with discovery role services
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				DiscoveryRole: k8sSDRoleService,
				Endpoints: []vmv1beta1.Endpoint{
					{
						AttachMetadata: vmv1beta1.AttachMetadata{
							Node:      ptr.To(true),
							Namespace: ptr.To(true),
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
							BearerTokenFile: "/var/run/token",
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"ca": []byte("ca-value"),
				},
			},
		},
		want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: service
  attach_metadata:
    namespace: true
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
  ca_file: /tls/default_tls-secret_ca
bearer_token_file: /var/run/token
`,
	})

	// bad discovery role service without port name
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				DiscoveryRole: k8sSDRoleService,
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
	})

	// config with tls insecure
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				DiscoveryRole: k8sSDRoleService,
				Endpoints: []vmv1beta1.Endpoint{
					{
						Port: "8080",
						EndpointAuth: vmv1beta1.EndpointAuth{
							TLSConfig: &vmv1beta1.TLSConfig{
								InsecureSkipVerify: true,
							},
							BearerTokenFile: "/var/run/token",
						},
					},
				},
			},
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
bearer_token_file: /var/run/token
`,
	})

	// complete config
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				DiscoveryRole: k8sSDRoleService,
				Endpoints: []vmv1beta1.Endpoint{
					{
						Port: "8080",
						EndpointAuth: vmv1beta1.EndpointAuth{
							TLSConfig: &vmv1beta1.TLSConfig{
								InsecureSkipVerify: true,
							},
							BasicAuth: &vmv1beta1.BasicAuth{
								Username: corev1.SecretKeySelector{
									Key:                  "username",
									LocalObjectReference: corev1.LocalObjectReference{Name: "ba-secret"},
								},
								Password: corev1.SecretKeySelector{
									Key:                  "password",
									LocalObjectReference: corev1.LocalObjectReference{Name: "ba-secret"},
								},
							},
							BearerTokenFile: "/var/run/token",
							BearerTokenSecret: &corev1.SecretKeySelector{
								Key:                  "bearer",
								LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
							},
							OAuth2: &vmv1beta1.OAuth2{
								Scopes:         []string{"scope-1"},
								TokenURL:       "http://some-token-url",
								EndpointParams: map[string]string{"timeout": "5s"},
								ClientID: vmv1beta1.SecretOrConfigMap{
									Secret: &corev1.SecretKeySelector{
										Key:                  "id",
										LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
									},
								},
								ClientSecret: &corev1.SecretKeySelector{
									Key:                  "secret",
									LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
								},
								ProxyURL: "http://oauth2-access-proxy",
								TLSConfig: &vmv1beta1.TLSConfig{
									InsecureSkipVerify: true,
									CA: vmv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{
											Key: "ca",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "tls-cm",
											},
										},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											Key: "key",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "tls",
											},
										},
									},
									KeySecret: &corev1.SecretKeySelector{
										Key: "cert",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "tls",
										},
									},
								},
							},
						},
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
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ba-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte("user"),
					"password": []byte("pass"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "access-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"bearer": []byte("token"),
					"id":     []byte("some-id"),
					"secret": []byte("some-secret"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"key":  []byte("key-value"),
					"cert": []byte("cert-value"),
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-cm",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca": "ca-value",
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
bearer_token_file: /var/run/token
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
  proxy_url: http://oauth2-access-proxy
  tls_config:
    insecure_skip_verify: true
    ca_file: /tls/default_configmap_tls-cm_ca
    cert_file: /tls/default_tls_key
    key_file: /tls/default_tls_cert
`,
	})

	// with templateRelabel
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeRelabelTemplate: []*vmv1beta1.RelabelConfig{
						{
							TargetLabel:  "node",
							SourceLabels: []string{"__meta_kubernetes_node_name"},
							Regex:        []string{".+"},
						},
					},
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
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
							BearerTokenFile: "/var/run/token",
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"ca": []byte("ca-value"),
				},
			},
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
  ca_file: /tls/default_tls-secret_ca
bearer_token_file: /var/run/token
`,
	})

	// with selectors endpoints
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					EnableKubernetesAPISelectors: true,
					CommonScrapeSecurityEnforcements: vmv1beta1.CommonScrapeSecurityEnforcements{
						OverrideHonorLabels:      false,
						OverrideHonorTimestamps:  false,
						IgnoreNamespaceSelectors: false,
						EnforcedNamespaceLabel:   "",
					},
				},
				APIServerConfig: &vmv1beta1.APIServerConfig{
					Host: "default-k8s-host",
					TLSConfig: &vmv1beta1.TLSConfig{
						CAFile: "/tls/default_k8s_host_ca",
					},
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				Selector: *metav1.SetAsLabelSelector(map[string]string{
					"env":  "dev",
					"dc":   "prod",
					"team": "go",
				}),
				Endpoints: []vmv1beta1.Endpoint{
					{
						Port: "8080",
						AttachMetadata: vmv1beta1.AttachMetadata{
							Node:      ptr.To(true),
							Namespace: ptr.To(true),
						},
					},
				},
			},
		},
		want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: endpoints
  attach_metadata:
    node: true
    namespace: true
  namespaces:
    names:
    - default
  api_server: default-k8s-host
  tls_config:
    ca_file: /tls/default_k8s_host_ca
  selectors:
  - role: endpoints
    label: dc=prod,env=dev,team=go
  - role: pod
    label: dc=prod,env=dev,team=go
  - role: service
    label: dc=prod,env=dev,team=go
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
`,
	})

	// with selectors services
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					EnableKubernetesAPISelectors: true,
				},
				APIServerConfig: &vmv1beta1.APIServerConfig{
					Host: "default-k8s-host",
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				DiscoveryRole: k8sSDRoleService,
				Endpoints: []vmv1beta1.Endpoint{
					{
						Port: "8080",
						AttachMetadata: vmv1beta1.AttachMetadata{
							Node: ptr.To(true),
						},
					},
				},
				Selector: *metav1.SetAsLabelSelector(map[string]string{
					"env": "dev",
				}),
			},
		},
		want: `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: service
  namespaces:
    names:
    - default
  api_server: default-k8s-host
  selectors:
  - role: service
    label: env=dev
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
`,
	})

	// relabelings in scrapeClass and in endpoint
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					CommonScrapeSecurityEnforcements: vmv1beta1.CommonScrapeSecurityEnforcements{
						OverrideHonorLabels:      false,
						OverrideHonorTimestamps:  false,
						IgnoreNamespaceSelectors: false,
						EnforcedNamespaceLabel:   "",
					},
					ScrapeClasses: []vmv1beta1.ScrapeClass{
						{
							Name:    "default",
							Default: ptr.To(true),
							EndpointRelabelings: vmv1beta1.EndpointRelabelings{
								RelabelConfigs: []*vmv1beta1.RelabelConfig{
									{
										Action:       "replace",
										SourceLabels: []string{"__meta_kubernetes_pod_app_name"},
										TargetLabel:  "app",
									},
								},
							},
						},
						{
							Name: "not-default",
							EndpointRelabelings: vmv1beta1.EndpointRelabelings{
								MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{
									{
										Action:       "replace",
										SourceLabels: []string{"__meta_kubernetes_pod_node_name"},
										TargetLabel:  "node",
									},
								},
							},
						},
					},
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
						Port: "8080",
						EndpointRelabelings: vmv1beta1.EndpointRelabelings{
							RelabelConfigs: []*vmv1beta1.RelabelConfig{
								{
									Action:       "replace",
									SourceLabels: []string{"__meta_kubernetes_namespace"},
									TargetLabel:  "namespace",
								},
							},
						},
						AttachMetadata: vmv1beta1.AttachMetadata{
							Node: ptr.To(true),
						},
					},
				},
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
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
  action: replace
- source_labels:
  - __meta_kubernetes_pod_app_name
  target_label: app
  action: replace
`,
	})

	// scrapeClass with TLS and relabel config inheritance
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ScrapeClasses: []vmv1beta1.ScrapeClass{
						{
							Name:    "custom-class",
							Default: ptr.To(false),
							EndpointAuth: vmv1beta1.EndpointAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CAFile: "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "tls-secret",
											},
											Key: "cert",
										},
									},
								},
							},
							EndpointRelabelings: vmv1beta1.EndpointRelabelings{
								RelabelConfigs: []*vmv1beta1.RelabelConfig{
									{
										SourceLabels: []string{"__meta_kubernetes_pod_node_name"},
										TargetLabel:  "node",
										Action:       "replace",
									},
								},
							},
						},
					},
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				ScrapeClassName: ptr.To("custom-class"),
				Endpoints: []vmv1beta1.Endpoint{
					{
						Port: "8080",
						EndpointRelabelings: vmv1beta1.EndpointRelabelings{
							RelabelConfigs: []*vmv1beta1.RelabelConfig{
								{
									SourceLabels: []string{"__meta_kubernetes_pod_container_name"},
									TargetLabel:  "container",
									Action:       "replace",
								},
							},
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"cert": []byte("cert-value"),
				},
			},
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
  - __meta_kubernetes_pod_container_name
  target_label: container
  action: replace
- source_labels:
  - __meta_kubernetes_pod_node_name
  target_label: node
  action: replace
tls_config:
  ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
  cert_file: /tls/default_tls-secret_cert
`,
	})

	// default scrapeClass with attachMetadata
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ScrapeClasses: []vmv1beta1.ScrapeClass{
						{
							Name:    "default",
							Default: ptr.To(true),
							AttachMetadata: &vmv1beta1.AttachMetadata{
								Node: ptr.To(true),
							},
						},
					},
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
						Port: "8080",
					},
				},
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
`,
	})

	// scrapeClass with authorization and TLS config inheritance
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ScrapeClasses: []vmv1beta1.ScrapeClass{
						{
							Name:    "secure-class",
							Default: ptr.To(false),
							EndpointAuth: vmv1beta1.EndpointAuth{
								Authorization: &vmv1beta1.Authorization{
									Credentials: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "auth-secret",
										},
										Key: "token",
									},
									Type: "Bearer",
								},
								TLSConfig: &vmv1beta1.TLSConfig{
									CAFile:             "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
									InsecureSkipVerify: false,
								},
							},
						},
					},
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				ScrapeClassName: ptr.To("secure-class"),
				Endpoints: []vmv1beta1.Endpoint{
					{
						Port: "8443",
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"token": []byte("secret-token-value"),
				},
			},
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
  regex: "8443"
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
  replacement: "8443"
tls_config:
  ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
authorization:
  credentials: secret-token-value
  type: Bearer
`,
	})

	// scrapeClass with multiple metric relabelings merge
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ScrapeClasses: []vmv1beta1.ScrapeClass{
						{
							Name:    "metrics-class",
							Default: ptr.To(true),
							EndpointRelabelings: vmv1beta1.EndpointRelabelings{
								MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{
									{
										SourceLabels: []string{"__name__"},
										Regex:        vmv1beta1.StringOrArray{"go_.*"},
										Action:       "keep",
									},
									{
										TargetLabel: "scrape_class",
										Replacement: ptr.To("metrics-class"),
										Action:      "replace",
									},
								},
							},
						},
					},
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
						Port: "9090",
						EndpointRelabelings: vmv1beta1.EndpointRelabelings{
							MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{
								{
									SourceLabels: []string{"instance"},
									TargetLabel:  "endpoint_instance",
									Action:       "replace",
								},
							},
						},
					},
				},
			},
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
  regex: "9090"
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
  replacement: "9090"
metric_relabel_configs:
- source_labels:
  - instance
  target_label: endpoint_instance
  action: replace
- source_labels:
  - __name__
  regex: go_.*
  action: keep
- target_label: scrape_class
  replacement: metrics-class
  action: replace
`,
	})

	// no default scrapeclass and no scrapeclass specified
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "thedefault-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ScrapeClasses: []vmv1beta1.ScrapeClass{
						{
							Name: "non-default-class",
							EndpointRelabelings: vmv1beta1.EndpointRelabelings{
								RelabelConfigs: []*vmv1beta1.RelabelConfig{
									{
										SourceLabels: []string{"__meta_kubernetes_pod_node_name"},
										TargetLabel:  "node",
										Action:       "replace",
									},
								},
							},
						},
					},
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
						Port: "8080",
						EndpointRelabelings: vmv1beta1.EndpointRelabelings{
							RelabelConfigs: []*vmv1beta1.RelabelConfig{
								{
									SourceLabels: []string{"__meta_kubernetes_pod_container_name"},
									TargetLabel:  "container",
									Action:       "replace",
								},
							},
						},
					},
				},
			},
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
  - __meta_kubernetes_pod_container_name
  target_label: container
  action: replace
`,
	})

	// oauth2 in scrapeClass and in endpoint
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ScrapeClasses: []vmv1beta1.ScrapeClass{
						{
							Name:    "default",
							Default: ptr.To(true),
							EndpointAuth: vmv1beta1.EndpointAuth{
								OAuth2: &vmv1beta1.OAuth2{
									ProxyURL: "http://some",
									Scopes:   []string{"1", "2"},
								},
							},
						},
						{
							Name: "not-default",
							EndpointAuth: vmv1beta1.EndpointAuth{
								OAuth2: &vmv1beta1.OAuth2{
									ProxyURL:         "http://some",
									Scopes:           []string{"1", "2"},
									ClientSecretFile: "/path/to/file",
									EndpointParams:   map[string]string{"param": "value"},
								},
							},
						},
					},
					CommonScrapeSecurityEnforcements: vmv1beta1.CommonScrapeSecurityEnforcements{
						OverrideHonorLabels:      false,
						OverrideHonorTimestamps:  false,
						IgnoreNamespaceSelectors: false,
						EnforcedNamespaceLabel:   "",
					},
				},
			},
		},
		sc: &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				ScrapeClassName: ptr.To("not-default"),

				Endpoints: []vmv1beta1.Endpoint{
					{
						AttachMetadata: vmv1beta1.AttachMetadata{
							Node: ptr.To(true),
						},
						Port: "8080",
						EndpointAuth: vmv1beta1.EndpointAuth{
							OAuth2: &vmv1beta1.OAuth2{
								ProxyURL: "http://expected",
								ClientID: vmv1beta1.SecretOrConfigMap{
									ConfigMap: &corev1.ConfigMapKeySelector{
										Key:                  "some",
										LocalObjectReference: corev1.LocalObjectReference{Name: "cm"},
									},
								},
							},
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cm",
					Namespace: "default",
				},
				Data: map[string]string{"some": "value"},
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
oauth2:
  client_id: value
  client_secret_file: /path/to/file
  scopes:
  - "1"
  - "2"
  endpoint_params:
    param: value
  proxy_url: http://expected
`,
	})
}
