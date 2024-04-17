package converter

import (
	"fmt"
	"reflect"
	"testing"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/google/go-cmp/cmp"
	v1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
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
			got := convertTLSConfig(tt.args.tlsConf)
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
						SourceLabels: []v1.LabelName{"__address__"},
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
						SourceLabels: []v1.LabelName{"__address__"},
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
			got := convertRelabelConfig(tt.args.promRelabelConfig)
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
								SourceLabels: []v1.LabelName{"__meta__instance"},
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
			if got := convertEndpoint(tt.args.promEndpoint); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertEndpoint() \ngot:  \n%v\n, \nwant: \n%v", got, tt.want)
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
										SourceLabels: []v1.LabelName{"__meta__instance"},
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
		{
			name: "with label and annotations filter",
			args: args{
				serviceMon: &v1.ServiceMonitor{
					ObjectMeta: v12.ObjectMeta{
						Labels:      map[string]string{"helm.sh/release": "prod", "keep-label": "value"},
						Annotations: map[string]string{"app.kubernetes.io/": "release"},
					},
					Spec: v1.ServiceMonitorSpec{
						Endpoints: []v1.Endpoint{
							{
								MetricRelabelConfigs: []*v1.RelabelConfig{
									{
										Action:       "drop",
										SourceLabels: []v1.LabelName{"__meta__instance"},
									},
								},
							},
						},
					},
				},
			},
			want: v1beta1vm.VMServiceScrape{
				ObjectMeta: v12.ObjectMeta{
					Labels: map[string]string{"keep-label": "value"},
				},
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
			got := ConvertServiceMonitor(tt.args.serviceMon, &config.BaseOperatorConf{
				FilterPrometheusConverterLabelPrefixes:      []string{"helm.sh"},
				FilterPrometheusConverterAnnotationPrefixes: []string{"app.kubernetes"},
			})
			if !reflect.DeepEqual(*got, tt.want) {
				t.Errorf("ConvertServiceMonitor() got = \n%v, \nwant \n%v", got, tt.want)
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
					CA: v1beta1vm.SecretOrConfigMap{
						ConfigMap: &corev1.ConfigMapKeySelector{
							Key: "ca",
						},
					},
				},
			}},
		},
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
				BearerTokenSecret: &corev1.SecretKeySelector{Key: "bearer"},
				BasicAuth: &v1beta1vm.BasicAuth{
					Username: corev1.SecretKeySelector{
						Key: "username",
					},
					Password: corev1.SecretKeySelector{Key: "password"},
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

func TestConvertAlertmanagerConfig(t *testing.T) {
	f := func(name string, promCfg *v1alpha1.AlertmanagerConfig, validate func(convertedAMCfg *v1beta1vm.VMAlertmanagerConfig) error) {
		t.Run(name, func(t *testing.T) {
			converted, err := ConvertAlertmanagerConfig(promCfg, &config.BaseOperatorConf{})
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if err := validate(converted); err != nil {
				t.Fatalf("not valid converted alertmanager config")
			}
		})
	}
	f("simple convert",
		&v1alpha1.AlertmanagerConfig{
			ObjectMeta: v12.ObjectMeta{Name: "test-1"},
			Spec: v1alpha1.AlertmanagerConfigSpec{
				Route: &v1alpha1.Route{Receiver: "webhook", GroupInterval: "1min"},
				Receivers: []v1alpha1.Receiver{
					{
						Name:           "webhook",
						WebhookConfigs: []v1alpha1.WebhookConfig{{URLSecret: &corev1.SecretKeySelector{Key: "secret"}}},
					},
				},
			},
		},
		func(convertedAMCfg *v1beta1vm.VMAlertmanagerConfig) error {
			if convertedAMCfg.Name != "test-1" {
				return fmt.Errorf("name not match, want: %s got: %s", "test-1", convertedAMCfg.Name)
			}
			if convertedAMCfg.Spec.Route.Receiver != "webhook" {
				return fmt.Errorf("unexpected receiver at route name: %s", convertedAMCfg.Spec.Route.Receiver)
			}
			if convertedAMCfg.Spec.Receivers[0].Name != "webhook" {
				return fmt.Errorf("unexpected receiver name: %s", convertedAMCfg.Spec.Receivers[0].Name)
			}
			if convertedAMCfg.Spec.Receivers[0].WebhookConfigs[0].URLSecret.Key != "secret" {
				return fmt.Errorf("expected url with secret key")
			}
			return nil
		})
}

func TestConvertProbe(t *testing.T) {
	type args struct {
		probe *v1.Probe
	}
	tests := []struct {
		name string
		args args
		want v1beta1vm.VMProbe
	}{
		{
			name: "with static config",
			args: args{
				probe: &v1.Probe{
					Spec: v1.ProbeSpec{
						ProberSpec: v1.ProberSpec{
							ProxyURL: "http://proxy.com",
						},
						Targets: v1.ProbeTargets{
							StaticConfig: &v1.ProbeTargetStaticConfig{
								Targets: []string{"target-1", "target-2"},
								Labels: map[string]string{
									"l1": "v1",
									"l2": "v2",
								},
								RelabelConfigs: []*v1.RelabelConfig{
									{
										Action:       "drop",
										SourceLabels: []v1.LabelName{"__address__"},
									},
								},
							},
						},
					},
				},
			},
			want: v1beta1vm.VMProbe{
				Spec: v1beta1vm.VMProbeSpec{
					ProxyURL: pointer.String("http://proxy.com"),
					Targets: v1beta1vm.VMProbeTargets{
						StaticConfig: &v1beta1vm.VMProbeTargetStaticConfig{
							Targets: []string{"target-1", "target-2"},
							Labels: map[string]string{
								"l1": "v1",
								"l2": "v2",
							},
							RelabelConfigs: []*v1beta1vm.RelabelConfig{
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
				probe: &v1.Probe{
					Spec: v1.ProbeSpec{
						Targets: v1.ProbeTargets{
							Ingress: &v1.ProbeTargetIngress{
								Selector: v12.LabelSelector{
									MatchLabels: map[string]string{
										"app": "test",
									},
									MatchExpressions: []v12.LabelSelectorRequirement{
										{
											Key:      "key",
											Operator: "op",
											Values:   []string{"v1", "v2"},
										},
									},
								},
								NamespaceSelector: v1.NamespaceSelector{
									MatchNames: []string{"test-ns"},
								},
								RelabelConfigs: []*v1.RelabelConfig{
									{
										Action:       "keep",
										SourceLabels: []v1.LabelName{"__address__"},
									},
								},
							},
						},
					},
				},
			},
			want: v1beta1vm.VMProbe{
				Spec: v1beta1vm.VMProbeSpec{
					Targets: v1beta1vm.VMProbeTargets{
						Ingress: &v1beta1vm.ProbeTargetIngress{
							Selector: v12.LabelSelector{
								MatchLabels: map[string]string{
									"app": "test",
								},
								MatchExpressions: []v12.LabelSelectorRequirement{
									{
										Key:      "key",
										Operator: "op",
										Values:   []string{"v1", "v2"},
									},
								},
							},
							NamespaceSelector: v1beta1vm.NamespaceSelector{
								MatchNames: []string{"test-ns"},
							},
							RelabelConfigs: []*v1beta1vm.RelabelConfig{
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

func TestConvertScrapeConfig(t *testing.T) {
	type args struct {
		scrapeConfig *v1alpha1.ScrapeConfig
	}
	tests := []struct {
		name string
		args args
		want v1beta1vm.VMScrapeConfig
	}{
		{
			name: "with static config",
			args: args{
				scrapeConfig: &v1alpha1.ScrapeConfig{
					Spec: v1alpha1.ScrapeConfigSpec{
						StaticConfigs: []v1alpha1.StaticConfig{
							{
								Targets: []v1alpha1.Target{"target-1", "target-2"},
							},
						},
						HonorTimestamps:   pointer.Bool(true),
						EnableCompression: pointer.Bool(true),
						BasicAuth: &v1.BasicAuth{
							Username: corev1.SecretKeySelector{Key: "username"},
							Password: corev1.SecretKeySelector{Key: "password"},
						},
						ProxyConfig: &v1alpha1.ProxyConfig{
							ProxyURL: pointer.String("http://proxy.com"),
						},
						RelabelConfigs: []*v1.RelabelConfig{
							{
								Action:      "LabelMap",
								Regex:       "__meta_kubernetes_pod_label_(.+)",
								Replacement: "foo_$1",
							},
						},
						MetricRelabelConfigs: []*v1.RelabelConfig{
							{
								SourceLabels: []v1.LabelName{"__meta_kubernetes_pod_name", "__meta_kubernetes_pod_container_port_number"},
								Separator:    ":",
								TargetLabel:  "host_port",
							},
						},
					},
				},
			},
			want: v1beta1vm.VMScrapeConfig{
				Spec: v1beta1vm.VMScrapeConfigSpec{
					ProxyURL: pointer.String("http://proxy.com"),
					StaticConfigs: []v1beta1vm.StaticConfig{
						{
							Targets: []string{"target-1", "target-2"},
						},
					},
					HonorTimestamps: pointer.Bool(true),
					VMScrapeParams:  &v1beta1vm.VMScrapeParams{DisableCompression: pointer.Bool(false)},
					BasicAuth: &v1beta1vm.BasicAuth{
						Username: corev1.SecretKeySelector{Key: "username"},
						Password: corev1.SecretKeySelector{Key: "password"},
					},
					RelabelConfigs: []*v1beta1vm.RelabelConfig{
						{
							Action:      "LabelMap",
							Regex:       v1beta1vm.StringOrArray{"__meta_kubernetes_pod_label_(.+)"},
							Replacement: "foo_$1",
						},
					},
					MetricRelabelConfigs: []*v1beta1vm.RelabelConfig{
						{
							SourceLabels: []string{"__meta_kubernetes_pod_name", "__meta_kubernetes_pod_container_port_number"},
							Separator:    ":",
							TargetLabel:  "host_port",
						},
					},
				},
			},
		},
		{
			name: "with httpsd config",
			args: args{
				scrapeConfig: &v1alpha1.ScrapeConfig{
					Spec: v1alpha1.ScrapeConfigSpec{
						HTTPSDConfigs: []v1alpha1.HTTPSDConfig{
							{
								URL: "http://test1.com",
								Authorization: &v1.SafeAuthorization{
									Type: "Bearer",
									Credentials: &corev1.SecretKeySelector{
										Key: "token",
									},
								},
							},
							{
								URL: "http://test2.com",
								TLSConfig: &v1.SafeTLSConfig{
									CA:                 v1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{Key: "ca.crt"}},
									Cert:               v1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{Key: "cert.pem"}},
									KeySecret:          &corev1.SecretKeySelector{Key: "key"},
									ServerName:         "test",
									InsecureSkipVerify: true,
								},
							},
						},
					},
				},
			},
			want: v1beta1vm.VMScrapeConfig{
				Spec: v1beta1vm.VMScrapeConfigSpec{
					HTTPSDConfigs: []v1beta1vm.HTTPSDConfig{
						{
							URL: "http://test1.com",
							Authorization: &v1beta1vm.Authorization{
								Type: "Bearer",
								Credentials: &corev1.SecretKeySelector{
									Key: "token",
								},
							},
						},
						{
							URL: "http://test2.com",
							TLSConfig: &v1beta1vm.TLSConfig{
								CA:                 v1beta1vm.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{Key: "ca.crt"}},
								Cert:               v1beta1vm.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{Key: "cert.pem"}},
								KeySecret:          &corev1.SecretKeySelector{Key: "key"},
								ServerName:         "test",
								InsecureSkipVerify: true,
							},
						},
					},
				},
			},
		},
		{
			name: "with k8s sd config",
			args: args{
				scrapeConfig: &v1alpha1.ScrapeConfig{
					Spec: v1alpha1.ScrapeConfigSpec{
						KubernetesSDConfigs: []v1alpha1.KubernetesSDConfig{
							{
								APIServer: pointer.String("http://1.2.3.4"),
								Role:      v1alpha1.Role("pod"),
								Selectors: []v1alpha1.K8SSelectorConfig{
									{
										Label: "app=test",
									},
								},
							},
						},
					},
				},
			},
			want: v1beta1vm.VMScrapeConfig{
				Spec: v1beta1vm.VMScrapeConfigSpec{
					KubernetesSDConfigs: []v1beta1vm.KubernetesSDConfig{
						{
							APIServer: pointer.String("http://1.2.3.4"),
							Role:      "pod",
							Selectors: []v1beta1vm.K8SSelectorConfig{
								{
									Label: "app=test",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with consul sd config",
			args: args{
				scrapeConfig: &v1alpha1.ScrapeConfig{
					Spec: v1alpha1.ScrapeConfigSpec{
						ConsulSDConfigs: []v1alpha1.ConsulSDConfig{
							{
								Server:     "http://1.2.3.4",
								TokenRef:   &corev1.SecretKeySelector{Key: "token"},
								Datacenter: pointer.String("prod"),
								Namespace:  pointer.String("test"),
							},
						},
					},
				},
			},
			want: v1beta1vm.VMScrapeConfig{
				Spec: v1beta1vm.VMScrapeConfigSpec{
					ConsulSDConfigs: []v1beta1vm.ConsulSDConfig{
						{
							Server:     "http://1.2.3.4",
							TokenRef:   &corev1.SecretKeySelector{Key: "token"},
							Datacenter: pointer.String("prod"),
							Namespace:  pointer.String("test"),
						},
					},
				},
			},
		},
		{
			name: "with ec2 sd config",
			args: args{
				scrapeConfig: &v1alpha1.ScrapeConfig{
					Spec: v1alpha1.ScrapeConfigSpec{
						EC2SDConfigs: []v1alpha1.EC2SDConfig{
							{
								Region:    pointer.String("us-west-1"),
								AccessKey: &corev1.SecretKeySelector{Key: "accesskey"},
								SecretKey: &corev1.SecretKeySelector{Key: "secret"},
								Filters: []*v1alpha1.EC2Filter{
									{
										Name:   "f1",
										Values: []string{"1"},
									},
								},
								Port: pointer.Int(80),
							},
						},
					},
				},
			},
			want: v1beta1vm.VMScrapeConfig{
				Spec: v1beta1vm.VMScrapeConfigSpec{
					EC2SDConfigs: []v1beta1vm.EC2SDConfig{
						{
							Region:    pointer.String("us-west-1"),
							AccessKey: &corev1.SecretKeySelector{Key: "accesskey"},
							SecretKey: &corev1.SecretKeySelector{Key: "secret"},
							Filters: []*v1beta1vm.EC2Filter{
								{
									Name:   "f1",
									Values: []string{"1"},
								},
							},
							Port: pointer.Int(80),
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertScrapeConfig(tt.args.scrapeConfig, &config.BaseOperatorConf{})
			if !cmp.Equal(*got, tt.want) {
				diff := cmp.Diff(*got, tt.want)
				t.Fatal("not expected output with diff: ", diff)
			}
		})
	}
}
