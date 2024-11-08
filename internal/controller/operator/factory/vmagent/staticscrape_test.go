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
	"k8s.io/utils/ptr"
)

func Test_generateStaticScrapeConfig(t *testing.T) {
	type args struct {
		cr      vmv1beta1.VMAgent
		m       *vmv1beta1.VMStaticScrape
		ep      *vmv1beta1.TargetEndpoint
		i       int
		ssCache *scrapesSecretsCache
		se      vmv1beta1.VMAgentSecurityEnforcements
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "basic cfg",
			args: args{
				ssCache: &scrapesSecretsCache{},
				m: &vmv1beta1.VMStaticScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "static-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMStaticScrapeSpec{
						JobName: "static-job",
					},
				},
				ep: &vmv1beta1.TargetEndpoint{
					Targets: []string{"192.168.11.1:9100", "some-host:9100"},
					Labels:  map[string]string{"env": "dev", "group": "prod"},
				},
			},
			want: `job_name: staticScrape/default/static-1/0
static_configs:
- targets:
  - 192.168.11.1:9100
  - some-host:9100
  labels:
    env: dev
    group: prod
honor_labels: false
relabel_configs:
- target_label: job
  replacement: static-job
`,
		},
		{
			name: "basic cfg with overrides",
			args: args{
				ssCache: &scrapesSecretsCache{},
				m: &vmv1beta1.VMStaticScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "static-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMStaticScrapeSpec{
						JobName: "static-job",
					},
				},
				ep: &vmv1beta1.TargetEndpoint{
					Targets: []string{"192.168.11.1:9100", "some-host:9100"},
					Labels:  map[string]string{"env": "dev", "group": "prod"},
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						HonorTimestamps: ptr.To(true),
					},
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{
							{
								TargetLabel:  "namespace",
								SourceLabels: []string{"abuse"},
								Action:       "replace",
							},
						},
					},
				},
				se: vmv1beta1.VMAgentSecurityEnforcements{
					OverrideHonorTimestamps: false,
					OverrideHonorLabels:     true,
					EnforcedNamespaceLabel:  "namespace",
				},
			},
			want: `job_name: staticScrape/default/static-1/0
static_configs:
- targets:
  - 192.168.11.1:9100
  - some-host:9100
  labels:
    env: dev
    group: prod
honor_labels: false
honor_timestamps: true
relabel_configs:
- target_label: job
  replacement: static-job
- target_label: namespace
  replacement: default
`,
		},
		{
			name: "complete cfg with overrides",
			args: args{
				ssCache: &scrapesSecretsCache{
					baSecrets: map[string]*k8stools.BasicAuthCredentials{
						"staticScrapeProxy/default/static-1/0": {
							Password: "proxy-password",
							Username: "proxy-user",
						},
						"staticScrape/default/static-1/0": {
							Password: "pass",
							Username: "admin",
						},
					},
					bearerTokens: map[string]string{},
					oauth2Secrets: map[string]*k8stools.OAuthCreds{
						"staticScrape/default/static-1/0": {
							ClientID:     "some-id",
							ClientSecret: "some-secret",
						},
					},
				},
				m: &vmv1beta1.VMStaticScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "static-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMStaticScrapeSpec{
						JobName:     "static-job",
						SampleLimit: 50,
					},
				},
				ep: &vmv1beta1.TargetEndpoint{
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Params:          map[string][]string{"timeout": {"50s"}, "follow": {"false"}},
						SampleLimit:     60,
						ScrapeTimeout:   "55s",
						Interval:        "10s",
						ScrapeInterval:  "50s",
						FollowRedirects: ptr.To(true),
						Path:            "/metrics-1",
						Scheme:          "https",
						ProxyURL:        ptr.To("https://some-proxy"),
						HonorLabels:     ptr.To(true),
						HonorTimestamps: ptr.To(true),

						VMScrapeParams: &vmv1beta1.VMScrapeParams{
							ScrapeOffset:        ptr.To("10s"),
							DisableKeepAlive:    ptr.To(true),
							DisableCompression:  ptr.To(true),
							ScrapeAlignInterval: ptr.To("5s"),
							StreamParse:         ptr.To(true),
							Headers:             []string{"customer-header: with-value"},
							ProxyClientConfig: &vmv1beta1.ProxyAuth{
								BasicAuth: &vmv1beta1.BasicAuth{
									Username: corev1.SecretKeySelector{
										Key:                  "user",
										LocalObjectReference: corev1.LocalObjectReference{Name: "ba-proxy-secret"},
									},
									Password: corev1.SecretKeySelector{
										Key:                  "password",
										LocalObjectReference: corev1.LocalObjectReference{Name: "ba-proxy-secret"},
									},
								},
							},
						},
					},
					EndpointAuth: vmv1beta1.EndpointAuth{
						TLSConfig: &vmv1beta1.TLSConfig{
							CA: vmv1beta1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{
								Key:                  "ca",
								LocalObjectReference: corev1.LocalObjectReference{Name: "tls-cfg"},
							}},
							CertFile: "/tmp/cert-part",
							KeySecret: &corev1.SecretKeySelector{
								Key:                  "key",
								LocalObjectReference: corev1.LocalObjectReference{Name: "tls-cfg"},
							},
							InsecureSkipVerify: true,
						},
						BasicAuth: &vmv1beta1.BasicAuth{
							Username: corev1.SecretKeySelector{
								Key:                  "user",
								LocalObjectReference: corev1.LocalObjectReference{Name: "ba-secret"},
							},
							Password: corev1.SecretKeySelector{
								Key:                  "password",
								LocalObjectReference: corev1.LocalObjectReference{Name: "ba-secret"},
							},
						},
						BearerTokenSecret: &corev1.SecretKeySelector{
							Key:                  "token",
							LocalObjectReference: corev1.LocalObjectReference{Name: "token-secret"},
						},
						OAuth2: &vmv1beta1.OAuth2{
							ClientSecret: &corev1.SecretKeySelector{Key: "client-s", LocalObjectReference: corev1.LocalObjectReference{Name: "oauth-2s"}},
							ClientID: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{Key: "client-id", LocalObjectReference: corev1.LocalObjectReference{Name: "oauth-2s"}},
							},
						},
					},
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						RelabelConfigs: []*vmv1beta1.RelabelConfig{
							{
								Action:       "drop",
								SourceLabels: []string{"src"},
								Regex:        []string{"vmagent", "vmalert"},
							},
						},

						MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{
							{
								TargetLabel:  "namespace",
								SourceLabels: []string{"abuse"},
								Action:       "replace",
							},
							{
								TargetLabel:  "pod_status",
								SourceLabels: []string{"pod"},
								Action:       "replace",
							},
						},
					},

					Targets: []string{"192.168.11.1:9100", "some-host:9100"},
					Labels:  map[string]string{"env": "dev", "group": "prod"},
				},
				se: vmv1beta1.VMAgentSecurityEnforcements{
					OverrideHonorTimestamps: false,
					OverrideHonorLabels:     true,
					EnforcedNamespaceLabel:  "namespace",
				},
			},
			want: `job_name: staticScrape/default/static-1/0
static_configs:
- targets:
  - 192.168.11.1:9100
  - some-host:9100
  labels:
    env: dev
    group: prod
honor_labels: false
honor_timestamps: true
scrape_interval: 50s
scrape_timeout: 55s
metrics_path: /metrics-1
proxy_url: https://some-proxy
follow_redirects: true
params:
  follow:
  - "false"
  timeout:
  - 50s
scheme: https
sample_limit: 60
relabel_configs:
- target_label: job
  replacement: static-job
- source_labels:
  - src
  regex:
  - vmagent
  - vmalert
  action: drop
- target_label: namespace
  replacement: default
metric_relabel_configs:
- source_labels:
  - pod
  target_label: pod_status
  action: replace
scrape_align_interval: 5s
stream_parse: true
disable_compression: true
scrape_offset: 10s
disable_keepalive: true
headers:
- 'customer-header: with-value'
proxy_basic_auth:
  username: proxy-user
  password: proxy-password
tls_config:
  insecure_skip_verify: true
  ca_file: /etc/vmagent-tls/certs/default_tls-cfg_ca
  cert_file: /tmp/cert-part
  key_file: /etc/vmagent-tls/certs/default_tls-cfg_key
basic_auth:
  username: admin
  password: pass
oauth2:
  client_id: some-id
  client_secret: some-secret
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateStaticScrapeConfig(context.Background(), &tt.args.cr, tt.args.m, tt.args.ep, tt.args.i, tt.args.ssCache, tt.args.se)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !assert.Equal(t, tt.want, string(gotBytes)) {
				t.Errorf("generateStaticScrapeConfig() = \n%v, want \n%v", string(gotBytes), tt.want)
			}
		})
	}
}
