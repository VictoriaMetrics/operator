package vmagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_generateProbeConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		sc                *vmv1beta1.VMProbe
		want              string
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ac := getAssetsCache(ctx, fclient, o.cr)
		got, err := generateProbeConfig(ctx, o.cr, o.sc, 0, nil, ac, o.cr.Spec.VMAgentSecurityEnforcements)
		if err != nil {
			t.Errorf("cannot generate ProbeConfig, err: %e", err)
			return
		}
		gotBytes, err := yaml.Marshal(got)
		if err != nil {
			t.Errorf("cannot decode probe config, it must be in yaml format: %e", err)
			return
		}
		assert.Equal(t, o.want, string(gotBytes))
	}

	// generate static config
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMProbe{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "static-probe",
			},
			Spec: vmv1beta1.VMProbeSpec{
				Module: "http",
				VMProberSpec: vmv1beta1.VMProberSpec{
					URL:    "blackbox-monitor:9115",
					Scheme: "https",
					Path:   "/probe2",
				},
				Targets: vmv1beta1.VMProbeTargets{
					StaticConfig: &vmv1beta1.VMProbeTargetStaticConfig{
						Targets: []string{"host-1", "host-2"},
						Labels:  map[string]string{"label1": "value1"},
					},
				},
			},
		},
		want: `job_name: probe/default/static-probe/0
honor_labels: false
metrics_path: /probe2
params:
  module:
  - http
scheme: https
static_configs:
- targets:
  - host-1
  - host-2
  labels:
    label1: value1
relabel_configs:
- source_labels:
  - __address__
  target_label: __param_target
- source_labels:
  - __param_target
  target_label: instance
- target_label: __address__
  replacement: blackbox-monitor:9115
`,
	})

	// with ingress discover
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMProbe{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "probe-ingress",
				Namespace: "monitor",
			},
			Spec: vmv1beta1.VMProbeSpec{
				Module:       "http200",
				VMProberSpec: vmv1beta1.VMProberSpec{URL: "blackbox:9115"},
				Targets: vmv1beta1.VMProbeTargets{
					Ingress: &vmv1beta1.ProbeTargetIngress{
						NamespaceSelector: vmv1beta1.NamespaceSelector{},
						RelabelConfigs: []*vmv1beta1.RelabelConfig{
							{
								SourceLabels: []string{"label1"},
								TargetLabel:  "api",
								Action:       "replacement",
							},
						},
					},
				},
			},
		},
		want: `job_name: probe/monitor/probe-ingress/0
honor_labels: false
metrics_path: /probe
params:
  module:
  - http200
kubernetes_sd_configs:
- role: ingress
  namespaces:
    names:
    - monitor
relabel_configs:
- source_labels:
  - __address__
  separator: ;
  regex: (.*)
  target_label: __tmp_ingress_address
  replacement: $1
  action: replace
- source_labels:
  - __meta_kubernetes_ingress_scheme
  - __address__
  - __meta_kubernetes_ingress_path
  separator: ;
  regex: (.+);(.+);(.+)
  target_label: __param_target
  replacement: ${1}://${2}${3}
  action: replace
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_ingress_name
  target_label: ingress
- source_labels:
  - label1
  target_label: api
  action: replacement
- source_labels:
  - __param_target
  target_label: instance
- target_label: __address__
  replacement: blackbox:9115
`,
	})

	// generate with vm params
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMProbe{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "static-probe",
			},
			Spec: vmv1beta1.VMProbeSpec{
				Module: "http",
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					FollowRedirects: ptr.To(true),
					ScrapeInterval:  "10s",
					Interval:        "5s",
					Params: map[string][]string{
						"timeout": {"10s"},
					},
					ScrapeTimeout: "15s",
					VMScrapeParams: &vmv1beta1.VMScrapeParams{
						StreamParse: ptr.To(false),
						ProxyClientConfig: &vmv1beta1.ProxyAuth{
							TLSConfig: &vmv1beta1.TLSConfig{
								CA: vmv1beta1.SecretOrConfigMap{
									ConfigMap: &corev1.ConfigMapKeySelector{
										Key: "ca",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "tls-secret",
										},
									},
								},
								Cert: vmv1beta1.SecretOrConfigMap{
									Secret: &corev1.SecretKeySelector{
										Key: "cert",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "tls-secret",
										},
									},
								},
								KeyFile: "/tmp/key-1",
							},
						},
					},
				},
				EndpointAuth: vmv1beta1.EndpointAuth{
					BearerTokenFile: "/tmp/some_path",
					BasicAuth: &vmv1beta1.BasicAuth{
						PasswordFile: "/tmp/some-file-ba",
					},
				},
				VMProberSpec: vmv1beta1.VMProberSpec{URL: "blackbox-monitor:9115"},
				Targets: vmv1beta1.VMProbeTargets{
					StaticConfig: &vmv1beta1.VMProbeTargetStaticConfig{
						Targets: []string{"host-1", "host-2"},
						Labels:  map[string]string{"label1": "value1"},
					},
				},
			},
		},
		want: `job_name: probe/default/static-probe/0
honor_labels: false
scrape_interval: 10s
scrape_timeout: 15s
metrics_path: /probe
follow_redirects: true
params:
  module:
  - http
  timeout:
  - 10s
static_configs:
- targets:
  - host-1
  - host-2
  labels:
    label1: value1
relabel_configs:
- source_labels:
  - __address__
  target_label: __param_target
- source_labels:
  - __param_target
  target_label: instance
- target_label: __address__
  replacement: blackbox-monitor:9115
stream_parse: false
proxy_tls_config:
  ca_file: /etc/vmagent-tls/certs/default_configmap_tls-secret_ca
  cert_file: /etc/vmagent-tls/certs/default_tls-secret_cert
  key_file: /tmp/key-1
bearer_token_file: /tmp/some_path
basic_auth:
  password_file: /tmp/some-file-ba
`,
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"key":  []byte("key-value"),
					"cert": []byte("cert-value"),
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca": "ca-value",
				},
			},
		},
	})

	// with ingress selectors
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				EnableKubernetesAPISelectors: true,
			},
		},
		sc: &vmv1beta1.VMProbe{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "probe-ingress",
				Namespace: "monitor",
			},
			Spec: vmv1beta1.VMProbeSpec{
				Module:       "http200",
				VMProberSpec: vmv1beta1.VMProberSpec{URL: "blackbox:9115"},
				Targets: vmv1beta1.VMProbeTargets{
					Ingress: &vmv1beta1.ProbeTargetIngress{
						NamespaceSelector: vmv1beta1.NamespaceSelector{},
						Selector: *metav1.SetAsLabelSelector(map[string]string{
							"ingress-class": "ngin",
						}),
						RelabelConfigs: []*vmv1beta1.RelabelConfig{
							{
								SourceLabels: []string{"label1"},
								TargetLabel:  "api",
								Action:       "replacement",
							},
						},
					},
				},
			},
		},
		want: `job_name: probe/monitor/probe-ingress/0
honor_labels: false
metrics_path: /probe
params:
  module:
  - http200
kubernetes_sd_configs:
- role: ingress
  namespaces:
    names:
    - monitor
  selectors:
  - role: ingress
    label: ingress-class=ngin
relabel_configs:
- source_labels:
  - __address__
  separator: ;
  regex: (.*)
  target_label: __tmp_ingress_address
  replacement: $1
  action: replace
- source_labels:
  - __meta_kubernetes_ingress_scheme
  - __address__
  - __meta_kubernetes_ingress_path
  separator: ;
  regex: (.+);(.+);(.+)
  target_label: __param_target
  replacement: ${1}://${2}${3}
  action: replace
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_ingress_name
  target_label: ingress
- source_labels:
  - label1
  target_label: api
  action: replacement
- source_labels:
  - __param_target
  target_label: instance
- target_label: __address__
  replacement: blackbox:9115
`,
	})
}
