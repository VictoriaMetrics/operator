package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

func TestConvertAlertmanagerConfig(t *testing.T) {
	type opts struct {
		promCfg  *promv1alpha1.AlertmanagerConfig
		validate func(convertedAMCfg *vmv1beta1.VMAlertmanagerConfig)
	}
	f := func(o opts) {
		t.Helper()
		converted, err := ConvertAlertmanagerConfig(o.promCfg, &config.BaseOperatorConf{})
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		o.validate(converted)
	}

	// simple convert
	f(opts{
		promCfg: &promv1alpha1.AlertmanagerConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-1"},
			Spec: promv1alpha1.AlertmanagerConfigSpec{
				MuteTimeIntervals: []promv1alpha1.MuteTimeInterval{{
					Name: "test",
					TimeIntervals: []promv1alpha1.TimeInterval{{
						Months: []promv1alpha1.MonthRange{"1:4"},
					}},
				}},
				Route: &promv1alpha1.Route{Receiver: "webhook", GroupInterval: "1min"},
				Receivers: []promv1alpha1.Receiver{
					{
						Name:           "webhook",
						WebhookConfigs: []promv1alpha1.WebhookConfig{{URLSecret: &corev1.SecretKeySelector{Key: "secret"}}},
					},
				},
			},
		},
		validate: func(convertedAMCfg *vmv1beta1.VMAlertmanagerConfig) {
			assert.Equal(t, convertedAMCfg.Name, "test-1")
			assert.Equal(t, convertedAMCfg.Spec.Route.Receiver, "webhook")
			assert.Equal(t, convertedAMCfg.Spec.Receivers[0].Name, "webhook")
			assert.Equal(t, convertedAMCfg.Spec.Receivers[0].WebhookConfigs[0].URLSecret.Key, "secret")
			assert.Equal(t, convertedAMCfg.Spec.TimeIntervals[0].Name, "test")
			assert.Equal(t, convertedAMCfg.Spec.TimeIntervals[0].TimeIntervals[0].Months, []string{"1:4"})
		},
	})

	// msteamsv2
	f(opts{
		promCfg: &promv1alpha1.AlertmanagerConfig{
			Spec: promv1alpha1.AlertmanagerConfigSpec{
				Receivers: []promv1alpha1.Receiver{
					{
						Name: "msteams",
						MSTeamsV2Configs: []promv1alpha1.MSTeamsV2Config{
							{
								SendResolved: ptr.To(true),
								WebhookURL: &corev1.SecretKeySelector{
									Key: "some-key",
								},
								Title: ptr.To("some title"),
							},
						},
					},
				},
			},
		},
		validate: func(convertedAMCfg *vmv1beta1.VMAlertmanagerConfig) {
			assert.Len(t, convertedAMCfg.Spec.Receivers, 1)
			assert.Len(t, convertedAMCfg.Spec.Receivers[0].MSTeamsV2Configs, 1)
			assert.Equal(t, convertedAMCfg.Spec.Receivers[0].MSTeamsV2Configs[0].Title, "some title")
			assert.Equal(t, convertedAMCfg.Spec.Receivers[0].MSTeamsV2Configs[0].URLSecret.Key, "some-key")
		},
	})
}

func TestConvertScrapeConfig(t *testing.T) {
	type opts struct {
		scrapeConfig *promv1alpha1.ScrapeConfig
		ownerRef     bool
		want         vmv1beta1.VMScrapeConfig
	}
	f := func(opts opts) {
		t.Helper()
		got := ConvertScrapeConfig(opts.scrapeConfig, &config.BaseOperatorConf{EnabledPrometheusConverterOwnerReferences: opts.ownerRef})
		if !cmp.Equal(*got, opts.want) {
			diff := cmp.Diff(*got, opts.want)
			t.Fatal("not expected output with diff: ", diff)
		}
	}

	// with static config
	f(opts{
		scrapeConfig: &promv1alpha1.ScrapeConfig{
			Spec: promv1alpha1.ScrapeConfigSpec{
				StaticConfigs: []promv1alpha1.StaticConfig{{
					Targets: []promv1alpha1.Target{"target-1", "target-2"},
				}},
				HonorTimestamps:   ptr.To(true),
				EnableCompression: ptr.To(true),
				BasicAuth: &promv1.BasicAuth{
					Username: corev1.SecretKeySelector{Key: "username"},
					Password: corev1.SecretKeySelector{Key: "password"},
				},
				ScrapeInterval: promv1.DurationPointer("5m"),
				MetricsPath:    ptr.To("/test"),
				ProxyConfig: promv1.ProxyConfig{
					ProxyURL: ptr.To("http://proxy.com"),
				},
				RelabelConfigs: []promv1.RelabelConfig{{
					Action:      "LabelMap",
					Regex:       "__meta_kubernetes_pod_label_(.+)",
					Replacement: ptr.To("foo_$1"),
				}},
				MetricRelabelConfigs: []promv1.RelabelConfig{{
					SourceLabels: []promv1.LabelName{"__meta_kubernetes_pod_name", "__meta_kubernetes_pod_container_port_number"},
					Separator:    ptr.To(":"),
					TargetLabel:  "host_port",
				}},
			},
		},
		want: vmv1beta1.VMScrapeConfig{
			Spec: vmv1beta1.VMScrapeConfigSpec{
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					ProxyURL:        ptr.To("http://proxy.com"),
					HonorTimestamps: ptr.To(true),
					VMScrapeParams:  &vmv1beta1.VMScrapeParams{DisableCompression: ptr.To(false)},
					Path:            "/test",
					ScrapeInterval:  "5m",
				},
				StaticConfigs: []vmv1beta1.StaticConfig{{
					Targets: []string{"target-1", "target-2"},
				}},
				EndpointAuth: vmv1beta1.EndpointAuth{
					BasicAuth: &vmv1beta1.BasicAuth{
						Username: corev1.SecretKeySelector{Key: "username"},
						Password: corev1.SecretKeySelector{Key: "password"},
					},
				},
				EndpointRelabelings: vmv1beta1.EndpointRelabelings{
					RelabelConfigs: []*vmv1beta1.RelabelConfig{{
						Action:      "LabelMap",
						Regex:       vmv1beta1.StringOrArray{"__meta_kubernetes_pod_label_(.+)"},
						Replacement: ptr.To("foo_$1"),
					}},
					MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{{
						SourceLabels: []string{"__meta_kubernetes_pod_name", "__meta_kubernetes_pod_container_port_number"},
						Separator:    ptr.To(":"),
						TargetLabel:  "host_port",
					}},
				},
			},
		},
	})

	// with httpsd config
	f(opts{
		scrapeConfig: &promv1alpha1.ScrapeConfig{
			Spec: promv1alpha1.ScrapeConfigSpec{
				HTTPSDConfigs: []promv1alpha1.HTTPSDConfig{
					{
						URL: "http://test1.com",
						Authorization: &promv1.SafeAuthorization{
							Type: "Bearer",
							Credentials: &corev1.SecretKeySelector{
								Key: "token",
							},
						},
					},
					{
						URL: "http://test2.com",
						TLSConfig: &promv1.SafeTLSConfig{
							CA:                 promv1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{Key: "ca.crt"}},
							Cert:               promv1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{Key: "cert.pem"}},
							KeySecret:          &corev1.SecretKeySelector{Key: "key"},
							ServerName:         ptr.To("test"),
							InsecureSkipVerify: ptr.To(true),
						},
					},
				},
			},
		},
		want: vmv1beta1.VMScrapeConfig{
			Spec: vmv1beta1.VMScrapeConfigSpec{
				HTTPSDConfigs: []vmv1beta1.HTTPSDConfig{
					{
						URL: "http://test1.com",
						Authorization: &vmv1beta1.Authorization{
							Type: "Bearer",
							Credentials: &corev1.SecretKeySelector{
								Key: "token",
							},
						},
					},
					{
						URL: "http://test2.com",
						TLSConfig: &vmv1beta1.TLSConfig{
							CA:                 vmv1beta1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{Key: "ca.crt"}},
							Cert:               vmv1beta1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{Key: "cert.pem"}},
							KeySecret:          &corev1.SecretKeySelector{Key: "key"},
							ServerName:         "test",
							InsecureSkipVerify: true,
						},
					},
				},
			},
		},
	})

	// with k8s sd config
	f(opts{
		scrapeConfig: &promv1alpha1.ScrapeConfig{
			Spec: promv1alpha1.ScrapeConfigSpec{
				KubernetesSDConfigs: []promv1alpha1.KubernetesSDConfig{
					{
						APIServer: ptr.To("http://1.2.3.4"),
						Role:      promv1alpha1.KubernetesRole("pod"),
						Selectors: []promv1alpha1.K8SSelectorConfig{{
							Label: ptr.To("app=test"),
						}},
					},
				},
			},
		},
		want: vmv1beta1.VMScrapeConfig{
			Spec: vmv1beta1.VMScrapeConfigSpec{
				KubernetesSDConfigs: []vmv1beta1.KubernetesSDConfig{
					{
						APIServer: ptr.To("http://1.2.3.4"),
						Role:      "pod",
						Selectors: []vmv1beta1.K8SSelectorConfig{{
							Label: "app=test",
						}},
					},
				},
			},
		},
	})

	// with consul sd config
	f(opts{
		scrapeConfig: &promv1alpha1.ScrapeConfig{
			Spec: promv1alpha1.ScrapeConfigSpec{
				ConsulSDConfigs: []promv1alpha1.ConsulSDConfig{{
					Server:     "http://1.2.3.4",
					TokenRef:   &corev1.SecretKeySelector{Key: "token"},
					Datacenter: ptr.To("prod"),
					Namespace:  ptr.To("test"),
				}},
			},
		},
		want: vmv1beta1.VMScrapeConfig{
			Spec: vmv1beta1.VMScrapeConfigSpec{
				ConsulSDConfigs: []vmv1beta1.ConsulSDConfig{{
					Server:     "http://1.2.3.4",
					TokenRef:   &corev1.SecretKeySelector{Key: "token"},
					Datacenter: ptr.To("prod"),
					Namespace:  ptr.To("test"),
				}},
			},
		},
	})

	// with ec2 sd config
	f(opts{
		scrapeConfig: &promv1alpha1.ScrapeConfig{
			Spec: promv1alpha1.ScrapeConfigSpec{
				EC2SDConfigs: []promv1alpha1.EC2SDConfig{{
					Region:    ptr.To("us-west-1"),
					AccessKey: &corev1.SecretKeySelector{Key: "accesskey"},
					SecretKey: &corev1.SecretKeySelector{Key: "secret"},
					Filters: []promv1alpha1.Filter{{
						Name:   "f1",
						Values: []string{"1"},
					}},
					Port: ptr.To[int32](80),
				}},
			},
		},
		want: vmv1beta1.VMScrapeConfig{
			Spec: vmv1beta1.VMScrapeConfigSpec{
				EC2SDConfigs: []vmv1beta1.EC2SDConfig{{
					Region:    ptr.To("us-west-1"),
					AccessKey: &corev1.SecretKeySelector{Key: "accesskey"},
					SecretKey: &corev1.SecretKeySelector{Key: "secret"},
					Filters: []*vmv1beta1.EC2Filter{{
						Name:   "f1",
						Values: []string{"1"},
					}},
					Port: ptr.To(80),
				}},
			},
		},
	})

	// with owner
	f(opts{
		scrapeConfig: &promv1alpha1.ScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test-ns",
				UID:       "42",
			},
			Spec: promv1alpha1.ScrapeConfigSpec{},
		},
		ownerRef: true,
		want: vmv1beta1.VMScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test-ns",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "monitoring.coreos.com/v1alpha1",
					Kind:               "ScrapeConfig",
					Name:               "test",
					UID:                "42",
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				}},
			},
			Spec: vmv1beta1.VMScrapeConfigSpec{},
		},
	})

	// with gce sd config
	f(opts{
		scrapeConfig: &promv1alpha1.ScrapeConfig{
			Spec: promv1alpha1.ScrapeConfigSpec{
				GCESDConfigs: []promv1alpha1.GCESDConfig{{
					Project:      "eu-project",
					Zone:         "zone-1",
					TagSeparator: ptr.To(""),
					Port:         ptr.To[int32](80),
				}},
			},
		},
		want: vmv1beta1.VMScrapeConfig{
			Spec: vmv1beta1.VMScrapeConfigSpec{
				GCESDConfigs: []vmv1beta1.GCESDConfig{{
					Project:      "eu-project",
					Zone:         vmv1beta1.StringOrArray{"zone-1"},
					TagSeparator: ptr.To(""),
					Port:         ptr.To(80),
				}},
			},
		},
	})

	// with nomad sd config
	f(opts{
		scrapeConfig: &promv1alpha1.ScrapeConfig{
			Spec: promv1alpha1.ScrapeConfigSpec{
				NomadSDConfigs: []promv1alpha1.NomadSDConfig{{
					Server:       "http://nomad.example.com:4646",
					Namespace:    ptr.To("default"),
					Region:       ptr.To("global"),
					TagSeparator: ptr.To(","),
					AllowStale:   ptr.To(true),
				}},
			},
		},
		want: vmv1beta1.VMScrapeConfig{
			Spec: vmv1beta1.VMScrapeConfigSpec{
				NomadSDConfigs: []vmv1beta1.NomadSDConfig{{
					Server:       "http://nomad.example.com:4646",
					Namespace:    ptr.To("default"),
					Region:       ptr.To("global"),
					TagSeparator: ptr.To(","),
					AllowStale:   ptr.To(true),
				}},
			},
		},
	})

	// nomad with multiple regions and relabeling
	f(opts{
		scrapeConfig: &promv1alpha1.ScrapeConfig{
			Spec: promv1alpha1.ScrapeConfigSpec{
				ScrapeInterval: promv1.DurationPointer("20s"),
				ScrapeTimeout:  promv1.DurationPointer("19s"),
				NomadSDConfigs: []promv1alpha1.NomadSDConfig{
					{
						Server:     "https://nomad.example.com:4646",
						Namespace:  ptr.To("default"),
						AllowStale: ptr.To(true),
						Authorization: &promv1.SafeAuthorization{
							Credentials: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "nomad-secret"},
								Key:                  "NOMAD_TOKEN",
							},
						},
					},
					{
						Server:     "https://nomad.example.com:4646",
						Namespace:  ptr.To("staging"),
						AllowStale: ptr.To(true),
						Authorization: &promv1.SafeAuthorization{
							Credentials: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "nomad-secret"},
								Key:                  "NOMAD_TOKEN",
							},
						},
					},
				},
				RelabelConfigs: []promv1.RelabelConfig{
					{Action: "replace", SourceLabels: []promv1.LabelName{"__meta_nomad_namespace"}, TargetLabel: "namespace"},
					{Action: "replace", SourceLabels: []promv1.LabelName{"__meta_nomad_service"}, TargetLabel: "service"},
					{Action: "replace", SourceLabels: []promv1.LabelName{"__meta_nomad_node_id"}, TargetLabel: "node"},
					{Action: "keep", Regex: "metrics", SourceLabels: []promv1.LabelName{"__meta_nomad_service"}},
				},
				MetricRelabelConfigs: []promv1.RelabelConfig{
					{
						Action:       "replace",
						Regex:        `org-id-123`,
						Replacement:  ptr.To("my-org"),
						SourceLabels: []promv1.LabelName{"customer"},
						TargetLabel:  "tenant",
					},
				},
			},
		},
		want: vmv1beta1.VMScrapeConfig{
			Spec: vmv1beta1.VMScrapeConfigSpec{
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					ScrapeInterval: "20s",
					ScrapeTimeout:  "19s",
				},
				NomadSDConfigs: []vmv1beta1.NomadSDConfig{
					{
						Server:     "https://nomad.example.com:4646",
						Namespace:  ptr.To("default"),
						AllowStale: ptr.To(true),
						Authorization: &vmv1beta1.Authorization{
							Credentials: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "nomad-secret"},
								Key:                  "NOMAD_TOKEN",
							},
						},
					},
					{
						Server:     "https://nomad.example.com:4646",
						Namespace:  ptr.To("staging"),
						AllowStale: ptr.To(true),
						Authorization: &vmv1beta1.Authorization{
							Credentials: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "nomad-secret"},
								Key:                  "NOMAD_TOKEN",
							},
						},
					},
				},
				EndpointRelabelings: vmv1beta1.EndpointRelabelings{
					RelabelConfigs: []*vmv1beta1.RelabelConfig{
						{
							Action:       "replace",
							SourceLabels: []string{"__meta_nomad_namespace"},
							TargetLabel:  "namespace",
						},
						{
							Action:       "replace",
							SourceLabels: []string{"__meta_nomad_service"},
							TargetLabel:  "service",
						},
						{
							Action:       "replace",
							SourceLabels: []string{"__meta_nomad_node_id"},
							TargetLabel:  "node",
						},
						{
							Action:       "keep",
							SourceLabels: []string{"__meta_nomad_service"},
							Regex:        vmv1beta1.StringOrArray{"metrics"},
						},
					},
					MetricRelabelConfigs: []*vmv1beta1.RelabelConfig{{
						SourceLabels: []string{"customer"},
						TargetLabel:  "tenant",
						Regex:        vmv1beta1.StringOrArray{"org-id-123"},
						Replacement:  ptr.To("my-org"),
						Action:       "replace",
					}},
				},
			},
		},
	})

	// nomad with basic auth
	f(opts{
		scrapeConfig: &promv1alpha1.ScrapeConfig{
			Spec: promv1alpha1.ScrapeConfigSpec{
				NomadSDConfigs: []promv1alpha1.NomadSDConfig{{
					Server:          "https://nomad.example.com:4646",
					Namespace:       ptr.To("prod"),
					Region:          ptr.To("us-west-1"),
					TagSeparator:    ptr.To(","),
					AllowStale:      ptr.To(true),
					FollowRedirects: ptr.To(true),
					BasicAuth: &promv1.BasicAuth{
						Username: corev1.SecretKeySelector{Key: "user"},
						Password: corev1.SecretKeySelector{Key: "pass"},
					},
					TLSConfig: &promv1.SafeTLSConfig{
						InsecureSkipVerify: ptr.To(true),
					},
				}},
			},
		},
		want: vmv1beta1.VMScrapeConfig{
			Spec: vmv1beta1.VMScrapeConfigSpec{
				NomadSDConfigs: []vmv1beta1.NomadSDConfig{{
					Server:          "https://nomad.example.com:4646",
					Namespace:       ptr.To("prod"),
					Region:          ptr.To("us-west-1"),
					TagSeparator:    ptr.To(","),
					AllowStale:      ptr.To(true),
					FollowRedirects: ptr.To(true),
					BasicAuth: &vmv1beta1.BasicAuth{
						Username: corev1.SecretKeySelector{Key: "user"},
						Password: corev1.SecretKeySelector{Key: "pass"},
					},
					TLSConfig: &vmv1beta1.TLSConfig{
						InsecureSkipVerify: true,
					},
				}},
			},
		},
	})
}
