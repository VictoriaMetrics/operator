package vmagent

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
	f := func(cr *vmv1beta1.VMAgent, sc *vmv1beta1.VMServiceScrape, ep vmv1beta1.Endpoint, i int, want string, predefinedObjects []runtime.Object) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		ac := getAssetsCache(ctx, fclient, cr)
		got, err := generateServiceScrapeConfig(ctx, cr, sc, ep, i, ac)
		if err != nil {
			t.Errorf("cannot generate ServiceScrapeConfig: %e", err)
			return
		}
		gotBytes, err := yaml.Marshal(got)
		if err != nil {
			t.Errorf("cannot marshal ServiceScrapeConfig to yaml: %e", err)
			return
		}
		assert.Equal(t, want, string(gotBytes))
	}

	// generate simple config
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			VMAgentSecurityEnforcements: vmv1beta1.VMAgentSecurityEnforcements{
				OverrideHonorLabels:      false,
				OverrideHonorTimestamps:  false,
				IgnoreNamespaceSelectors: false,
				EnforcedNamespaceLabel:   "",
			},
		},
	}, &vmv1beta1.VMServiceScrape{
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
						BearerTokenFile: "/var/run/token",
					},
				},
			},
		},
	}, vmv1beta1.Endpoint{
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
	}, 0, `job_name: serviceScrape/default/test-scrape/0
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
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/token
`, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tls-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca": []byte("ca-value"),
			},
		},
	})

	// generate config with scrape interval limit
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			MaxScrapeInterval: ptr.To("40m"),
		},
	}, &vmv1beta1.VMServiceScrape{
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
						BearerTokenFile: "/var/run/token",
					},
				},
			},
		},
	}, vmv1beta1.Endpoint{
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
	}, 0, `job_name: serviceScrape/default/test-scrape/0
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
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/token
`, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tls-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca": []byte("ca-value"),
			},
		},
	})

	// generate config with scrape interval limit - reach min
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			MinScrapeInterval: ptr.To("1m"),
		},
	}, &vmv1beta1.VMServiceScrape{
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
						BearerTokenFile: "/var/run/token",
					},
				},
			},
		},
	}, vmv1beta1.Endpoint{
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
	}, 0, `job_name: serviceScrape/default/test-scrape/0
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
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/token
`, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tls-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca": []byte("ca-value"),
			},
		},
	})

	// config with discovery role endpointslices
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
	}, &vmv1beta1.VMServiceScrape{
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
						BearerTokenFile: "/var/run/token",
					},
				},
			},
		},
	}, vmv1beta1.Endpoint{
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
	}, 0, `job_name: serviceScrape/default/test-scrape/0
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
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/token
`, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tls-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca": []byte("ca-value"),
			},
		},
	})

	// config with discovery role services
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
	}, &vmv1beta1.VMServiceScrape{
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
						BearerTokenFile: "/var/run/token",
					},
				},
			},
		},
	}, vmv1beta1.Endpoint{
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
			BearerTokenFile: "/var/run/token",
		},
	}, 0, `job_name: serviceScrape/default/test-scrape/0
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
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/token
`, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tls-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca": []byte("ca-value"),
			},
		},
	})

	// bad discovery role service without port name
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
	}, &vmv1beta1.VMServiceScrape{
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
	}, vmv1beta1.Endpoint{
		TargetPort: func() *intstr.IntOrString {
			v := intstr.FromString("8080")
			return &v
		}(),
	}, 0, `job_name: serviceScrape/default/test-scrape/0
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
`, nil)

	// config with tls insecure
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
	}, &vmv1beta1.VMServiceScrape{
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
	}, vmv1beta1.Endpoint{
		Port: "8080",
		EndpointAuth: vmv1beta1.EndpointAuth{
			TLSConfig: &vmv1beta1.TLSConfig{
				InsecureSkipVerify: true,
			},
			BearerTokenFile: "/var/run/token",
		},
	}, 0, `job_name: serviceScrape/default/test-scrape/0
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
`, nil)

	// complete config
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
	}, &vmv1beta1.VMServiceScrape{
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
	}, vmv1beta1.Endpoint{
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
			TLSConfig: &vmv1beta1.TLSConfig{
				InsecureSkipVerify: true,
			},
			BearerTokenFile: "/var/run/token",
			BearerTokenSecret: &corev1.SecretKeySelector{
				Key:                  "bearer",
				LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
			},
		},
		Port: "8080",
	}, 0, `job_name: serviceScrape/default/test-scrape/0
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
    ca_file: /etc/vmagent-tls/certs/default_configmap_tls-cm_ca
    cert_file: /etc/vmagent-tls/certs/default_tls_key
    key_file: /etc/vmagent-tls/certs/default_tls_cert
`, []runtime.Object{
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
	})

	// with templateRelabel
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			ServiceScrapeRelabelTemplate: []*vmv1beta1.RelabelConfig{
				{
					TargetLabel:  "node",
					SourceLabels: []string{"__meta_kubernetes_node_name"},
					Regex:        []string{".+"},
				},
			},
		},
	}, &vmv1beta1.VMServiceScrape{
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
						BearerTokenFile: "/var/run/token",
					},
				},
			},
		},
	}, vmv1beta1.Endpoint{
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
	}, 0, `job_name: serviceScrape/default/test-scrape/0
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
  ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
bearer_token_file: /var/run/token
`, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tls-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca": []byte("ca-value"),
			},
		},
	})

	// with selectors endpoints
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			EnableKubernetesAPISelectors: true,
			APIServerConfig: &vmv1beta1.APIServerConfig{
				Host: "default-k8s-host",
			},
			VMAgentSecurityEnforcements: vmv1beta1.VMAgentSecurityEnforcements{
				OverrideHonorLabels:      false,
				OverrideHonorTimestamps:  false,
				IgnoreNamespaceSelectors: false,
				EnforcedNamespaceLabel:   "",
			},
		},
	}, &vmv1beta1.VMServiceScrape{
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
			Selector: *metav1.SetAsLabelSelector(map[string]string{
				"env":  "dev",
				"dc":   "prod",
				"team": "go",
			}),
		},
	}, vmv1beta1.Endpoint{
		AttachMetadata: vmv1beta1.AttachMetadata{
			Node: ptr.To(true),
		},
		Port: "8080",
	}, 0, `job_name: serviceScrape/default/test-scrape/0
kubernetes_sd_configs:
- role: endpoints
  attach_metadata:
    node: true
  namespaces:
    names:
    - default
  api_server: default-k8s-host
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
`, nil)

	// with selectors services
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			EnableKubernetesAPISelectors: true,
			APIServerConfig: &vmv1beta1.APIServerConfig{
				Host: "default-k8s-host",
			},
		},
	}, &vmv1beta1.VMServiceScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-scrape",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMServiceScrapeSpec{
			DiscoveryRole: kubernetesSDRoleService,
			Endpoints: []vmv1beta1.Endpoint{
				{
					Port: "8080",
				},
			},
			Selector: *metav1.SetAsLabelSelector(map[string]string{
				"env": "dev",
			}),
		},
	}, vmv1beta1.Endpoint{
		AttachMetadata: vmv1beta1.AttachMetadata{
			Node: ptr.To(true),
		},
		Port: "8080",
	}, 0, `job_name: serviceScrape/default/test-scrape/0
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
`, nil)
}
