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

func Test_generateStaticScrapeConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		sc                *vmv1beta1.VMStaticScrape
		ep                *vmv1beta1.TargetEndpoint
		i                 int
		want              string
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		ac := getAssetsCache(ctx, fclient, opts.cr)
		got, err := generateStaticScrapeConfig(ctx, opts.cr, opts.sc, opts.ep, opts.i, ac)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		gotBytes, err := yaml.Marshal(got)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !assert.Equal(t, opts.want, string(gotBytes)) {
			t.Errorf("generateStaticScrapeConfig() = \n%v, want \n%v", string(gotBytes), opts.want)
		}
	}

	// basic cfg
	o := opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMStaticScrape{
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
	}
	f(o)

	// basic cfg with overrides
	o = opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				VMAgentSecurityEnforcements: vmv1beta1.VMAgentSecurityEnforcements{
					OverrideHonorTimestamps: false,
					OverrideHonorLabels:     true,
					EnforcedNamespaceLabel:  "namespace",
				},
			},
		},
		sc: &vmv1beta1.VMStaticScrape{
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
	}
	f(o)

	// complete cfg with overrides
	o = opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				VMAgentSecurityEnforcements: vmv1beta1.VMAgentSecurityEnforcements{
					OverrideHonorTimestamps: false,
					OverrideHonorLabels:     true,
					EnforcedNamespaceLabel:  "namespace",
				},
			},
		},
		sc: &vmv1beta1.VMStaticScrape{
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
				HonorLabels:     true,
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
								Key:                  "username",
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
						Key:                  "username",
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
bearer_token: token-value
basic_auth:
  username: admin
  password: pass
oauth2:
  client_id: some-id
  client_secret: some-secret
`,
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "token-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"token": []byte("token-value"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth-2s",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"client-s":  []byte("some-secret"),
					"client-id": []byte("some-id"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ba-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte("admin"),
					"password": []byte("pass"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ba-proxy-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte("proxy-user"),
					"password": []byte("proxy-password"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-cfg",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"ca":  []byte("ca-value"),
					"key": []byte("key-value"),
				},
			},
		},
	}
	f(o)
}
