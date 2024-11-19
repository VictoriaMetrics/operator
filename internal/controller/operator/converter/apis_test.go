package converter

import (
	"reflect"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestConvertTlsConfig(t *testing.T) {
	type args struct {
		tlsConf *promv1.TLSConfig
	}
	tests := []struct {
		name string
		args args
		want *vmv1beta1.TLSConfig
	}{
		{
			name: "replace prom secret path",
			args: args{
				tlsConf: &promv1.TLSConfig{
					CAFile:   "/etc/prom_add/ca",
					CertFile: "/etc/prometheus/secrets/cert.crt",
					KeyFile:  "/etc/prometheus/configmaps/key.pem",
				},
			},
			want: &vmv1beta1.TLSConfig{
				CAFile:   "/etc/prom_add/ca",
				CertFile: "/etc/vm/secrets/cert.crt",
				KeyFile:  "/etc/vm/configs/key.pem",
			},
		},
		{
			name: "with server name and insecure",
			args: args{
				tlsConf: &promv1.TLSConfig{
					CAFile:        "/etc/prom_add/ca",
					CertFile:      "/etc/prometheus/secrets/cert.crt",
					KeyFile:       "/etc/prometheus/configmaps/key.pem",
					SafeTLSConfig: promv1.SafeTLSConfig{ServerName: ptr.To("some-hostname"), InsecureSkipVerify: ptr.To(true)},
				},
			},
			want: &vmv1beta1.TLSConfig{
				CAFile:             "/etc/prom_add/ca",
				CertFile:           "/etc/vm/secrets/cert.crt",
				KeyFile:            "/etc/vm/configs/key.pem",
				ServerName:         "some-hostname",
				InsecureSkipVerify: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertTLSConfig(tt.args.tlsConf)
			if got.KeyFile != tt.want.KeyFile || got.CertFile != tt.want.CertFile || got.CAFile != tt.want.CAFile {
				t.Errorf("ConvertTlsConfig() = \n%v, \nwant \n%v", got, tt.want)
			}
		})
	}
}

func TestConvertRelabelConfig(t *testing.T) {
	type args struct {
		promRelabelConfig []promv1.RelabelConfig
	}
	tests := []struct {
		name string
		args args
		want []*vmv1beta1.RelabelConfig
	}{
		{
			name: "test empty cfg",
			args: args{},
			want: nil,
		},
		{
			name: "1 relabel cfg rule",
			args: args{
				promRelabelConfig: []promv1.RelabelConfig{
					{
						Action:       "drop",
						SourceLabels: []promv1.LabelName{"__address__"},
					},
				},
			},
			want: []*vmv1beta1.RelabelConfig{
				{
					Action:       "drop",
					SourceLabels: []string{"__address__"},
				},
			},
		},
		{
			name: "unsupported config",
			args: args{
				promRelabelConfig: []promv1.RelabelConfig{
					{
						Action: "drop",
					},
					{
						Action:       "keep",
						SourceLabels: []promv1.LabelName{"__address__"},
					},
				},
			},
			want: []*vmv1beta1.RelabelConfig{
				{
					Action:       "keep",
					SourceLabels: []string{"__address__"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertRelabelConfig(tt.args.promRelabelConfig)
			if len(got) != len(tt.want) {
				t.Fatalf("len of relabelConfigs mismatch, want: %d, got %d", len(tt.want), len(got))
			}
			for i, wantRelabelConfig := range tt.want {
				if !reflect.DeepEqual(*wantRelabelConfig, *got[i]) {
					t.Fatalf("ConvertRelabelConfig() = %v, want %v", *got[i], *wantRelabelConfig)
				}
			}
		})
	}
}

func TestConvertEndpoint(t *testing.T) {
	type args struct {
		promEndpoint []promv1.Endpoint
	}
	tests := []struct {
		name string
		args args
		want []vmv1beta1.Endpoint
	}{
		{
			name: "convert endpoint with relabel config",
			args: args{
				promEndpoint: []promv1.Endpoint{
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertEndpoint(tt.args.promEndpoint); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertEndpoint() \ngot:  \n%v\n, \nwant: \n%v", got, tt.want)
			}
		})
	}
}

func TestConvertServiceMonitor(t *testing.T) {
	type args struct {
		serviceMon *promv1.ServiceMonitor
	}
	tests := []struct {
		name                                     string
		VMServiceScrapeDefaultRoleEndpointslices bool
		args                                     args
		want                                     vmv1beta1.VMServiceScrape
	}{
		{
			name:                                     "with metricsRelabelConfig",
			VMServiceScrapeDefaultRoleEndpointslices: false,
			args: args{
				serviceMon: &promv1.ServiceMonitor{
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
		},
		{
			name:                                     "with label and annotations filter",
			VMServiceScrapeDefaultRoleEndpointslices: false,
			args: args{
				serviceMon: &promv1.ServiceMonitor{
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
		},
		{
			name:                                     "with endpointslice discovery role",
			VMServiceScrapeDefaultRoleEndpointslices: true,
			args: args{
				serviceMon: &promv1.ServiceMonitor{
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
			},
			want: vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"keep-label": "value"},
				},
				Spec: vmv1beta1.VMServiceScrapeSpec{
					DiscoveryRole: "endpointslices",
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertServiceMonitor(tt.args.serviceMon, &config.BaseOperatorConf{
				FilterPrometheusConverterLabelPrefixes:      []string{"helm.sh"},
				FilterPrometheusConverterAnnotationPrefixes: []string{"app.kubernetes"},
				VMServiceScrapeDefaultRoleEndpointslices:    tt.VMServiceScrapeDefaultRoleEndpointslices,
			})
			if !reflect.DeepEqual(*got, tt.want) {
				t.Errorf("ConvertServiceMonitor() got = \n%v, \nwant \n%v", got, tt.want)
			}
		})
	}
}

func TestConvertPodEndpoints(t *testing.T) {
	type args struct {
		promPodEnpoints []promv1.PodMetricsEndpoint
	}
	tests := []struct {
		name string
		args args
		want []vmv1beta1.PodMetricsEndpoint
	}{
		{
			name: "with partial tls config",
			args: args{promPodEnpoints: []promv1.PodMetricsEndpoint{
				{
					BearerTokenSecret: corev1.SecretKeySelector{},
					TLSConfig: &promv1.SafeTLSConfig{
						CA: promv1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{
							Key: "ca",
						}},
					},
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
		},

		{
			name: "with tls config",
			args: args{promPodEnpoints: []promv1.PodMetricsEndpoint{
				{
					BearerTokenSecret: corev1.SecretKeySelector{},
					TLSConfig: &promv1.SafeTLSConfig{
						InsecureSkipVerify: ptr.To(true),
						ServerName:         ptr.To("some-srv"),
						CA: promv1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{
							Key: "ca",
						}},
					},
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
		},
		{
			name: "with basic auth and bearer",
			args: args{promPodEnpoints: []promv1.PodMetricsEndpoint{
				{
					BearerTokenSecret: corev1.SecretKeySelector{Key: "bearer"},
					BasicAuth: &promv1.BasicAuth{
						Username: corev1.SecretKeySelector{Key: "username"},
						Password: corev1.SecretKeySelector{Key: "password"},
					},
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertPodEndpoints(tt.args.promPodEnpoints); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertPodEndpoints() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertProbe(t *testing.T) {
	type args struct {
		probe *promv1.Probe
	}
	tests := []struct {
		name string
		args args
		want vmv1beta1.VMProbe
	}{
		{
			name: "with static config",
			args: args{
				probe: &promv1.Probe{
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
		},
		{
			name: "with ingress config",
			args: args{
				probe: &promv1.Probe{
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertProbe(tt.args.probe, &config.BaseOperatorConf{
				FilterPrometheusConverterLabelPrefixes:      []string{"helm.sh"},
				FilterPrometheusConverterAnnotationPrefixes: []string{"app.kubernetes"},
			})

			if !reflect.DeepEqual(*got, tt.want) {
				t.Errorf("ConvertProbe() got = \n%v, \nwant \n%v", got, tt.want)
			}
		})
	}
}
