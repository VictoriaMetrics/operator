package factory

import (
	"reflect"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_generateProbeConfig(t *testing.T) {
	type args struct {
		cr                       *victoriametricsv1beta1.VMProbe
		i                        int
		apiserverConfig          *victoriametricsv1beta1.APIServerConfig
		basicAuthSecrets         map[string]BasicAuthCredentials
		ignoreNamespaceSelectors bool
		enforcedNamespaceLabel   string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "generate static config",
			args: args{
				cr: &victoriametricsv1beta1.VMProbe{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "static-probe",
					},
					Spec: victoriametricsv1beta1.VMProbeSpec{
						Module:       "http",
						VMProberSpec: victoriametricsv1beta1.VMProberSpec{URL: "blackbox-monitor:9115"},
						Targets: victoriametricsv1beta1.VMProbeTargets{
							StaticConfig: &victoriametricsv1beta1.VMProbeTargetStaticConfig{
								Targets: []string{"host-1", "host-2"},
								Labels:  map[string]string{"label1": "value1"},
							},
						}},
				},
				i: 0,
			},
			want: `job_name: default/static-probe/0
params:
  module:
  - http
metrics_path: /probe
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
		},
		{
			name: "with ingress discover",
			args: args{
				cr: &victoriametricsv1beta1.VMProbe{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "probe-ingress",
						Namespace: "monitor",
					},
					Spec: victoriametricsv1beta1.VMProbeSpec{
						Module:       "http200",
						VMProberSpec: victoriametricsv1beta1.VMProberSpec{URL: "blackbox:9115"},
						Targets: victoriametricsv1beta1.VMProbeTargets{
							Ingress: &victoriametricsv1beta1.ProbeTargetIngress{
								NamespaceSelector: victoriametricsv1beta1.NamespaceSelector{},
								RelabelConfigs: []*victoriametricsv1beta1.RelabelConfig{
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
			},
			want: `job_name: monitor/probe-ingress/0
params:
  module:
  - http200
metrics_path: /probe
kubernetes_sd_configs:
- role: ingress
  namespaces:
    names:
    - monitor
relabel_configs:
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
  - __address__
  target_label: __param_target
- source_labels:
  - __param_target
  target_label: instance
- target_label: __address__
  replacement: blackbox:9115
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateProbeConfig(tt.args.cr, tt.args.i, tt.args.apiserverConfig, tt.args.basicAuthSecrets, tt.args.ignoreNamespaceSelectors, tt.args.enforcedNamespaceLabel)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot decode probe config, it must be in yaml format :%e", err)
				return
			}
			if !reflect.DeepEqual(string(gotBytes), tt.want) {
				t.Errorf("generateProbeConfig() result mismatch \ngot: \n%v \nwant \n%v", string(gotBytes), tt.want)
			}
		})
	}
}
