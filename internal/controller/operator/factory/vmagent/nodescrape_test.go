package vmagent

import (
	"context"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func Test_generateNodeScrapeConfig(t *testing.T) {
	type args struct {
		cr                      vmv1beta1.VMAgent
		m                       *vmv1beta1.VMNodeScrape
		i                       int
		apiserverConfig         *vmv1beta1.APIServerConfig
		ssCache                 *scrapesSecretsCache
		ignoreHonorLabels       bool
		overrideHonorTimestamps bool
		enforcedNamespaceLabel  string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "ok build node",
			args: args{
				apiserverConfig: nil,
				ssCache:         &scrapesSecretsCache{},
				i:               1,
				m: &vmv1beta1.VMNodeScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nodes-basic",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMNodeScrapeSpec{
						Port:     "9100",
						Path:     "/metrics",
						Interval: "30s",
					},
				},
			},
			want: `job_name: nodeScrape/default/nodes-basic/1
honor_labels: false
kubernetes_sd_configs:
- role: node
scrape_interval: 30s
metrics_path: /metrics
relabel_configs:
- source_labels:
  - __meta_kubernetes_node_name
  target_label: node
- target_label: job
  replacement: default/nodes-basic
- source_labels:
  - __address__
  target_label: __address__
  regex: ^(.*):(.*)
  replacement: ${1}:9100
`,
		},
		{
			name: "complete ok build node",
			args: args{
				apiserverConfig: nil,
				ssCache: &scrapesSecretsCache{
					oauth2Secrets: map[string]*k8stools.OAuthCreds{},
					bearerTokens:  map[string]string{},
					baSecrets: map[string]*k8stools.BasicAuthCredentials{
						"nodeScrape/default/nodes-basic": {
							Username: "username",
						},
					},
				},
				i: 1,
				m: &vmv1beta1.VMNodeScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nodes-basic",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMNodeScrapeSpec{
						Port:            "9100",
						Path:            "/metrics",
						Interval:        "30s",
						Scheme:          "https",
						HonorLabels:     true,
						ProxyURL:        pointer.String("https://some-url"),
						SampleLimit:     50,
						SeriesLimit:     1000,
						FollowRedirects: pointer.Bool(true),
						ScrapeTimeout:   "10s",
						ScrapeInterval:  "5s",
						Params:          map[string][]string{"module": {"client"}},
						JobLabel:        "env",
						HonorTimestamps: pointer.Bool(true),
						TargetLabels:    []string{"app", "env"},
						BearerTokenFile: "/tmp/bearer",
						BasicAuth: &vmv1beta1.BasicAuth{
							Username: corev1.SecretKeySelector{Key: "username", LocalObjectReference: corev1.LocalObjectReference{Name: "ba-secret"}},
						},
						TLSConfig: &vmv1beta1.TLSConfig{
							InsecureSkipVerify: true,
						},
						OAuth2: &vmv1beta1.OAuth2{},
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"job": "prod"},
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{Key: "external", Operator: metav1.LabelSelectorOpIn, Values: []string{"world"}},
							},
						},
						VMScrapeParams: &vmv1beta1.VMScrapeParams{
							StreamParse: pointer.Bool(true),
							ProxyClientConfig: &vmv1beta1.ProxyAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
								BearerTokenFile: "/tmp/proxy-token",
							},
						},
						RelabelConfigs:       []*vmv1beta1.RelabelConfig{},
						MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{},
					},
				},
			},
			want: `job_name: nodeScrape/default/nodes-basic/1
honor_labels: true
honor_timestamps: true
kubernetes_sd_configs:
- role: node
scrape_interval: 5s
scrape_timeout: 10s
metrics_path: /metrics
proxy_url: https://some-url
sample_limit: 50
series_limit: 1000
params:
  module:
  - client
follow_redirects: true
scheme: https
tls_config:
  insecure_skip_verify: true
bearer_token_file: /tmp/bearer
basic_auth:
  username: username
  password: ""
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_node_label_job
  regex: prod
- action: keep
  source_labels:
  - __meta_kubernetes_node_label_external
  regex: world
- source_labels:
  - __meta_kubernetes_node_name
  target_label: node
- source_labels:
  - __meta_kubernetes_node_label_app
  target_label: app
  regex: (.+)
  replacement: ${1}
- source_labels:
  - __meta_kubernetes_node_label_env
  target_label: env
  regex: (.+)
  replacement: ${1}
- target_label: job
  replacement: default/nodes-basic
- source_labels:
  - __meta_kubernetes_node_label_env
  target_label: job
  regex: (.+)
  replacement: ${1}
- source_labels:
  - __address__
  target_label: __address__
  regex: ^(.*):(.*)
  replacement: ${1}:9100
metric_relabel_configs: []
stream_parse: true
proxy_tls_config:
  insecure_skip_verify: true
proxy_bearer_token_file: /tmp/proxy-token
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateNodeScrapeConfig(context.Background(), &tt.args.cr, tt.args.m, tt.args.i, tt.args.apiserverConfig, tt.args.ssCache, tt.args.ignoreHonorLabels, tt.args.overrideHonorTimestamps, tt.args.enforcedNamespaceLabel)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal NodeScrapeConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))
		})
	}
}
