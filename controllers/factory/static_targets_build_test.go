package factory

import (
	"testing"

	"k8s.io/utils/pointer"

	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gopkg.in/yaml.v2"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

func Test_generateStaticScrapeConfig(t *testing.T) {
	type args struct {
		m                       *victoriametricsv1beta1.VMStaticScrape
		ep                      *victoriametricsv1beta1.TargetEndpoint
		i                       int
		basicAuthSecrets        map[string]BasicAuthCredentials
		bearerTokens            map[string]BearerToken
		overrideHonorLabels     bool
		overrideHonorTimestamps bool
		enforceNamespaceLabel   string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "basic cfg",
			args: args{
				m: &victoriametricsv1beta1.VMStaticScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "static-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMStaticScrapeSpec{
						JobName: "static-job",
					},
				},
				ep: &victoriametricsv1beta1.TargetEndpoint{
					Targets: []string{"192.168.11.1:9100", "some-host:9100"},
					Labels:  map[string]string{"env": "dev", "group": "prod"},
				},
			},
			want: `job_name: default/static-1/0
honor_labels: false
static_configs:
- targets:
  - 192.168.11.1:9100
  - some-host:9100
  labels:
    env: dev
    group: prod
relabel_configs:
- target_label: job
  replacement: static-job
`,
		},
		{
			name: "basic cfg with overrides",
			args: args{
				m: &victoriametricsv1beta1.VMStaticScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "static-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMStaticScrapeSpec{
						JobName: "static-job",
					},
				},
				ep: &victoriametricsv1beta1.TargetEndpoint{
					Targets:         []string{"192.168.11.1:9100", "some-host:9100"},
					Labels:          map[string]string{"env": "dev", "group": "prod"},
					HonorTimestamps: pointer.BoolPtr(true),
					MetricRelabelConfigs: []*victoriametricsv1beta1.RelabelConfig{
						{
							TargetLabel:  "namespace",
							SourceLabels: []string{"abuse"},
							Action:       "replace",
						},
					},
				},
				overrideHonorTimestamps: false,
				overrideHonorLabels:     true,
				enforceNamespaceLabel:   "namespace",
			},
			want: `job_name: default/static-1/0
honor_labels: false
honor_timestamps: true
static_configs:
- targets:
  - 192.168.11.1:9100
  - some-host:9100
  labels:
    env: dev
    group: prod
relabel_configs:
- target_label: job
  replacement: static-job
- target_label: namespace
  replacement: default
metric_relabel_configs: []
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateStaticScrapeConfig(tt.args.m, tt.args.ep, tt.args.i, tt.args.basicAuthSecrets, tt.args.bearerTokens, tt.args.overrideHonorLabels, tt.args.overrideHonorTimestamps, tt.args.enforceNamespaceLabel)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !assert.Equal(t, string(gotBytes), tt.want) {
				t.Errorf("generateStaticScrapeConfig() = \n%v, want \n%v", string(gotBytes), tt.want)
			}
		})
	}
}
