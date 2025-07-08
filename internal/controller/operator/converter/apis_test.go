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
	f := func(cfg *promv1.TLSConfig, want *vmv1beta1.TLSConfig) {
		t.Helper()
		got := ConvertTLSConfig(cfg)
		if got.KeyFile != want.KeyFile || got.CertFile != want.CertFile || got.CAFile != want.CAFile {
			t.Errorf("ConvertTlsConfig() = \n%v, \nwant \n%v", got, want)
		}
	}

	// replace prom secret path
	f(&promv1.TLSConfig{
		CAFile:   "/etc/prom_add/ca",
		CertFile: "/etc/prometheus/secrets/cert.crt",
		KeyFile:  "/etc/prometheus/configmaps/key.pem",
	}, &vmv1beta1.TLSConfig{
		CAFile:   "/etc/prom_add/ca",
		CertFile: "/etc/vm/secrets/cert.crt",
		KeyFile:  "/etc/vm/configs/key.pem",
	})

	// with server name and insecure
	f(&promv1.TLSConfig{
		CAFile:        "/etc/prom_add/ca",
		CertFile:      "/etc/prometheus/secrets/cert.crt",
		KeyFile:       "/etc/prometheus/configmaps/key.pem",
		SafeTLSConfig: promv1.SafeTLSConfig{ServerName: ptr.To("some-hostname"), InsecureSkipVerify: ptr.To(true)},
	}, &vmv1beta1.TLSConfig{
		CAFile:             "/etc/prom_add/ca",
		CertFile:           "/etc/vm/secrets/cert.crt",
		KeyFile:            "/etc/vm/configs/key.pem",
		ServerName:         "some-hostname",
		InsecureSkipVerify: true,
	})
}

func TestConvertRelabelConfig(t *testing.T) {
	f := func(cfg []promv1.RelabelConfig, want []*vmv1beta1.RelabelConfig) {
		t.Helper()
		got := ConvertRelabelConfig(cfg)
		if len(got) != len(want) {
			t.Fatalf("len of relabelConfigs mismatch, want: %d, got %d", len(want), len(got))
		}
		for i, wantRelabelConfig := range want {
			if !reflect.DeepEqual(*wantRelabelConfig, *got[i]) {
				t.Fatalf("ConvertRelabelConfig() = %v, want %v", *got[i], *wantRelabelConfig)
			}
		}
	}

	// test empty cfg
	f(nil, nil)

	// 1 relabel cfg rule
	f([]promv1.RelabelConfig{
		{
			Action:       "drop",
			SourceLabels: []promv1.LabelName{"__address__"},
		},
	}, []*vmv1beta1.RelabelConfig{
		{
			Action:       "drop",
			SourceLabels: []string{"__address__"},
		},
	})

	// unsupported config
	f([]promv1.RelabelConfig{
		{
			Action: "drop",
		},
		{
			Action:       "keep",
			SourceLabels: []promv1.LabelName{"__address__"},
		},
	}, []*vmv1beta1.RelabelConfig{
		{
			Action:       "keep",
			SourceLabels: []string{"__address__"},
		},
	})
}

func TestConvertEndpoint(t *testing.T) {
	f := func(cfg []promv1.Endpoint, want []vmv1beta1.Endpoint) {
		t.Helper()
		if got := convertEndpoint(cfg); !reflect.DeepEqual(got, want) {
			t.Errorf("ConvertEndpoint() \ngot:  \n%v\n, \nwant: \n%v", got, want)
		}
	}

	// convert endpoint with relabel config
	f([]promv1.Endpoint{
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
	}, []vmv1beta1.Endpoint{
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
	})
}

func TestConvertServiceMonitor(t *testing.T) {
	f := func(cfg *promv1.ServiceMonitor, want vmv1beta1.VMServiceScrape) {
		t.Helper()
		got := ConvertServiceMonitor(cfg, &config.BaseOperatorConf{
			FilterPrometheusConverterLabelPrefixes:      []string{"app.kubernetes", "helm.sh"},
			FilterPrometheusConverterAnnotationPrefixes: []string{"another-annotation-filter", "app.kubernetes"},
		})
		if !reflect.DeepEqual(*got, want) {
			t.Errorf("ConvertServiceMonitor() got = \n%v, \nwant \n%v", got, want)
		}
	}

	// with metricsRelabelConfig
	f(&promv1.ServiceMonitor{
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
	}, vmv1beta1.VMServiceScrape{
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
	})

	// with label and annotations filter
	f(&promv1.ServiceMonitor{
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
	}, vmv1beta1.VMServiceScrape{
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
	})
}

func TestConvertPodEndpoints(t *testing.T) {
	f := func(cfg []promv1.PodMetricsEndpoint, want []vmv1beta1.PodMetricsEndpoint) {
		t.Helper()
		if got := convertPodEndpoints(cfg); !reflect.DeepEqual(got, want) {
			t.Errorf("ConvertPodEndpoints() = %v, want %v", got, want)
		}
	}

	// with partial tls config
	f([]promv1.PodMetricsEndpoint{{
		BearerTokenSecret: corev1.SecretKeySelector{},
		TLSConfig: &promv1.SafeTLSConfig{
			CA: promv1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{
				Key: "ca",
			}},
		},
	}}, []vmv1beta1.PodMetricsEndpoint{{
		EndpointAuth: vmv1beta1.EndpointAuth{
			TLSConfig: &vmv1beta1.TLSConfig{
				CA: vmv1beta1.SecretOrConfigMap{
					ConfigMap: &corev1.ConfigMapKeySelector{
						Key: "ca",
					},
				},
			},
		},
	}})

	// with tls config
	f([]promv1.PodMetricsEndpoint{{
		BearerTokenSecret: corev1.SecretKeySelector{},
		TLSConfig: &promv1.SafeTLSConfig{
			InsecureSkipVerify: ptr.To(true),
			ServerName:         ptr.To("some-srv"),
			CA: promv1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{
				Key: "ca",
			}},
		},
	}}, []vmv1beta1.PodMetricsEndpoint{{
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
	}})

	// with basic auth and bearer
	f([]promv1.PodMetricsEndpoint{{
		BearerTokenSecret: corev1.SecretKeySelector{Key: "bearer"},
		BasicAuth: &promv1.BasicAuth{
			Username: corev1.SecretKeySelector{Key: "username"},
			Password: corev1.SecretKeySelector{Key: "password"},
		},
	}}, []vmv1beta1.PodMetricsEndpoint{{
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
	}})
}

func TestConvertProbe(t *testing.T) {
	f := func(cfg *promv1.Probe, want vmv1beta1.VMProbe) {
		got := ConvertProbe(cfg, &config.BaseOperatorConf{
			FilterPrometheusConverterLabelPrefixes:      []string{"helm.sh"},
			FilterPrometheusConverterAnnotationPrefixes: []string{"app.kubernetes"},
		})

		if !reflect.DeepEqual(*got, want) {
			t.Errorf("ConvertProbe() got = \n%v, \nwant \n%v", got, want)
		}
	}

	// with static config
	f(&promv1.Probe{
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
	}, vmv1beta1.VMProbe{
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
	})

	// with ingress config
	f(&promv1.Probe{
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
	}, vmv1beta1.VMProbe{
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
	})
}
