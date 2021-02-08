package converter

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/v1beta1"
	v1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

func TestConvertTlsConfig(t *testing.T) {
	type args struct {
		tlsConf *v1.TLSConfig
	}
	tests := []struct {
		name string
		args args
		want *v1beta1vm.TLSConfig
	}{
		{
			name: "replace prom secret path",
			args: args{
				tlsConf: &v1.TLSConfig{
					CAFile:   "/etc/prom_add/ca",
					CertFile: "/etc/prometheus/secrets/cert.crt",
					KeyFile:  "/etc/prometheus/configmaps/key.pem",
				},
			},
			want: &v1beta1vm.TLSConfig{
				CAFile:   "/etc/prom_add/ca",
				CertFile: "/etc/vm/secrets/cert.crt",
				KeyFile:  "/etc/vm/configs/key.pem",
			},
		},
		{
			name: "with server name and insecure",
			args: args{
				tlsConf: &v1.TLSConfig{
					CAFile:        "/etc/prom_add/ca",
					CertFile:      "/etc/prometheus/secrets/cert.crt",
					KeyFile:       "/etc/prometheus/configmaps/key.pem",
					SafeTLSConfig: v1.SafeTLSConfig{ServerName: "some-hostname", InsecureSkipVerify: true},
				},
			},
			want: &v1beta1vm.TLSConfig{
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
			got := ConvertTlsConfig(tt.args.tlsConf)
			if got.KeyFile != tt.want.KeyFile || got.CertFile != tt.want.CertFile || got.CAFile != tt.want.CAFile {
				t.Errorf("ConvertTlsConfig() = \n%v, \nwant \n%v", got, tt.want)
			}
		})
	}
}

func TestConvertRelabelConfig(t *testing.T) {
	type args struct {
		promRelabelConfig []*v1.RelabelConfig
	}
	tests := []struct {
		name string
		args args
		want []*v1beta1vm.RelabelConfig
	}{
		{
			name: "test empty cfg",
			args: args{},
			want: nil,
		},
		{
			name: "1 relabel cfg rule",
			args: args{
				promRelabelConfig: []*v1.RelabelConfig{
					{
						Action:       "drop",
						SourceLabels: []string{"__address__"},
					},
				},
			},
			want: []*v1beta1vm.RelabelConfig{
				{
					Action:       "drop",
					SourceLabels: []string{"__address__"},
				},
			},
		},
		{
			name: "unsupported config",
			args: args{
				promRelabelConfig: []*v1.RelabelConfig{
					{
						Action: "drop",
					},
					{
						Action:       "keep",
						SourceLabels: []string{"__address__"},
					},
				},
			},
			want: []*v1beta1vm.RelabelConfig{
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
		promEndpoint []v1.Endpoint
	}
	tests := []struct {
		name string
		args args
		want []v1beta1vm.Endpoint
	}{
		{
			name: "convert endpoint with relabel config",
			args: args{
				promEndpoint: []v1.Endpoint{
					{
						Port: "9100",
						Path: "/metrics",
						RelabelConfigs: []*v1.RelabelConfig{
							{
								Action:       "drop",
								SourceLabels: []string{"__meta__instance"},
							},
							{
								Action: "keep",
							},
						},
					},
				},
			},
			want: []v1beta1vm.Endpoint{
				{
					Path: "/metrics",
					Port: "9100",
					RelabelConfigs: []*v1beta1vm.RelabelConfig{
						{
							Action:       "drop",
							SourceLabels: []string{"__meta__instance"},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertEndpoint(tt.args.promEndpoint); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertEndpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertServiceMonitor(t *testing.T) {
	type args struct {
		serviceMon *v1.ServiceMonitor
	}
	tests := []struct {
		name string
		args args
		want v1beta1vm.VMServiceScrape
	}{
		{
			name: "with metricsRelabelConfig",
			args: args{
				serviceMon: &v1.ServiceMonitor{
					Spec: v1.ServiceMonitorSpec{
						Endpoints: []v1.Endpoint{
							{
								MetricRelabelConfigs: []*v1.RelabelConfig{
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
			want: v1beta1vm.VMServiceScrape{
				Spec: v1beta1vm.VMServiceScrapeSpec{
					Endpoints: []v1beta1vm.Endpoint{
						{
							MetricRelabelConfigs: []*v1beta1vm.RelabelConfig{
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertServiceMonitor(tt.args.serviceMon, false)
			if !reflect.DeepEqual(*got, tt.want) {
				t.Errorf("ConvertServiceMonitor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertPodEndpoints(t *testing.T) {
	type args struct {
		promPodEnpoints []v1.PodMetricsEndpoint
	}
	tests := []struct {
		name string
		args args
		want []v1beta1vm.PodMetricsEndpoint
	}{
		{
			name: "with tls config",
			args: args{promPodEnpoints: []v1.PodMetricsEndpoint{
				{
					TLSConfig: &v1.PodMetricsEndpointTLSConfig{
						SafeTLSConfig: v1.SafeTLSConfig{
							InsecureSkipVerify: true,
							ServerName:         "some-srv",
							CA: v1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{
								Key: "ca",
							}},
						},
					},
				},
			}},
			want: []v1beta1vm.PodMetricsEndpoint{{
				TLSConfig: &v1beta1vm.TLSConfig{
					InsecureSkipVerify: true,
					ServerName:         "some-srv",
					CA: v1beta1vm.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{
						Key: "ca"},
					}},
			}}},
		{
			name: "with basic auth and bearer",
			args: args{promPodEnpoints: []v1.PodMetricsEndpoint{
				{
					BearerTokenSecret: corev1.SecretKeySelector{Key: "bearer"},
					BasicAuth: &v1.BasicAuth{
						Username: corev1.SecretKeySelector{Key: "username"},
						Password: corev1.SecretKeySelector{Key: "password"},
					},
				},
			}},
			want: []v1beta1vm.PodMetricsEndpoint{{
				BearerTokenSecret: corev1.SecretKeySelector{Key: "bearer"},
				BasicAuth: &v1beta1vm.BasicAuth{Username: corev1.SecretKeySelector{
					Key: "username",
				},
					Password: corev1.SecretKeySelector{Key: "password"},
				},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertPodEndpoints(tt.args.promPodEnpoints); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertPodEndpoints() = %v, want %v", got, tt.want)
			}
		})
	}
}
