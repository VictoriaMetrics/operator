package v1alpha1

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

func TestConvertAlertmanagerConfig(t *testing.T) {
	type opts struct {
		promCfg  *promv1alpha1.AlertmanagerConfig
		validate func(convertedAMCfg *vmv1beta1.VMAlertmanagerConfig) error
	}
	f := func(opts opts) {
		t.Helper()
		converted, err := ConvertAlertmanagerConfig(opts.promCfg, &config.BaseOperatorConf{})
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if err := opts.validate(converted); err != nil {
			t.Fatalf("not valid converted alertmanager config: %s", err)
		}
	}

	// simple convert
	o := opts{
		promCfg: &promv1alpha1.AlertmanagerConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-1"},
			Spec: promv1alpha1.AlertmanagerConfigSpec{
				Route: &promv1alpha1.Route{Receiver: "webhook", GroupInterval: "1min"},
				Receivers: []promv1alpha1.Receiver{
					{
						Name:           "webhook",
						WebhookConfigs: []promv1alpha1.WebhookConfig{{URLSecret: &corev1.SecretKeySelector{Key: "secret"}}},
					},
				},
			},
		},
		validate: func(convertedAMCfg *vmv1beta1.VMAlertmanagerConfig) error {
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
		},
	}
	f(o)

	// msteamsv2
	o = opts{
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
		validate: func(convertedAMCfg *vmv1beta1.VMAlertmanagerConfig) error {
			if len(convertedAMCfg.Spec.Receivers) != 1 {
				return fmt.Errorf("expected single receiver, got: %d", len(convertedAMCfg.Spec.Receivers))
			}
			if len(convertedAMCfg.Spec.Receivers[0].MSTeamsV2Configs) != 1 {
				return fmt.Errorf("expected single msteamsv2 receiver, got: %d", len(convertedAMCfg.Spec.Receivers[0].MSTeamsV2Configs))
			}
			msv2 := convertedAMCfg.Spec.Receivers[0].MSTeamsV2Configs[0]
			if msv2.Title != "some title" {
				return fmt.Errorf("unexpected title: %q", msv2.Title)
			}
			if msv2.URLSecret.Key != "some-key" {
				return fmt.Errorf("unexpected key=%q", msv2.URLSecret.Key)
			}
			return nil
		},
	}
	f(o)
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
	o := opts{
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
				MetricsPath: ptr.To("/test"),
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
	}
	f(o)

	// with httpsd config
	o = opts{
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
	}
	f(o)

	// with k8s sd config
	o = opts{
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
	}
	f(o)

	// with consul sd config
	o = opts{
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
	}
	f(o)

	// with ec2 sd config
	o = opts{
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
	}
	f(o)

	// with owner
	o = opts{
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
	}
	f(o)

	// with gce sd config
	o = opts{
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
	}
	f(o)
}
