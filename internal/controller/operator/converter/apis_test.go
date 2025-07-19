package converter

import (
	"reflect"
	"testing"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

func TestConvertTlsConfig(t *testing.T) {
	type opts struct {
		cfg  *promv1.TLSConfig
		want *vmv1beta1.TLSConfig
	}
	f := func(opts opts) {
		t.Helper()
		got := ConvertTLSConfig(opts.cfg)
		if got.KeyFile != opts.want.KeyFile || got.CertFile != opts.want.CertFile || got.CAFile != opts.want.CAFile {
			t.Errorf("ConvertTlsConfig() = \n%v, \nwant \n%v", got, opts.want)
		}
	}

	// replace prom secret path
	o := opts{
		cfg: &promv1.TLSConfig{
			CAFile:   "/etc/prom_add/ca",
			CertFile: "/etc/prometheus/secrets/cert.crt",
			KeyFile:  "/etc/prometheus/configmaps/key.pem",
		},
		want: &vmv1beta1.TLSConfig{
			CAFile:   "/etc/prom_add/ca",
			CertFile: "/etc/vm/secrets/cert.crt",
			KeyFile:  "/etc/vm/configs/key.pem",
		},
	}
	f(o)

	// with server name and insecure
	o = opts{
		cfg: &promv1.TLSConfig{
			CAFile:        "/etc/prom_add/ca",
			CertFile:      "/etc/prometheus/secrets/cert.crt",
			KeyFile:       "/etc/prometheus/configmaps/key.pem",
			SafeTLSConfig: promv1.SafeTLSConfig{ServerName: ptr.To("some-hostname"), InsecureSkipVerify: ptr.To(true)},
		},
		want: &vmv1beta1.TLSConfig{
			CAFile:             "/etc/prom_add/ca",
			CertFile:           "/etc/vm/secrets/cert.crt",
			KeyFile:            "/etc/vm/configs/key.pem",
			ServerName:         "some-hostname",
			InsecureSkipVerify: true,
		},
	}
	f(o)
}

func TestConvertRelabelConfig(t *testing.T) {
	type opts struct {
		cfg  []promv1.RelabelConfig
		want []*vmv1beta1.RelabelConfig
	}
	f := func(opts opts) {
		t.Helper()
		got := ConvertRelabelConfig(opts.cfg)
		if len(got) != len(opts.want) {
			t.Fatalf("len of relabelConfigs mismatch, want: %d, got %d", len(opts.want), len(got))
		}
		for i, wantRelabelConfig := range opts.want {
			if !reflect.DeepEqual(*wantRelabelConfig, *got[i]) {
				t.Fatalf("ConvertRelabelConfig() = %v, want %v", *got[i], *wantRelabelConfig)
			}
		}
	}

	// test empty cfg
	f(opts{})

	// 1 relabel cfg rule
	o := opts{
		cfg: []promv1.RelabelConfig{
			{
				Action:       "drop",
				SourceLabels: []promv1.LabelName{"__address__"},
			},
		},
		want: []*vmv1beta1.RelabelConfig{
			{
				Action:       "drop",
				SourceLabels: []string{"__address__"},
			},
		},
	}
	f(o)

	// unsupported config
	o = opts{
		cfg: []promv1.RelabelConfig{
			{
				Action: "drop",
			},
			{
				Action:       "keep",
				SourceLabels: []promv1.LabelName{"__address__"},
			},
		},
		want: []*vmv1beta1.RelabelConfig{
			{
				Action:       "keep",
				SourceLabels: []string{"__address__"},
			},
		},
	}
	f(o)
}

func TestConvertEndpoint(t *testing.T) {
	type opts struct {
		cfg  []promv1.Endpoint
		want []vmv1beta1.Endpoint
	}
	f := func(opts opts) {
		t.Helper()
		if got := convertEndpoint(opts.cfg); !reflect.DeepEqual(got, opts.want) {
			t.Errorf("ConvertEndpoint() \ngot:  \n%v\n, \nwant: \n%v", got, opts.want)
		}
	}

	// convert endpoint with relabel config
	o := opts{
		cfg: []promv1.Endpoint{
			{
				Port: "9100",
				Path: "/metrics",
				RelabelConfigs: []promv1.RelabelConfig{
					{
						Action:       "drop",
						SourceLabels: []promv1.LabelName{"__meta__instance"},
					},
					{
						Action: "keep",
					},
				},
			},
		},
		want: []vmv1beta1.Endpoint{
			{
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					Path: "/metrics",
				},
				Port: "9100",
				EndpointRelabelings: vmv1beta1.EndpointRelabelings{
					RelabelConfigs: []*vmv1beta1.RelabelConfig{
						{
							Action:       "drop",
							SourceLabels: []string{"__meta__instance"},
						},
					},
				},
			},
		},
	}
	f(o)
}

func TestConvertServiceMonitor(t *testing.T) {
	type opts struct {
		cfg  *promv1.ServiceMonitor
		want vmv1beta1.VMServiceScrape
	}
	f := func(opts opts) {
		t.Helper()
		got := ConvertServiceMonitor(opts.cfg, &config.BaseOperatorConf{
			FilterPrometheusConverterLabelPrefixes:      []string{"app.kubernetes", "helm.sh"},
			FilterPrometheusConverterAnnotationPrefixes: []string{"another-annotation-filter", "app.kubernetes"},
		})
		if !reflect.DeepEqual(*got, opts.want) {
			t.Errorf("ConvertServiceMonitor() got = \n%v, \nwant \n%v", got, opts.want)
		}
	}

	// with metricsRelabelConfig
	o := opts{
		cfg: &promv1.ServiceMonitor{
			Spec: promv1.ServiceMonitorSpec{
				Endpoints: []promv1.Endpoint{
					{
						MetricRelabelConfigs: []promv1.RelabelConfig{
							{
								Action:       "drop",
								SourceLabels: []promv1.LabelName{"__meta__instance"},
							},
						},
					},
				},
			},
		},
		want: vmv1beta1.VMServiceScrape{
			Spec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
						EndpointRelabelings: vmv1beta1.EndpointRelabelings{
							MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{
								{
									Action:       "drop",
									SourceLabels: []string{"__meta__instance"},
								},
							},
						},
					},
				},
			},
		},
	}
	f(o)

	// with label and annotations filter
	o = opts{
		cfg: &promv1.ServiceMonitor{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      map[string]string{"helm.sh/release": "prod", "keep-label": "value"},
				Annotations: map[string]string{"app.kubernetes.io/": "release"},
			},
			Spec: promv1.ServiceMonitorSpec{
				Endpoints: []promv1.Endpoint{
					{
						MetricRelabelConfigs: []promv1.RelabelConfig{
							{
								Action:       "drop",
								SourceLabels: []promv1.LabelName{"__meta__instance"},
							},
						},
					},
				},
			},
		},
		want: vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"keep-label": "value"},
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
						EndpointRelabelings: vmv1beta1.EndpointRelabelings{
							MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{
								{
									Action:       "drop",
									SourceLabels: []string{"__meta__instance"},
								},
							},
						},
					},
				},
			},
		},
	}
	f(o)
}

func TestConvertPodEndpoints(t *testing.T) {
	type opts struct {
		cfg  []promv1.PodMetricsEndpoint
		want []vmv1beta1.PodMetricsEndpoint
	}
	f := func(opts opts) {
		t.Helper()
		if got := convertPodEndpoints(opts.cfg); !reflect.DeepEqual(got, opts.want) {
			t.Errorf("ConvertPodEndpoints() = %v, want %v", got, opts.want)
		}
	}

	// with partial tls config
	o := opts{
		cfg: []promv1.PodMetricsEndpoint{{
			BearerTokenSecret: corev1.SecretKeySelector{},
			TLSConfig: &promv1.SafeTLSConfig{
				CA: promv1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{
					Key: "ca",
				}},
			},
		}},
		want: []vmv1beta1.PodMetricsEndpoint{{
			EndpointAuth: vmv1beta1.EndpointAuth{
				TLSConfig: &vmv1beta1.TLSConfig{
					CA: vmv1beta1.SecretOrConfigMap{
						ConfigMap: &corev1.ConfigMapKeySelector{
							Key: "ca",
						},
					},
				},
			},
		}},
	}
	f(o)

	// with tls config
	o = opts{
		cfg: []promv1.PodMetricsEndpoint{{
			BearerTokenSecret: corev1.SecretKeySelector{},
			TLSConfig: &promv1.SafeTLSConfig{
				InsecureSkipVerify: ptr.To(true),
				ServerName:         ptr.To("some-srv"),
				CA: promv1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{
					Key: "ca",
				}},
			},
		}},
		want: []vmv1beta1.PodMetricsEndpoint{{
			EndpointAuth: vmv1beta1.EndpointAuth{
				TLSConfig: &vmv1beta1.TLSConfig{
					InsecureSkipVerify: true,
					ServerName:         "some-srv",
					CA: vmv1beta1.SecretOrConfigMap{
						ConfigMap: &corev1.ConfigMapKeySelector{
							Key: "ca",
						},
					},
				},
			},
		}},
	}
	f(o)

	// with basic auth and bearer
	o = opts{
		cfg: []promv1.PodMetricsEndpoint{{
			BearerTokenSecret: corev1.SecretKeySelector{Key: "bearer"},
			BasicAuth: &promv1.BasicAuth{
				Username: corev1.SecretKeySelector{Key: "username"},
				Password: corev1.SecretKeySelector{Key: "password"},
			},
		}},
		want: []vmv1beta1.PodMetricsEndpoint{{
			EndpointAuth: vmv1beta1.EndpointAuth{
				BearerTokenSecret: &corev1.SecretKeySelector{
					Key: "bearer",
				},
				BasicAuth: &vmv1beta1.BasicAuth{
					Username: corev1.SecretKeySelector{
						Key: "username",
					},
					Password: corev1.SecretKeySelector{Key: "password"},
				},
			},
		}},
	}
	f(o)
}

func TestConvertProbe(t *testing.T) {
	type opts struct {
		cfg  *promv1.Probe
		want vmv1beta1.VMProbe
	}
	f := func(opts opts) {
		got := ConvertProbe(opts.cfg, &config.BaseOperatorConf{
			FilterPrometheusConverterLabelPrefixes:      []string{"helm.sh"},
			FilterPrometheusConverterAnnotationPrefixes: []string{"app.kubernetes"},
		})

		if !reflect.DeepEqual(*got, opts.want) {
			t.Errorf("ConvertProbe() got = \n%v, \nwant \n%v", got, opts.want)
		}
	}

	// with static config
	o := opts{
		cfg: &promv1.Probe{
			Spec: promv1.ProbeSpec{
				ProberSpec: promv1.ProberSpec{
					ProxyURL: "http://proxy.com",
				},
				Targets: promv1.ProbeTargets{
					StaticConfig: &promv1.ProbeTargetStaticConfig{
						Targets: []string{"target-1", "target-2"},
						Labels: map[string]string{
							"l1": "v1",
							"l2": "v2",
						},
						RelabelConfigs: []promv1.RelabelConfig{
							{
								Action:       "drop",
								SourceLabels: []promv1.LabelName{"__address__"},
							},
						},
					},
				},
			},
		},
		want: vmv1beta1.VMProbe{
			Spec: vmv1beta1.VMProbeSpec{
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					ProxyURL: ptr.To("http://proxy.com"),
				},
				Targets: vmv1beta1.VMProbeTargets{
					StaticConfig: &vmv1beta1.VMProbeTargetStaticConfig{
						Targets: []string{"target-1", "target-2"},
						Labels: map[string]string{
							"l1": "v1",
							"l2": "v2",
						},
						RelabelConfigs: []*vmv1beta1.RelabelConfig{
							{
								Action:       "drop",
								SourceLabels: []string{"__address__"},
							},
						},
					},
				},
			},
		},
	}
	f(o)

	// with ingress config
	o = opts{
		cfg: &promv1.Probe{
			Spec: promv1.ProbeSpec{
				Targets: promv1.ProbeTargets{
					Ingress: &promv1.ProbeTargetIngress{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "test",
							},
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "key",
									Operator: "op",
									Values:   []string{"v1", "v2"},
								},
							},
						},
						NamespaceSelector: promv1.NamespaceSelector{
							MatchNames: []string{"test-ns"},
						},
						RelabelConfigs: []promv1.RelabelConfig{
							{
								Action:       "keep",
								SourceLabels: []promv1.LabelName{"__address__"},
							},
						},
					},
				},
			},
		},
		want: vmv1beta1.VMProbe{
			Spec: vmv1beta1.VMProbeSpec{
				Targets: vmv1beta1.VMProbeTargets{
					Ingress: &vmv1beta1.ProbeTargetIngress{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "test",
							},
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "key",
									Operator: "op",
									Values:   []string{"v1", "v2"},
								},
							},
						},
						NamespaceSelector: vmv1beta1.NamespaceSelector{
							MatchNames: []string{"test-ns"},
						},
						RelabelConfigs: []*vmv1beta1.RelabelConfig{
							{
								Action:       "keep",
								SourceLabels: []string{"__address__"},
							},
						},
					},
				},
			},
		},
	}
	f(o)
}
