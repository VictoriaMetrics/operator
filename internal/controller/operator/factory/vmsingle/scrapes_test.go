package vmsingle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestCreateOrUpdateScrapeConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMSingle
		cfgMutator        func(c *config.BaseOperatorConf)
		predefinedObjects []runtime.Object
		wantConfig        string
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.TODO()
		testClient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		cfg := config.MustGetBaseConfig()
		if o.cfgMutator != nil {
			defaultCfg := *cfg
			o.cfgMutator(cfg)
			defer func() {
				*config.MustGetBaseConfig() = defaultCfg
			}()
		}
		build.AddDefaults(testClient.Scheme())
		ac := getAssetsCache(ctx, testClient, o.cr)
		assert.NoError(t, createOrUpdateScrapeConfig(ctx, testClient, o.cr, nil, nil, ac))
		var expectSecret corev1.Secret
		nsn := types.NamespacedName{Namespace: o.cr.Namespace, Name: o.cr.PrefixedName()}
		assert.NoError(t, testClient.Get(ctx, nsn, &expectSecret))
		gotCfg := expectSecret.Data[scrapeGzippedFilename]
		data, err := build.GunzipConfig(gotCfg)
		assert.NoError(t, err)
		assert.Equal(t, o.wantConfig, string(data))
	}

	// complete test
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode:                 ptr.To(false),
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
		},
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
						Static: &vmv1beta1.VMProbeTargetStatic{
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
    ca_file: /etc/vm-tls/certs/default_access-creds_ca
    cert_file: /etc/vm-tls/certs/default_access-creds_cert
    key_file: /etc/vm-tls/certs/default_access-creds_key
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
    ca_file: /etc/vm-tls/certs/default_access-creds_ca
    cert_file: /etc/vm-tls/certs/default_access-creds_cert
    key_file: /etc/vm-tls/certs/default_access-creds_key
- job_name: probe/kube-system/test-vmp
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
	})

	// with missing secret references
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode:                 ptr.To(false),
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
		},
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
	})

	// with changed default config value
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode:                 ptr.To(false),
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
					ScrapeTimeout:                  "20m",
					ScrapeInterval:                 "30m",
					ExternalLabels: map[string]string{
						"externalLabelName": "externalLabelValue",
					},
					GlobalScrapeRelabelConfigs: []*vmv1beta1.RelabelConfig{
						{
							UnderScoreSourceLabels: []string{"test2"},
						},
					},
					GlobalScrapeMetricRelabelConfigs: []*vmv1beta1.RelabelConfig{
						{
							UnderScoreSourceLabels: []string{"test1"},
						},
					},
				},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VMServiceScrape.EnforceEndpointSlices = true
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
  scrape_interval: 30m
  external_labels:
    externalLabelName: externalLabelValue
    prometheus: default/test
  scrape_timeout: 20m
  metric_relabel_configs:
  - source_labels:
    - test1
  relabel_configs:
  - source_labels:
    - test2
scrape_configs:
- job_name: serviceScrape/default/test-vms/0
  kubernetes_sd_configs:
  - role: endpointslice
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
  - role: endpointslice
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
	})

	// with oauth2 tls config
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode:                 ptr.To(false),
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
      ca_file: /etc/vm-tls/certs/default_configmap_tls-default_CA
      cert_file: /etc/vm-tls/certs/default_tls-auth_CERT
      key_file: /etc/vm-tls/certs/default_tls-auth_SECRET_KEY
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
      ca_file: /etc/vm-tls/certs/default_configmap_tls-default_CA
      cert_file: /etc/vm-tls/certs/default_tls-auth_CERT
      key_file: /etc/vm-tls/certs/default_tls-auth_SECRET_KEY
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
      ca_file: /etc/vm-tls/certs/default_configmap_tls-default_CA
      cert_file: /etc/vm-tls/certs/default_tls-auth_CERT
      key_file: /etc/vm-tls/certs/default_tls-auth_SECRET_KEY
`,
	})

	// with invalid objects syntax
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "select-all",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode:     ptr.To(false),
					SelectAllByDefault: true,
					CommonScrapeSecurityEnforcements: vmv1beta1.CommonScrapeSecurityEnforcements{
						ArbitraryFSAccessThroughSMs: vmv1beta1.ArbitraryFSAccessThroughSMsConfig{
							Deny: true,
						},
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
						Kubernetes: []*vmv1beta1.VMProbeTargetKubernetes{
							{
								Role: "ingress",
								Selector: *metav1.SetAsLabelSelector(map[string]string{
									"alb.ingress.kubernetes.io/tags": "Environment=devl",
								}),
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
	})

	// with partial missing refs
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "select-all",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode:     ptr.To(false),
					SelectAllByDefault: true,
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
	})

	// with scrape classes
	f(opts{

		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "scrape-classes",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode:     ptr.To(false),
					SelectAllByDefault: true,
					ScrapeClasses: []vmv1beta1.ScrapeClass{
						{
							Name:    "default",
							Default: ptr.To(true),
							EndpointAuth: vmv1beta1.EndpointAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CA:         vmv1beta1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{Key: "CA", LocalObjectReference: corev1.LocalObjectReference{Name: "tls-default"}}},
									ServerName: "my-server",
								},
							},
							AttachMetadata: &vmv1beta1.AttachMetadata{Node: ptr.To(true)},
							EndpointRelabelings: vmv1beta1.EndpointRelabelings{
								MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{},
								RelabelConfigs:       []*vmv1beta1.RelabelConfig{},
							},
						},

						{
							Name: "with-oauth2",
							EndpointAuth: vmv1beta1.EndpointAuth{
								OAuth2: &vmv1beta1.OAuth2{
									TokenURL:         "http://some-other",
									ClientSecretFile: "/path/to/file",
									ClientID: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											Key:                  "CLIENT_ID",
											LocalObjectReference: corev1.LocalObjectReference{Name: "oauth2-access"},
										},
									},
								},
							},
						},
						{
							Name: "with-basic-auth",
							EndpointAuth: vmv1beta1.EndpointAuth{
								BasicAuth: &vmv1beta1.BasicAuth{
									Username: corev1.SecretKeySelector{
										Key:                  "username",
										LocalObjectReference: corev1.LocalObjectReference{Name: "basic-auth"},
									},
									PasswordFile: "/path/to/file",
								},
							},
						},
						{
							Name: "with-oauth2-tls",
							EndpointAuth: vmv1beta1.EndpointAuth{
								OAuth2: &vmv1beta1.OAuth2{
									TokenURL:         "http://some",
									ClientSecretFile: "/path/to/file",
									ClientID: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											Key:                  "CLIENT_ID",
											LocalObjectReference: corev1.LocalObjectReference{Name: "oauth2-access"},
										},
									},
									TLSConfig: &vmv1beta1.TLSConfig{
										CA:        vmv1beta1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{Key: "CA", LocalObjectReference: corev1.LocalObjectReference{Name: "tls-default"}}},
										Cert:      vmv1beta1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{Key: "CERT", LocalObjectReference: corev1.LocalObjectReference{Name: "tls-auth"}}},
										KeySecret: &corev1.SecretKeySelector{Key: "CERT", LocalObjectReference: corev1.LocalObjectReference{Name: "tls-auth"}},
									},
								},
							},
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMPodScrape{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "class",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMPodScrapeSpec{
					ScrapeClassName:     ptr.To("default"),
					PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{{Port: ptr.To("some")}},
				},
			},
			&vmv1beta1.VMPodScrape{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "class-oauth2-tls",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMPodScrapeSpec{
					ScrapeClassName:     ptr.To("with-oauth2-tls"),
					PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{{Port: ptr.To("some-other")}},
				},
			},
			&vmv1beta1.VMNodeScrape{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "class-oauth2",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMNodeScrapeSpec{
					ScrapeClassName: ptr.To("with-basic-auth"),
					Port:            "8035",
				},
			},
			&vmv1beta1.VMStaticScrape{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "class-oauth2",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMStaticScrapeSpec{
					ScrapeClassName: ptr.To("with-oauth2"),
					TargetEndpoints: []*vmv1beta1.TargetEndpoint{
						{
							Targets: []string{"host-1", "host-2"},
						},
					},
				},
			},
			&vmv1beta1.VMScrapeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "with-own",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMScrapeConfigSpec{
					ConsulSDConfigs: []vmv1beta1.ConsulSDConfig{
						{
							Server: "some",
							TLSConfig: &vmv1beta1.TLSConfig{
								CAFile:     "/some/other/path",
								CertFile:   "/some/other/cert",
								KeyFile:    "/some/other/key",
								ServerName: "my-name",
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
		},
		wantConfig: `global:
  scrape_interval: 30s
  external_labels:
    prometheus: default/scrape-classes
scrape_configs:
- job_name: podScrape/default/class/0
  kubernetes_sd_configs:
  - role: pod
    attach_metadata:
      node: true
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
    regex: some
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
    replacement: default/class
  - target_label: endpoint
    replacement: some
  tls_config:
    ca_file: /etc/vm-tls/certs/default_configmap_tls-default_CA
    server_name: my-server
- job_name: podScrape/default/class-oauth2-tls/0
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
    regex: some-other
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
    replacement: default/class-oauth2-tls
  - target_label: endpoint
    replacement: some-other
  oauth2:
    client_id: data
    client_secret_file: /path/to/file
    token_url: http://some
    tls_config:
      ca_file: /etc/vm-tls/certs/default_configmap_tls-default_CA
      cert_file: /etc/vm-tls/certs/default_tls-auth_CERT
      key_file: /etc/vm-tls/certs/default_tls-auth_CERT
- job_name: staticScrape/default/class-oauth2/0
  static_configs:
  - targets:
    - host-1
    - host-2
  honor_labels: false
  relabel_configs: []
  oauth2:
    client_id: data
    client_secret_file: /path/to/file
    token_url: http://some-other
- job_name: scrapeConfig/default/with-own
  honor_labels: false
  relabel_configs: []
  tls_config:
    ca_file: /etc/vm-tls/certs/default_configmap_tls-default_CA
    server_name: my-server
  consul_sd_configs:
  - server: some
    tls_config:
      ca_file: /some/other/path
      cert_file: /some/other/cert
      key_file: /some/other/key
      server_name: my-name
`,
	})

	// oauth2 with partial fields set
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "with-oauth2",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode:     ptr.To(false),
					SelectAllByDefault: true,
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMPodScrape{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "with-oauth2-simple",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMPodScrapeSpec{
					PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
						{
							Port: ptr.To("8085"),
							EndpointAuth: vmv1beta1.EndpointAuth{
								OAuth2: &vmv1beta1.OAuth2{
									TokenURL: "http://some-url",
								},
							},
						},
						{
							Port: ptr.To("8085"),
							EndpointAuth: vmv1beta1.EndpointAuth{
								OAuth2: &vmv1beta1.OAuth2{
									TokenURL:         "http://some-other",
									ClientSecretFile: "/path/to/file",
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
    prometheus: default/with-oauth2
scrape_configs:
- job_name: podScrape/default/with-oauth2-simple/0
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
    regex: "8085"
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
    replacement: default/with-oauth2-simple
  - target_label: endpoint
    replacement: "8085"
  oauth2:
    token_url: http://some-url
- job_name: podScrape/default/with-oauth2-simple/1
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
    regex: "8085"
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
    replacement: default/with-oauth2-simple
  - target_label: endpoint
    replacement: "8085"
  oauth2:
    client_secret_file: /path/to/file
    token_url: http://some-other
`,
	})
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
    prometheus: default/vmsingle
scrape_configs: []
`
		ctx := context.TODO()
		testClient := k8stools.GetTestClientWithObjects([]runtime.Object{so})
		build.AddDefaults(testClient.Scheme())

		cr := &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmsingle",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode:     ptr.To(false),
					SelectAllByDefault: true,
				},
			},
		}
		ac := getAssetsCache(ctx, testClient, cr)
		if err := createOrUpdateScrapeConfig(ctx, testClient, cr, nil, nil, ac); err != nil {
			t.Errorf("createOrUpdateScrapeConfig() error = %s", err)
		}
		var configSecret corev1.Secret
		assert.NoError(t, testClient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &configSecret))

		gotCfg := configSecret.Data[scrapeGzippedFilename]
		data, err := build.GunzipConfig(gotCfg)
		assert.NoError(t, err)
		assert.Equal(t, expectedConfig, string(data))

		assert.NoError(t, testClient.Get(ctx, types.NamespacedName{Name: so.GetName(), Namespace: so.GetNamespace()}, so))
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
				Kubernetes: []*vmv1beta1.VMProbeTargetKubernetes{
					{
						Role:     "ingress",
						Selector: *metav1.SetAsLabelSelector(map[string]string{"alb.ingress.kubernetes.io/tags": "Environment=devl"}),
					},
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

	// missing scrapeClass
	f(&vmv1beta1.VMServiceScrape{
		ObjectMeta: commonMeta,
		Spec: vmv1beta1.VMServiceScrapeSpec{
			ScrapeClassName: ptr.To("non-exist"),
			Endpoints: []vmv1beta1.Endpoint{
				{
					Port: "9090",
				},
			},
		},
	})

	// missing scrapeClass
	f(&vmv1beta1.VMPodScrape{
		ObjectMeta: commonMeta,
		Spec: vmv1beta1.VMPodScrapeSpec{
			ScrapeClassName: ptr.To("non-exist"),
			PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
				{
					Port: ptr.To("9090"),
				},
			},
		},
	},
	)

}
